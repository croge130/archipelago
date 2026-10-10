package jobsdirector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	"github.com/croge130/archipelago/jobs/structure"
	"github.com/croge130/archipelago/jobsauth"
	"github.com/croge130/archipelago/jobsexec"
	"github.com/croge130/archipelago/peerauth"
	"github.com/croge130/archipelago/router"
	"github.com/croge130/archipelago/routerauth"
	"github.com/croge130/archipelago/wire"
	"github.com/google/uuid"
)

// PermissionExecute is the global permission an executor holds to use the
// protocol at all. It is registered by jobsauth.RegisterPermissions with the
// other jobs permissions (and so by the SDK's seed step).
const PermissionExecute = jobsauth.PermissionExecute

// Config configures a Director.
type Config struct {
	// Deps are the stores. All four are required: a director claims and
	// completes.
	Deps jobsauth.Deps

	// ClaimTTL is how long a pull claims a job for, and how far each
	// heartbeat extends it (default one minute).
	ClaimTTL time.Duration
	// MaxPull caps how many jobs one pull may take (default 16).
	MaxPull int

	Logger *slog.Logger
}

// Director serves the executor protocol.
type Director struct {
	cfg Config
	log *slog.Logger
}

// New creates a Director.
func New(cfg Config) (*Director, error) {
	d := cfg.Deps
	if d.GatehouseReader == nil || d.GatehouseWriter == nil || d.JobsReader == nil || d.JobsWriter == nil {
		return nil, errors.New("jobsdirector: a director needs a Gatehouse reader and writer and a jobs reader and writer")
	}
	if cfg.ClaimTTL <= 0 {
		cfg.ClaimTTL = time.Minute
	}
	if cfg.MaxPull <= 0 {
		cfg.MaxPull = 16
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Director{cfg: cfg, log: log}, nil
}

// Register registers the six routes through routerauth, each requiring
// PermissionExecute. The pull is sequential per session; the rest may
// overlap, since a session's jobs heartbeat and finish independently.
func (d *Director) Register(ctx context.Context, reg *routerauth.Registrar) error {
	routes := []struct {
		key, desc   string
		h           router.Handler
		concurrency int
	}{
		{jobsexec.RoutePull, "Claim jobs for a remote executor", d.pull, 1},
		{jobsexec.RouteHeartbeat, "Extend a remote executor's claim", d.heartbeat, 8},
		{jobsexec.RouteComplete, "Report a job succeeded", d.complete, 8},
		{jobsexec.RouteFail, "Report a job attempt failed", d.fail, 8},
		{jobsexec.RouteAbandon, "Hand a claim back", d.abandon, 8},
		{jobsexec.RouteAuthorize, "Ask whether a running job may perform an operation", d.authorize, 8},
	}
	for _, r := range routes {
		if err := reg.Handle(ctx, routerauth.Endpoint{
			Key: r.key, Description: r.desc, Permission: PermissionExecute, Handler: r.h,
			Concurrency: r.concurrency, AllowReservedNamespace: true,
		}); err != nil {
			return fmt.Errorf("jobsdirector: register %s: %w", r.key, err)
		}
	}
	return nil
}

func decode(req router.Request, into any) error {
	if err := json.Unmarshal(req.Message.Payload, into); err != nil {
		return router.Errorf(wire.ErrInvalid, "malformed payload")
	}
	return nil
}

var errNotYours = router.Errorf(wire.ErrUnauthorized, "not authorized")

// caller resolves the executor's principal from the verified connection and
// checks that the instance it names is registered to that principal.
func (d *Director) caller(ctx context.Context, req router.Request, instance uuid.UUID) (uuid.UUID, error) {
	actor, err := peerauth.ResolvePrincipal(ctx, d.cfg.Deps.GatehouseReader, req.Session)
	if err != nil {
		if errors.Is(err, peerauth.ErrNoPeerIdentity) || errors.Is(err, peerauth.ErrUnknownPeer) {
			return uuid.Nil, errNotYours
		}
		return uuid.Nil, err
	}
	inst, found, err := d.cfg.Deps.GatehouseReader.GetInstance(ctx, instance)
	if err != nil {
		return uuid.Nil, err
	}
	if !found || inst.PrincipalID != actor {
		return uuid.Nil, errNotYours
	}
	return actor, nil
}

func (d *Director) pull(ctx context.Context, req router.Request) (json.RawMessage, error) {
	var body jobsexec.PullRequest
	if err := decode(req, &body); err != nil {
		return nil, err
	}
	if len(body.TaskKeys) == 0 || len(body.QueueKeys) == 0 || body.Limit < 1 {
		return nil, router.Errorf(wire.ErrInvalid, "pull needs task_keys, queue_keys and a positive limit")
	}
	actor, err := d.caller(ctx, req, body.InstanceID)
	if err != nil {
		return nil, err
	}
	limit := min(body.Limit, d.cfg.MaxPull)
	ready, skipped, cerr := jobsauth.Claim(ctx, d.cfg.Deps, jobsauth.ClaimRequest{
		ExecutorInstanceID: body.InstanceID, ExecutorPrincipalID: actor,
		TaskKeys: body.TaskKeys, QueueKeys: body.QueueKeys, Limit: limit, ClaimTTL: d.cfg.ClaimTTL,
	})
	if cerr != nil {
		if errors.Is(cerr, evaluation.ErrDenied) && len(ready) == 0 {
			d.log.Warn("pull refused: not permitted on a queue", "principal_id", actor, "reason", cerr.Error())
			return nil, errNotYours
		}
		d.log.Error("pull hit an error", "principal_id", actor, "error", cerr.Error(), "delivering", len(ready))
		if len(ready) == 0 && len(skipped) == 0 {
			return nil, cerr
		}
	}
	reply := jobsexec.PullReply{Jobs: make([]jobsexec.PulledJob, 0, len(ready)), Partial: cerr != nil}
	for _, cj := range ready {
		reply.Jobs = append(reply.Jobs, jobsexec.PulledJob{
			JobID: cj.JobID, TaskKey: cj.TaskKey, QueueKey: cj.QueueKey, Params: cj.Params, Attempt: cj.Attempt,
			Priority: string(cj.Priority), AttemptTimeoutSeconds: int(cj.AttemptTimeout / time.Second),
			ClaimSeconds: int(d.cfg.ClaimTTL / time.Second), RequestedBy: cj.RequestedBy,
			EffectivePrincipalID: cj.EffectivePrincipalID, AssumedSessionID: cj.SessionID, Trace: cj.TraceContext,
		})
	}
	for _, s := range skipped {
		reply.Skipped = append(reply.Skipped, jobsexec.SkippedJob{JobID: s.JobID, Reason: s.Reason, Released: s.Released})
	}
	return json.Marshal(reply)
}

// resume rebuilds the claim a call names. ok is false when the claim is no
// longer held; rec is then the current record if it is this actor's own
// attempt (see jobsauth.Resume), which finishing calls use to recognise a
// retry.
func (d *Director) resume(ctx context.Context, req router.Request, ref jobsexec.ClaimRef) (cj jobsauth.ClaimedJob, rec structure.Job, hasRec, ok bool, err error) {
	actor, err := d.caller(ctx, req, ref.InstanceID)
	if err != nil {
		return cj, rec, false, false, err
	}
	cj, err = jobsauth.Resume(ctx, d.cfg.Deps, actor, ref.JobID, ref.Attempt)
	switch {
	case err == nil:
		if cj.ClaimedBy == nil || *cj.ClaimedBy != ref.InstanceID {
			return jobsauth.ClaimedJob{}, structure.Job{}, false, false, nil
		}
		return cj, cj.Job, true, true, nil
	case errors.Is(err, jobsauth.ErrClaimLost):
		return jobsauth.ClaimedJob{}, cj.Job, cj.JobID != uuid.Nil, false, nil
	default:
		return jobsauth.ClaimedJob{}, structure.Job{}, false, false, err
	}
}

func (d *Director) heartbeat(ctx context.Context, req router.Request) (json.RawMessage, error) {
	var ref jobsexec.ClaimRef
	if err := decode(req, &ref); err != nil {
		return nil, err
	}
	cj, _, _, ok, err := d.resume(ctx, req, ref)
	if err != nil {
		return nil, err
	}
	if !ok {
		return json.Marshal(jobsexec.HeartbeatReply{OK: false})
	}
	res, err := d.cfg.Deps.JobsWriter.HeartbeatJob(ctx, cj.JobID, cj.Attempt, d.cfg.ClaimTTL)
	if err != nil {
		return nil, err
	}
	return json.Marshal(jobsexec.HeartbeatReply{OK: res.OK, CancelRequested: res.CancelRequested})
}

func (d *Director) complete(ctx context.Context, req router.Request) (json.RawMessage, error) {
	var body jobsexec.CompleteRequest
	if err := decode(req, &body); err != nil {
		return nil, err
	}
	cj, rec, hasRec, ok, err := d.resume(ctx, req, body.ClaimRef)
	if err != nil {
		return nil, err
	}
	if !ok {
		// A retry after a lost reply: the first completion already landed.
		if hasRec && rec.State == structure.StateSucceeded {
			return json.Marshal(jobsexec.FinishReply{Applied: true, Replayed: true, State: string(rec.State)})
		}
		return json.Marshal(jobsexec.FinishReply{Applied: false})
	}
	applied, err := jobsauth.Complete(ctx, d.cfg.Deps, cj, body.Result)
	if err != nil {
		return nil, err
	}
	state := structure.StateSucceeded
	if !applied {
		state = ""
	}
	return json.Marshal(jobsexec.FinishReply{Applied: applied, State: string(state)})
}

func (d *Director) fail(ctx context.Context, req router.Request) (json.RawMessage, error) {
	var body jobsexec.FailRequest
	if err := decode(req, &body); err != nil {
		return nil, err
	}
	cj, rec, hasRec, ok, err := d.resume(ctx, req, body.ClaimRef)
	if err != nil {
		return nil, err
	}
	if !ok {
		// pending (to retry) or dead at this same attempt means our fail landed.
		if hasRec && (rec.State == structure.StatePending || rec.State == structure.StateDead) {
			return json.Marshal(jobsexec.FinishReply{Applied: true, Replayed: true, State: string(rec.State)})
		}
		return json.Marshal(jobsexec.FinishReply{Applied: false})
	}
	cause := errors.New(body.Error)
	if body.AuthorityDenied {
		cause = fmt.Errorf("%w: %s", jobsauth.ErrAuthorityDenied, body.Error)
	}
	res, err := jobsauth.Fail(ctx, d.cfg.Deps, cj, cause)
	if err != nil {
		return nil, err
	}
	return json.Marshal(jobsexec.FinishReply{Applied: res.Applied, State: string(res.State)})
}

func (d *Director) abandon(ctx context.Context, req router.Request) (json.RawMessage, error) {
	var body jobsexec.AbandonRequest
	if err := decode(req, &body); err != nil {
		return nil, err
	}
	if body.RetryAfterSeconds < 0 {
		return nil, router.Errorf(wire.ErrInvalid, "retry_after_seconds must not be negative")
	}
	cj, rec, hasRec, ok, err := d.resume(ctx, req, body.ClaimRef)
	if err != nil {
		return nil, err
	}
	if !ok {
		if hasRec && (rec.State == structure.StatePending || rec.State == structure.StateCancelled) {
			return json.Marshal(jobsexec.FinishReply{Applied: true, Replayed: true, State: string(rec.State)})
		}
		return json.Marshal(jobsexec.FinishReply{Applied: false})
	}
	res, err := jobsauth.Abandon(ctx, d.cfg.Deps, cj, time.Duration(body.RetryAfterSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	return json.Marshal(jobsexec.FinishReply{Applied: res.Applied, State: string(res.State)})
}

func (d *Director) authorize(ctx context.Context, req router.Request) (json.RawMessage, error) {
	var body jobsexec.AuthorizeRequest
	if err := decode(req, &body); err != nil {
		return nil, err
	}
	if body.Permission == "" {
		return nil, router.Errorf(wire.ErrInvalid, "permission is required")
	}
	cj, _, _, ok, err := d.resume(ctx, req, body.ClaimRef)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, router.Errorf(wire.ErrInvalid, "the claim is not held")
	}
	err = jobsauth.Authorize(ctx, d.cfg.Deps, cj, body.Permission, body.ContextType, body.ContextID)
	switch {
	case err == nil:
		return json.Marshal(jobsexec.AuthorizeReply{Allowed: true})
	case errors.Is(err, jobsauth.ErrOutOfScope):
		return json.Marshal(jobsexec.AuthorizeReply{OutOfScope: true, Reason: err.Error()})
	case errors.Is(err, jobsauth.ErrAuthorityDenied):
		return json.Marshal(jobsexec.AuthorizeReply{Reason: err.Error()})
	default:
		return nil, err // a lapsed session or a store failure is not a decision
	}
}
