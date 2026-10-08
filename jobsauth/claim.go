package jobsauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	jobsEvaluation "github.com/croge130/archipelago/jobs/evaluation"
	jobsFacade "github.com/croge130/archipelago/jobs/facade"
	"github.com/croge130/archipelago/jobs/structure"
	"github.com/google/uuid"
)

// DefaultReleaseDelay is how long a claim this executor cannot serve is
// kept from it, so another executor gets the chance. Overridable per
// claim.
const DefaultReleaseDelay = 30 * time.Second

// ClaimRequest is an executor asking for work.
type ClaimRequest struct {
	// ExecutorInstanceID is the Instance taking the claim; ExecutorPrincipalID
	// is A, the principal that instance authenticates as.
	ExecutorInstanceID  uuid.UUID
	ExecutorPrincipalID uuid.UUID

	// TaskKeys are the kinds this executor has handlers for.
	TaskKeys []string
	// QueueKeys are the queues to pull from. Required and explicit: the
	// executor must hold jobs.claim on each, and "any queue" cannot be
	// checked against a permission.
	QueueKeys []string

	Limit    int
	ClaimTTL time.Duration

	// ReleaseDelay overrides DefaultReleaseDelay.
	ReleaseDelay time.Duration
}

// ClaimedJob is a job this executor holds, with the identities and scope
// its handler runs under.
type ClaimedJob struct {
	structure.Job

	// ActorPrincipalID is A, the executor. EffectivePrincipalID is C,
	// whose permissions the handler's operations are evaluated against.
	ActorPrincipalID     uuid.UUID
	EffectivePrincipalID uuid.UUID

	// SessionID is the assumed session minted for this claim, or nil when
	// the effective principal is the executor itself.
	SessionID *uuid.UUID

	// Scope is the task kind's declared scope resolved for this job.
	Scope []jobsEvaluation.ResolvedScope
}

// Skipped explains a job the claim took but this executor will not run.
type Skipped struct {
	JobID  uuid.UUID
	Reason string
	// Released means the job went back to pending for someone else;
	// otherwise it ended terminally.
	Released bool
}

// Claim pulls work for an executor. It checks the executor holds
// jobs.claim on every queue it asks for, claims, and for each job
// prepares the identity its handler will run under — minting an assumed
// session where the effective principal is not the executor.
//
// Jobs it cannot prepare are dealt with, not left claimed:
//   - the executor may not execute as the effective principal: the claim
//     is released (without spending an attempt) after a delay, since
//     another executor may be allowed;
//   - the requester may no longer cause work as the effective principal,
//     or a principal is gone, or the scope cannot be resolved: the job as
//     submitted can never run, so it ends dead.
//
// Both the ready jobs and the skipped ones are returned. The error
// collects unexpected failures (a store error mid-way); it does not
// discard jobs that were prepared successfully.
func Claim(ctx context.Context, d Deps, req ClaimRequest) ([]ClaimedJob, []Skipped, error) {
	if err := d.needWriters("claim"); err != nil {
		return nil, nil, err
	}
	if len(req.QueueKeys) == 0 {
		return nil, nil, fmt.Errorf("jobsauth: claim: QueueKeys is required")
	}
	if req.ExecutorPrincipalID == uuid.Nil {
		return nil, nil, fmt.Errorf("jobsauth: claim: ExecutorPrincipalID is required")
	}
	for _, q := range req.QueueKeys {
		if err := evaluation.RequireContextPermission(ctx, d.GatehouseReader, req.ExecutorPrincipalID, PermissionClaim, ContextTypeQueue, q); err != nil {
			return nil, nil, fmt.Errorf("jobsauth: claim from queue %q: %w", q, err)
		}
	}

	actor := req.ExecutorPrincipalID
	claimed, err := d.JobsWriter.ClaimJobs(ctx, structure.ClaimRequest{
		ClaimerInstanceID: req.ExecutorInstanceID,
		ActorPrincipalID:  &actor,
		TaskKeys:          req.TaskKeys,
		QueueKeys:         req.QueueKeys,
		Limit:             req.Limit,
		ClaimTTL:          req.ClaimTTL,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("jobsauth: claim: %w", err)
	}

	delay := req.ReleaseDelay
	if delay <= 0 {
		delay = DefaultReleaseDelay
	}
	var ready []ClaimedJob
	var skipped []Skipped
	var errs []error
	defs := map[string]structure.TaskDefinition{}
	for _, job := range claimed {
		cj, skip, err := prepare(ctx, d, defs, actor, job, delay)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("job %s: %w", job.JobID, err))
			if skip != nil {
				skipped = append(skipped, *skip)
			}
		case skip != nil:
			skipped = append(skipped, *skip)
		default:
			ready = append(ready, *cj)
		}
	}
	return ready, skipped, errors.Join(errs...)
}

func prepare(ctx context.Context, d Deps, defs map[string]structure.TaskDefinition, actor uuid.UUID, job structure.Job, delay time.Duration) (*ClaimedJob, *Skipped, error) {
	fail := func(reason string, cause error) (*ClaimedJob, *Skipped, error) {
		_, ferr := jobsFacade.FailAttempt(ctx, d.JobsWriter, job, fmt.Errorf("%s: %w", reason, cause), true)
		return nil, &Skipped{JobID: job.JobID, Reason: reason + ": " + cause.Error()}, ferr
	}
	release := func(reason string, cause error) (*ClaimedJob, *Skipped, error) {
		_, rerr := d.JobsWriter.ReleaseJob(ctx, job.JobID, job.Attempt, delay)
		return nil, &Skipped{JobID: job.JobID, Reason: reason + ": " + cause.Error(), Released: true}, rerr
	}

	def, ok := defs[job.TaskKey]
	if !ok {
		var found bool
		var err error
		def, found, err = d.JobsReader.GetTaskDefinition(ctx, job.TaskKey)
		if err != nil {
			return release("could not read the task definition", err)
		}
		if !found {
			return fail("task definition is missing", jobsFacade.ErrUnknownTask)
		}
		defs[job.TaskKey] = def
	}
	scope, err := jobsEvaluation.ResolveScope(def, job.Params)
	if err != nil {
		return fail("declared scope cannot be resolved for this job", err)
	}

	effective := effectivePrincipal(job, actor)
	cj := &ClaimedJob{Job: job, ActorPrincipalID: actor, EffectivePrincipalID: effective, Scope: scope}
	if effective == actor {
		return cj, nil, nil
	}

	session, err := gatehouseFacade.AssumeSession(ctx, d.GatehouseReader, d.GatehouseWriter, gatehouseFacade.AssumeRequest{
		CreatorPrincipalID:   actor,
		RequesterPrincipalID: job.RequestedBy,
		AsPrincipalID:        effective,
		TTL:                  job.AttemptTimeout,
		Metadata:             sessionBinding(job),
	})
	switch {
	case err == nil:
	case errors.Is(err, gatehouseFacade.ErrAssumeExecuteDenied):
		return release("this executor may not execute as the job's principal", err)
	case errors.Is(err, gatehouseFacade.ErrAssumeCauseDenied), errors.Is(err, gatehouseFacade.ErrPrincipalNotFound):
		return fail("authority denied", err)
	default:
		return release("could not create the assumed session", err)
	}

	attached, err := d.JobsWriter.AttachClaimIdentity(ctx, job.JobID, job.Attempt, &session.SessionID)
	if err != nil || !attached {
		// The claim is gone (or the store failed): the session serves
		// nothing. Revoke it and let the claim lapse or be reclaimed.
		rerr := gatehouseFacade.RevokeSession(ctx, d.GatehouseWriter, session.SessionID)
		if err != nil {
			return nil, nil, errors.Join(err, rerr)
		}
		return nil, &Skipped{JobID: job.JobID, Reason: "the claim was lost before the session could be attached"}, rerr
	}
	cj.SessionID = &session.SessionID
	return cj, nil, nil
}

// effectivePrincipal is C for a job claimed by actor.
func effectivePrincipal(job structure.Job, actor uuid.UUID) uuid.UUID {
	switch job.AuthorityMode {
	case structure.AuthorityOwner:
		return job.RequestedBy
	case structure.AuthorityAssumed:
		return *job.RunAs // Validate guarantees it is set for this mode
	default: // service
		return actor
	}
}

// sessionBinding is the opaque metadata tying an assumed session to the
// one claim it serves.
func sessionBinding(job structure.Job) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"job_id":      job.JobID.String(),
		"attempt":     job.Attempt,
		"task_key":    job.TaskKey,
		"params_hash": job.ParamsHash,
	})
	return b
}

// Complete marks the claimed job succeeded and revokes its session. The
// session is revoked even when the completion is refused — a refusal
// means the claim was lost, and the session belongs to the lost attempt.
// ok is false in that case.
func Complete(ctx context.Context, d Deps, cj ClaimedJob, result json.RawMessage) (ok bool, err error) {
	if err := d.needWriters("complete"); err != nil {
		return false, err
	}
	ok, cerr := d.JobsWriter.CompleteJob(ctx, cj.JobID, cj.Attempt, result)
	rerr := revokeSession(ctx, d, cj)
	return ok && cerr == nil, errors.Join(cerr, rerr)
}

// Fail reports a failed attempt and revokes the session. A cause that
// wraps ErrAuthorityDenied is terminal: retrying a permission failure
// never helps. Any other cause is retried per the job's rules.
func Fail(ctx context.Context, d Deps, cj ClaimedJob, cause error) (structure.FailResult, error) {
	if err := d.needWriters("fail"); err != nil {
		return structure.FailResult{}, err
	}
	res, ferr := jobsFacade.FailAttempt(ctx, d.JobsWriter, cj.Job, cause, errors.Is(cause, ErrAuthorityDenied))
	rerr := revokeSession(ctx, d, cj)
	return res, errors.Join(ferr, rerr)
}

// Abandon hands a claim back without spending an attempt — for an
// executor that is shutting down, say — and revokes the session.
func Abandon(ctx context.Context, d Deps, cj ClaimedJob, retryAfter time.Duration) (structure.ReleaseResult, error) {
	if err := d.needWriters("abandon"); err != nil {
		return structure.ReleaseResult{}, err
	}
	res, aerr := d.JobsWriter.ReleaseJob(ctx, cj.JobID, cj.Attempt, retryAfter)
	rerr := revokeSession(ctx, d, cj)
	return res, errors.Join(aerr, rerr)
}

func revokeSession(ctx context.Context, d Deps, cj ClaimedJob) error {
	if cj.SessionID == nil {
		return nil
	}
	return gatehouseFacade.RevokeSession(ctx, d.GatehouseWriter, *cj.SessionID)
}

// Authorize is the one check a handler makes before an operation that
// needs permission: it requires the operation to fall inside the task
// kind's declared scope for this job, and to be permitted to the
// effective principal right now — through the assumed session when there
// is one (so a revoked or expired session, or an edge that has since been
// withdrawn, denies), directly otherwise.
//
// contextType and contextID are both empty for a global check. A
// scope or permission denial wraps ErrAuthorityDenied; a revoked or
// expired session and store failures are returned as themselves, so a
// handler (or Fail) does not mistake a lapsed session for a permanent
// refusal.
func Authorize(ctx context.Context, d Deps, cj ClaimedJob, permissionKey, contextType, contextID string) error {
	if !jobsEvaluation.ScopePermits(cj.Scope, permissionKey, contextType, contextID) {
		return fmt.Errorf("%w: %s", ErrOutOfScope, describe(permissionKey, contextType, contextID))
	}
	var err error
	switch {
	case cj.SessionID != nil && contextType == "":
		err = gatehouseFacade.RequireAssumedPermission(ctx, d.GatehouseReader, *cj.SessionID, permissionKey)
	case cj.SessionID != nil:
		err = gatehouseFacade.RequireAssumedContextPermission(ctx, d.GatehouseReader, *cj.SessionID, permissionKey, contextType, contextID)
	case contextType == "":
		err = evaluation.RequirePermission(ctx, d.GatehouseReader, cj.EffectivePrincipalID, permissionKey)
	default:
		err = evaluation.RequireContextPermission(ctx, d.GatehouseReader, cj.EffectivePrincipalID, permissionKey, contextType, contextID)
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, evaluation.ErrDenied) || errors.Is(err, gatehouseFacade.ErrAssumeNotPermitted) {
		return fmt.Errorf("%w: %s: %v", ErrAuthorityDenied, describe(permissionKey, contextType, contextID), err)
	}
	return err
}

func describe(permissionKey, contextType, contextID string) string {
	if contextType == "" {
		return permissionKey
	}
	return fmt.Sprintf("%s on %s/%s", permissionKey, contextType, contextID)
}
