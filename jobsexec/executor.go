package jobsexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/router"
	"github.com/croge130/archipelago/wire"
	"github.com/google/uuid"
)

var (
	// ErrAuthorityDenied is returned by Job.Authorize when the director
	// says no. A handler that returns it (or an error wrapping it) fails the
	// job terminally: retrying a permission failure never helps.
	ErrAuthorityDenied = errors.New("jobsexec: authority denied")

	// ErrOutOfScope is the part of ErrAuthorityDenied where the operation
	// is outside what the task kind declared it may do.
	ErrOutOfScope = fmt.Errorf("%w: outside the task kind's declared scope", ErrAuthorityDenied)

	// ErrClaimLost is the cause of a handler's context ending because the
	// director says this executor no longer holds the claim.
	ErrClaimLost = errors.New("jobsexec: the claim was lost")

	// ErrCancelRequested is the cause when the job's owner asked for
	// cancellation.
	ErrCancelRequested = errors.New("jobsexec: cancellation requested")

	errShutdown = errors.New("jobsexec: executor shutting down")
)

// Caller is the one thing an Executor needs of its director connection.
// *router.Peer satisfies it.
type Caller interface {
	Call(ctx context.Context, typ string, payload json.RawMessage) (json.RawMessage, error)
}

// Job is what a handler is given.
type Job struct {
	ID                   uuid.UUID
	TaskKey              string
	QueueKey             string
	Params               json.RawMessage
	Attempt              int
	Priority             string
	RequestedBy          uuid.UUID
	EffectivePrincipalID uuid.UUID
	AssumedSessionID     *uuid.UUID

	e   *Executor
	ref ClaimRef
}

// Authorize asks the director whether the operation the handler is about
// to perform is permitted: inside the task kind's declared scope and
// allowed to the effective principal now. It returns nil if so, an error
// wrapping ErrAuthorityDenied (or ErrOutOfScope) if not, and any other
// error if no decision could be made, which is not a denial. Handlers
// that skip it are trusted to; see 20, "Remote executors".
func (j Job) Authorize(ctx context.Context, permission, contextType, contextID string) error {
	var reply AuthorizeReply
	if err := j.e.call(ctx, RouteAuthorize, AuthorizeRequest{ClaimRef: j.ref, Permission: permission, ContextType: contextType, ContextID: contextID}, &reply); err != nil {
		return err
	}
	switch {
	case reply.Allowed:
		return nil
	case reply.OutOfScope:
		return fmt.Errorf("%w: %s", ErrOutOfScope, reply.Reason)
	default:
		return fmt.Errorf("%w: %s", ErrAuthorityDenied, reply.Reason)
	}
}

// Handler runs one job. The context ends when the attempt times out, the
// claim is lost, cancellation is requested (context.Cause says which) or
// the executor shuts down. A handler must be idempotent: a claim can lapse
// and the work run again elsewhere.
type Handler func(ctx context.Context, job Job) (json.RawMessage, error)

// Config configures an Executor. InstanceID and QueueKeys are required.
type Config struct {
	// InstanceID is this executor's registered Instance (registry): it must
	// belong to the principal the director sees on the connection.
	InstanceID uuid.UUID
	QueueKeys  []string

	// MaxConcurrent bounds jobs in flight, and so how many it asks for
	// (default 4). Until the governor exists (19) this is the whole of the
	// consent rule.
	MaxConcurrent int
	// PollInterval is the wait after a pull that found nothing (default 2s).
	PollInterval time.Duration
	// HeartbeatInterval overrides the default of a third of the claim.
	HeartbeatInterval time.Duration
	// BusyBackoff is the wait before retrying a call answered busy
	// (default 200ms), up to five times.
	BusyBackoff time.Duration

	Logger *slog.Logger
}

// Executor pulls jobs from a director and runs them.
type Executor struct {
	cfg      Config
	director Caller
	log      *slog.Logger

	mu       sync.Mutex
	handlers map[string]Handler
	inFlight int
	wg       sync.WaitGroup
	wake     chan struct{}
}

// New creates an Executor over a connection to its director.
func New(director Caller, cfg Config) (*Executor, error) {
	if director == nil {
		return nil, errors.New("jobsexec: a director connection is required")
	}
	if cfg.InstanceID == uuid.Nil || len(cfg.QueueKeys) == 0 {
		return nil, errors.New("jobsexec: InstanceID and QueueKeys are required")
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 4
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.BusyBackoff <= 0 {
		cfg.BusyBackoff = 200 * time.Millisecond
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Executor{cfg: cfg, director: director, log: log, handlers: map[string]Handler{}, wake: make(chan struct{}, 1)}, nil
}

// Handle registers the handler for a task kind. Registering one is how a
// node adopts the executor node role for that kind.
func (e *Executor) Handle(taskKey string, h Handler) error {
	if taskKey == "" || h == nil {
		return errors.New("jobsexec: a task key and a handler are required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, dup := e.handlers[taskKey]; dup {
		return fmt.Errorf("jobsexec: a handler for %q is already registered", taskKey)
	}
	e.handlers[taskKey] = h
	return nil
}

// InFlight is how many jobs are running now.
func (e *Executor) InFlight() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.inFlight
}

// Run pulls and runs jobs until ctx ends, then hands back every claim it
// still holds (without spending an attempt) and returns once the handlers
// have stopped. It returns early, with the error, if the director refuses
// the executor in a way waiting will not fix: unauthorized, unknown route,
// or an unsupported protocol version.
func (e *Executor) Run(ctx context.Context) error {
	defer e.wg.Wait()
	for {
		started, err := e.PullOnce(ctx)
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil:
			var re *router.RemoteError
			if errors.As(err, &re) {
				switch re.Code {
				case wire.ErrUnauthorized, wire.ErrUnknownRoute, wire.ErrVersionUnsupported, wire.ErrHelloRequired:
					return fmt.Errorf("jobsexec: the director refused this executor: %w", err)
				}
			}
			e.log.Warn("pull failed; will retry", "error", err.Error())
		case started > 0:
			continue // there may be more, and capacity may remain
		}
		select {
		case <-ctx.Done():
			return nil
		case <-e.wake: // a job finished: capacity is free
		case <-time.After(e.cfg.PollInterval):
		}
	}
}

// PullOnce asks the director for as many jobs as there is free capacity
// for and starts them. It returns how many it started.
func (e *Executor) PullOnce(ctx context.Context) (int, error) {
	e.mu.Lock()
	free := e.cfg.MaxConcurrent - e.inFlight
	kinds := make([]string, 0, len(e.handlers))
	for k := range e.handlers {
		kinds = append(kinds, k)
	}
	e.mu.Unlock()
	if free <= 0 || len(kinds) == 0 {
		return 0, nil
	}
	var reply PullReply
	if err := e.call(ctx, RoutePull, PullRequest{InstanceID: e.cfg.InstanceID, TaskKeys: kinds, QueueKeys: e.cfg.QueueKeys, Limit: free}, &reply); err != nil {
		return 0, err
	}
	for _, s := range reply.Skipped {
		e.log.Info("the director did not deliver a claimed job", "job_id", s.JobID, "released", s.Released, "reason", s.Reason)
	}
	if reply.Partial {
		e.log.Warn("the director reported an error preparing some jobs; the ones delivered are valid")
	}
	for _, pj := range reply.Jobs {
		e.start(ctx, pj)
	}
	return len(reply.Jobs), nil
}

func (e *Executor) start(parent context.Context, pj PulledJob) {
	e.mu.Lock()
	h := e.handlers[pj.TaskKey]
	e.inFlight++
	e.mu.Unlock()
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			e.mu.Lock()
			e.inFlight--
			e.mu.Unlock()
			select {
			case e.wake <- struct{}{}:
			default:
			}
		}()
		e.runJob(parent, h, pj)
	}()
}

func (e *Executor) runJob(parent context.Context, h Handler, pj PulledJob) {
	ref := ClaimRef{JobID: pj.JobID, Attempt: pj.Attempt, InstanceID: e.cfg.InstanceID}
	job := Job{ID: pj.JobID, TaskKey: pj.TaskKey, QueueKey: pj.QueueKey, Params: pj.Params, Attempt: pj.Attempt,
		Priority: pj.Priority, RequestedBy: pj.RequestedBy, EffectivePrincipalID: pj.EffectivePrincipalID,
		AssumedSessionID: pj.AssumedSessionID, e: e, ref: ref}
	log := e.log.With("job_id", pj.JobID, "task_key", pj.TaskKey, "attempt", pj.Attempt)

	// The shutdown cause is set on the parent's cancellation so it can be
	// told apart from a claim lost or a cancel requested.
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(parent))
	defer cancel(nil)
	stopParent := context.AfterFunc(parent, func() { cancel(errShutdown) })
	defer stopParent()
	if pj.AttemptTimeoutSeconds > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, time.Duration(pj.AttemptTimeoutSeconds)*time.Second)
		defer stop()
	}
	if pj.Trace != nil {
		ctx = logging.ContextWithSpan(ctx, *pj.Trace)
	}

	interval := e.cfg.HeartbeatInterval
	if interval <= 0 {
		interval = time.Duration(pj.ClaimSeconds) * time.Second / 3
	}
	if interval <= 0 {
		interval = time.Second
	}
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			var reply HeartbeatReply
			if err := e.call(ctx, RouteHeartbeat, ref, &reply); err != nil {
				if ctx.Err() == nil {
					log.Warn("heartbeat failed", "error", err.Error())
				}
				continue
			}
			switch {
			case !reply.OK:
				cancel(ErrClaimLost)
				return
			case reply.CancelRequested:
				cancel(ErrCancelRequested)
			}
		}
	}()

	result, herr := e.safely(ctx, h, job)
	cause := context.Cause(ctx)
	cancel(nil)
	<-hbDone

	// Reports go out on a context that outlives the handler's, bounded.
	rctx, rstop := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer rstop()
	switch {
	case errors.Is(cause, ErrClaimLost):
		log.Warn("the claim was lost; reporting nothing")
	case herr == nil:
		var reply FinishReply
		if err := e.call(rctx, RouteComplete, CompleteRequest{ClaimRef: ref, Result: result}, &reply); err != nil {
			log.Error("could not report completion", "error", err.Error())
		} else if !reply.Applied {
			log.Warn("completion was refused: the claim was no longer this attempt's")
		}
	case errors.Is(cause, ErrCancelRequested), errors.Is(cause, errShutdown):
		var reply FinishReply
		if err := e.call(rctx, RouteAbandon, AbandonRequest{ClaimRef: ref}, &reply); err != nil {
			log.Error("could not hand the claim back", "error", err.Error())
		}
		log.Info("claim handed back", "cause", cause.Error(), "state", reply.State)
	default:
		msg := herr.Error()
		if errors.Is(cause, context.DeadlineExceeded) && errors.Is(herr, context.DeadlineExceeded) {
			msg = "attempt timed out: " + msg
		}
		var reply FinishReply
		if err := e.call(rctx, RouteFail, FailRequest{ClaimRef: ref, Error: msg, AuthorityDenied: errors.Is(herr, ErrAuthorityDenied)}, &reply); err != nil {
			log.Error("could not report failure", "error", err.Error())
		}
		log.Info("attempt failed", "error", msg, "state", reply.State)
	}
}

// safely turns a handler panic into an error: one bad handler must not
// take the executor, and every other job it holds, down with it.
func (e *Executor) safely(ctx context.Context, h Handler, job Job) (result json.RawMessage, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panicked: %v", r)
		}
	}()
	return h(ctx, job)
}

// call sends a request and decodes the reply, retrying a few times when the
// director answers busy. It does not retry anything else.
func (e *Executor) call(ctx context.Context, route string, req, reply any) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	var raw json.RawMessage
	for attempt := 0; ; attempt++ {
		raw, err = e.director.Call(ctx, route, payload)
		if err == nil || !router.IsRetryable(err) || attempt >= 5 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.cfg.BusyBackoff):
		}
	}
	if err != nil {
		return err
	}
	if reply == nil {
		return nil
	}
	if err := json.Unmarshal(raw, reply); err != nil {
		return fmt.Errorf("jobsexec: malformed %s reply: %w", route, err)
	}
	return nil
}
