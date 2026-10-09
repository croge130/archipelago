package jobsexec

import (
	"encoding/json"

	"github.com/croge130/archipelago/logging"
	"github.com/google/uuid"
)

// The routes a director serves. Every one is a Call from an executor.
const (
	RoutePull      = "jobs.pull"
	RouteHeartbeat = "jobs.heartbeat"
	RouteComplete  = "jobs.complete"
	RouteFail      = "jobs.fail"
	RouteAbandon   = "jobs.abandon"
	RouteAuthorize = "jobs.authorize"
)

// ClaimRef names the claim a call is about. The attempt is the claim
// token: a call naming an old attempt is refused or reported as lost. The
// instance is the executor's registered Instance, which the director checks
// belongs to the caller's verified principal.
type ClaimRef struct {
	JobID      uuid.UUID `json:"job_id"`
	Attempt    int       `json:"attempt"`
	InstanceID uuid.UUID `json:"instance_id"`
}

// PullRequest asks for work the executor can admit right now.
type PullRequest struct {
	InstanceID uuid.UUID `json:"instance_id"`
	TaskKeys   []string  `json:"task_keys"`
	QueueKeys  []string  `json:"queue_keys"`
	Limit      int       `json:"limit"`
}

// PulledJob is a claimed job as the executor sees it. It carries no
// credential: EffectivePrincipalID and AssumedSessionID say whose authority
// applies, and the director is asked (RouteAuthorize) before anything that
// needs it.
type PulledJob struct {
	JobID                 uuid.UUID            `json:"job_id"`
	TaskKey               string               `json:"task_key"`
	QueueKey              string               `json:"queue_key"`
	Params                json.RawMessage      `json:"params"`
	Attempt               int                  `json:"attempt"`
	Priority              string               `json:"priority"`
	AttemptTimeoutSeconds int                  `json:"attempt_timeout_seconds"`
	ClaimSeconds          int                  `json:"claim_seconds"`
	RequestedBy           uuid.UUID            `json:"requested_by"`
	EffectivePrincipalID  uuid.UUID            `json:"effective_principal_id"`
	AssumedSessionID      *uuid.UUID           `json:"assumed_session_id,omitempty"`
	Trace                 *logging.SpanContext `json:"trace,omitempty"`
}

// SkippedJob is a job the director claimed for this executor and then
// dealt with instead of delivering (released for someone else, or ended).
type SkippedJob struct {
	JobID    uuid.UUID `json:"job_id"`
	Reason   string    `json:"reason"`
	Released bool      `json:"released"`
}

// PullReply carries the jobs to run. Partial means the director hit an
// error preparing some other job; the ones listed are valid regardless.
type PullReply struct {
	Jobs    []PulledJob  `json:"jobs"`
	Skipped []SkippedJob `json:"skipped,omitempty"`
	Partial bool         `json:"partial,omitempty"`
}

// HeartbeatReply: OK false means the claim is gone and the executor must
// stop and report nothing; CancelRequested asks it to stop cooperatively.
type HeartbeatReply struct {
	OK              bool `json:"ok"`
	CancelRequested bool `json:"cancel_requested,omitempty"`
}

type CompleteRequest struct {
	ClaimRef
	Result json.RawMessage `json:"result,omitempty"`
}

type FailRequest struct {
	ClaimRef
	Error           string `json:"error"`
	AuthorityDenied bool   `json:"authority_denied,omitempty"`
}

type AbandonRequest struct {
	ClaimRef
	RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`
}

// FinishReply answers complete, fail and abandon. Applied false means the
// claim was no longer this attempt's. Replayed means an earlier identical
// call already did the work and this one only confirms it, which is what
// makes retrying after a lost reply safe.
type FinishReply struct {
	Applied  bool   `json:"applied"`
	Replayed bool   `json:"replayed,omitempty"`
	State    string `json:"state,omitempty"`
}

type AuthorizeRequest struct {
	ClaimRef
	Permission  string `json:"permission"`
	ContextType string `json:"context_type,omitempty"`
	ContextID   string `json:"context_id,omitempty"`
}

// AuthorizeReply is a decision. A lapsed session or a store failure is not
// a decision and comes back as a coded error instead.
type AuthorizeReply struct {
	Allowed    bool   `json:"allowed"`
	OutOfScope bool   `json:"out_of_scope,omitempty"`
	Reason     string `json:"reason,omitempty"`
}
