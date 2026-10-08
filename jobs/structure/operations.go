package structure

import (
	"time"

	"github.com/google/uuid"
)

// The request and result types of the Writer's coarse commands. They
// live here, not in facade, because the facade declares the Writer
// interface and the storage layer implements it: both need the types
// and neither may import the other.

// ClaimRequest asks for up to Limit jobs this executor can serve.
type ClaimRequest struct {
	// ClaimerInstanceID is the executor Instance taking the claim.
	ClaimerInstanceID uuid.UUID
	// ActorPrincipalID is A, the executor's principal; recorded on the job.
	ActorPrincipalID *uuid.UUID
	// TaskKeys are the kinds this executor has handlers for. A claim
	// never returns a kind not listed: a node runs only what it can.
	TaskKeys []string
	// QueueKeys restricts the claim to these queues; empty means any
	// queue the TaskKeys appear in. Authorization of which queues a
	// claimant may pull from is the jobsauth integration's job.
	QueueKeys []string
	Limit     int
	// ClaimTTL is how long the claim lasts without a heartbeat.
	ClaimTTL time.Duration
}

// HeartbeatResult reports whether the heartbeat extended a live claim.
type HeartbeatResult struct {
	// OK is false when the claim is gone: the job was reclaimed under a
	// newer attempt, finished, or cancelled. The executor must stop.
	OK bool
	// CancelRequested tells the executor that someone asked for
	// cancellation; stopping is cooperative.
	CancelRequested bool
}

// FailRequest reports a failed attempt.
type FailRequest struct {
	JobID   uuid.UUID
	Attempt int // the claim token
	Error   string
	// Terminal means never retry (an authority denial).
	Terminal bool
	// RetryDelay is how long to wait before the next attempt, if there
	// will be one. Computed by the caller from the job's BackoffPolicy.
	RetryDelay time.Duration
}

// FailResult reports what the failure led to.
type FailResult struct {
	// Applied is false when the claim was no longer this attempt's.
	Applied bool
	State   State
}

// CancelResult reports what a cancel request did.
type CancelResult struct {
	// Applied is false when the job was not cancellable: unknown, or
	// already in a terminal state.
	Applied bool
	// State is the state after the request: cancelled for a pending job,
	// still claimed (with CancelRequested set) for one in progress.
	State State
}

// ReapResult counts what a housekeeping pass moved.
type ReapResult struct {
	ExpiredPending  int64 // pending past ExpiresAt -> dead
	LapsedCancelled int64 // claim lapsed with a cancel pending -> cancelled
	LapsedDead      int64 // claim lapsed with no retry available -> dead
}

// ListFilter narrows ListJobs. Zero values mean "no restriction".
type ListFilter struct {
	QueueKeys   []string
	States      []State
	RequestedBy *uuid.UUID
	Limit       int // default 100, capped at 1000
}
