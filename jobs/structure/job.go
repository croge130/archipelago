package structure

import (
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/croge130/archipelago/logging"
	"github.com/google/uuid"
)

// Job is one durable unit of deferred work. Times are set by the
// database's clock, never a node's, which sidesteps the clock-skew
// problem 19 records for the current lease code.
type Job struct {
	JobID uuid.UUID

	TaskKey  string
	QueueKey string // the authorization scope (a context ID)

	Params     json.RawMessage // validated against the TaskDefinition
	ParamsHash string          // fixed at submission; binds authority to these exact parameters

	State    State
	Priority Priority // a hint; the executor applies its own cap

	// TargetInstanceID optionally names one executor; nil means any
	// executor of the kind.
	TargetInstanceID *uuid.UUID
	IdempotencyKey   string // "" means none

	RunAt     time.Time  // earliest start
	ExpiresAt *time.Time // give up if still pending

	// Retryable is copied from the TaskDefinition's Idempotent at
	// submission, so claiming and failing can decide in one statement
	// without joining the definition. MaxAttempts is 1 unless Retryable.
	Retryable      bool
	MaxAttempts    int
	AttemptTimeout time.Duration
	Backoff        BackoffPolicy

	// Attempt is incremented at each claim and doubles as the claim
	// token: completing or failing a job names the attempt it was
	// claimed under, so a stale claimant is refused.
	Attempt         int
	ClaimedBy       *uuid.UUID // an executor Instance
	ClaimedUntil    *time.Time
	CancelRequested bool

	// B, C and A of 09's model. RequestedBy is the owner.
	RequestedBy        uuid.UUID
	AuthorityMode      AuthorityMode
	RunAs              *uuid.UUID // C; set only for AuthorityAssumed
	ActorPrincipalID   *uuid.UUID // A; recorded at claim
	AssumedSessionID   *uuid.UUID // the assumed session for the current claim, if any
	AuthoritySourceRef string     // optional, e.g. a schedule, per 09

	TraceContext *logging.SpanContext

	Result    json.RawMessage
	LastError string

	CreatedAt  time.Time
	UpdatedAt  time.Time
	FinishedAt *time.Time
}

// Validate checks a job as submitted. It does not check cross-field
// consistency of a claimed or finished job; the state machine in
// evaluation and the single-statement storage operations own that.
func (j Job) Validate() error {
	if j.JobID == uuid.Nil {
		return fmt.Errorf("structure: job: JobID is required")
	}
	if err := ValidateTaskKey(j.TaskKey); err != nil {
		return fmt.Errorf("structure: job: %w", err)
	}
	if err := ValidateQueueKey(j.QueueKey); err != nil {
		return fmt.Errorf("structure: job: %w", err)
	}
	if len(j.Params) > MaxParamsBytes {
		return fmt.Errorf("structure: job: Params is %d bytes, over the %d limit", len(j.Params), MaxParamsBytes)
	}
	if j.ParamsHash == "" {
		return fmt.Errorf("structure: job: ParamsHash is required")
	}
	if !j.State.Valid() {
		return fmt.Errorf("structure: job: invalid State %q", j.State)
	}
	if !j.Priority.Valid() {
		return fmt.Errorf("structure: job: invalid Priority %q", j.Priority)
	}
	if utf8.RuneCountInString(j.IdempotencyKey) > MaxIdempotencyKeyRunes {
		return fmt.Errorf("structure: job: IdempotencyKey is longer than %d characters", MaxIdempotencyKeyRunes)
	}
	if j.MaxAttempts < 1 {
		return fmt.Errorf("structure: job: MaxAttempts must be at least 1")
	}
	if !j.Retryable && j.MaxAttempts != 1 {
		return fmt.Errorf("structure: job: a job that is not Retryable has exactly one attempt, got MaxAttempts %d", j.MaxAttempts)
	}
	if j.AttemptTimeout <= 0 {
		return fmt.Errorf("structure: job: AttemptTimeout must be positive")
	}
	if err := j.Backoff.Validate(); err != nil {
		return err
	}
	if j.RequestedBy == uuid.Nil {
		return fmt.Errorf("structure: job: RequestedBy (the owner) is required")
	}
	if !j.AuthorityMode.Valid() {
		return fmt.Errorf("structure: job: invalid AuthorityMode %q", j.AuthorityMode)
	}
	switch {
	case j.AuthorityMode == AuthorityAssumed && (j.RunAs == nil || *j.RunAs == uuid.Nil):
		return fmt.Errorf("structure: job: AuthorityAssumed requires RunAs")
	case j.AuthorityMode != AuthorityAssumed && j.RunAs != nil:
		return fmt.Errorf("structure: job: RunAs is only valid with AuthorityAssumed")
	}
	return nil
}
