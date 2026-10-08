package facade

import (
	"context"
	"encoding/json"
	"time"

	"github.com/croge130/archipelago/jobs/structure"
	"github.com/google/uuid"
)

// Reader is everything the facade needs to read.
type Reader interface {
	GetTaskDefinition(ctx context.Context, key string) (structure.TaskDefinition, bool, error)
	ListTaskDefinitions(ctx context.Context) ([]structure.TaskDefinition, error)
	GetJob(ctx context.Context, id uuid.UUID) (structure.Job, bool, error)
	GetJobByIdempotencyKey(ctx context.Context, taskKey, key string) (structure.Job, bool, error)
	ListJobs(ctx context.Context, f structure.ListFilter) ([]structure.Job, error)
	CountJobsByState(ctx context.Context, queueKey string) (map[structure.State]int, error)
}

// Writer is every mutation, declared here (its consumer) and made of
// coarse commands — each one atomic — so a remote implementation of the
// same interface is feasible (02-package-boundaries.md). Every deadline
// in these commands is measured by the store's own clock.
type Writer interface {
	RegisterTaskDefinition(ctx context.Context, d structure.TaskDefinition) error
	EnqueueJob(ctx context.Context, j structure.Job, runAfter, expiresAfter time.Duration) (created bool, err error)
	ClaimJobs(ctx context.Context, req structure.ClaimRequest) ([]structure.Job, error)
	AttachClaimIdentity(ctx context.Context, jobID uuid.UUID, attempt int, assumedSessionID *uuid.UUID) (bool, error)
	HeartbeatJob(ctx context.Context, jobID uuid.UUID, attempt int, extendBy time.Duration) (structure.HeartbeatResult, error)
	CompleteJob(ctx context.Context, jobID uuid.UUID, attempt int, result json.RawMessage) (bool, error)
	FailJob(ctx context.Context, req structure.FailRequest) (structure.FailResult, error)
	CancelJob(ctx context.Context, jobID uuid.UUID) (structure.CancelResult, error)
	ReapJobs(ctx context.Context) (structure.ReapResult, error)
	PruneFinishedJobs(ctx context.Context, olderThan time.Duration) (int64, error)
}
