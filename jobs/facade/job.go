package facade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/jobs/evaluation"
	"github.com/croge130/archipelago/jobs/structure"
	"github.com/croge130/archipelago/logging"
	"github.com/google/uuid"
)

var (
	ErrUnknownTask = errors.New("facade: jobs: unknown task kind")
	ErrJobNotFound = errors.New("facade: jobs: job not found")
	ErrJobFinished = errors.New("facade: jobs: job is already finished")
)

// SubmitRequest is a request to run a task kind with parameters. Fields
// left zero take the task definition's defaults.
type SubmitRequest struct {
	TaskKey  string
	QueueKey string // default: the definition's DefaultQueueKey
	Params   json.RawMessage

	// Priority is a hint, clamped to the definition's PriorityCap.
	// Default: normal (or the cap, if the cap is lower).
	Priority         structure.Priority
	TargetInstanceID *uuid.UUID

	// IdempotencyKey makes submission safe to repeat: a second submission
	// under the same (task, key) returns the first job.
	IdempotencyKey string

	// RunAfter delays the earliest start; ExpiresAfter bounds how long
	// the job may sit pending (zero: never). Both use the store's clock.
	RunAfter     time.Duration
	ExpiresAfter time.Duration

	// RequestedBy is B, the owner. AuthorityMode and RunAs follow
	// structure.Job's rules; default mode is owner.
	RequestedBy        uuid.UUID
	AuthorityMode      structure.AuthorityMode
	RunAs              *uuid.UUID
	AuthoritySourceRef string

	TraceContext *logging.SpanContext
}

// SubmitResult reports the job, and whether this call created it.
type SubmitResult struct {
	Job     structure.Job
	Created bool
}

// Submit validates a request against its task definition and enqueues a
// job. It checks the kind exists and its parameters conform, fixes the
// parameters' hash so authority cannot later be applied to different
// ones, copies the definition's retry rules onto the job (so claiming
// and failing never need to join the definition), and enqueues.
//
// It does not authorize: whether RequestedBy may submit to the queue, or
// act as RunAs, is jobsauth's check, made before this is called.
func Submit(ctx context.Context, reader Reader, writer Writer, req SubmitRequest) (SubmitResult, error) {
	def, found, err := reader.GetTaskDefinition(ctx, req.TaskKey)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("facade: submit: %w", err)
	}
	if !found {
		return SubmitResult{}, fmt.Errorf("%w: %q", ErrUnknownTask, req.TaskKey)
	}
	params, hash, err := evaluation.NormalizeParams(def, req.Params)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("facade: submit: %w", err)
	}

	queue := req.QueueKey
	if queue == "" {
		queue = def.DefaultQueueKey
	}
	priority := req.Priority
	if priority == "" {
		priority = structure.PriorityNormal
	}
	if !priority.Valid() {
		return SubmitResult{}, fmt.Errorf("facade: submit: invalid priority %q", priority)
	}
	priority = structure.MinPriority(priority, def.PriorityCap)
	mode := req.AuthorityMode
	if mode == "" {
		mode = structure.AuthorityOwner
	}

	job := structure.Job{
		JobID:              uuid.New(),
		TaskKey:            def.TaskKey,
		QueueKey:           queue,
		Params:             params,
		ParamsHash:         hash,
		State:              structure.StatePending,
		Priority:           priority,
		TargetInstanceID:   req.TargetInstanceID,
		IdempotencyKey:     req.IdempotencyKey,
		Retryable:          def.Idempotent,
		MaxAttempts:        def.EffectiveMaxAttempts(),
		AttemptTimeout:     def.DefaultAttemptTimeout,
		Backoff:            def.EffectiveBackoff(),
		RequestedBy:        req.RequestedBy,
		AuthorityMode:      mode,
		RunAs:              req.RunAs,
		AuthoritySourceRef: req.AuthoritySourceRef,
		TraceContext:       req.TraceContext,
	}
	if err := job.Validate(); err != nil {
		return SubmitResult{}, fmt.Errorf("facade: submit: %w", err)
	}
	if req.RunAfter < 0 || req.ExpiresAfter < 0 {
		return SubmitResult{}, fmt.Errorf("facade: submit: RunAfter and ExpiresAfter must not be negative")
	}

	created, err := writer.EnqueueJob(ctx, job, req.RunAfter, req.ExpiresAfter)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("facade: submit: %w", err)
	}
	if created {
		stored, found, err := reader.GetJob(ctx, job.JobID)
		if err != nil || !found {
			// The insert succeeded; the read-back is only for the
			// database-stamped times. Return what we have rather than
			// report a failure for a job that exists.
			return SubmitResult{Job: job, Created: true}, nil
		}
		return SubmitResult{Job: stored, Created: true}, nil
	}

	// An idempotency key matched an existing job. Same task and same
	// parameters means this is a repeat; anything else is a conflict.
	existing, found, err := reader.GetJobByIdempotencyKey(ctx, def.TaskKey, req.IdempotencyKey)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("facade: submit: %w", err)
	}
	if !found {
		return SubmitResult{}, fmt.Errorf("facade: submit: idempotency key matched a job that has since been pruned; retry")
	}
	if existing.ParamsHash != hash {
		return SubmitResult{}, ErrConflict
	}
	return SubmitResult{Job: existing, Created: false}, nil
}

// FailAttempt reports a failed attempt on a claimed job. It computes the
// retry delay from the job's own backoff policy and the attempt number,
// so every caller retries on the same schedule, then asks the store to
// decide. terminal marks a failure that must never be retried (an
// authority denial).
func FailAttempt(ctx context.Context, writer Writer, job structure.Job, cause error, terminal bool) (structure.FailResult, error) {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	res, err := writer.FailJob(ctx, structure.FailRequest{
		JobID:      job.JobID,
		Attempt:    job.Attempt,
		Error:      msg,
		Terminal:   terminal,
		RetryDelay: evaluation.Delay(job.Backoff, job.Attempt),
	})
	if err != nil {
		return structure.FailResult{}, fmt.Errorf("facade: fail attempt: %w", err)
	}
	return res, nil
}

// Cancel requests cancellation. A pending job is cancelled outright; a
// claimed one is flagged so its executor learns on its next heartbeat
// (cancellation is cooperative — nothing kills running code). It tells
// "no such job" apart from "already finished", which the store alone
// cannot.
func Cancel(ctx context.Context, reader Reader, writer Writer, jobID uuid.UUID) (structure.CancelResult, error) {
	res, err := writer.CancelJob(ctx, jobID)
	if err != nil {
		return structure.CancelResult{}, fmt.Errorf("facade: cancel: %w", err)
	}
	if res.Applied {
		return res, nil
	}
	job, found, err := reader.GetJob(ctx, jobID)
	if err != nil {
		return structure.CancelResult{}, fmt.Errorf("facade: cancel: %w", err)
	}
	if !found {
		return structure.CancelResult{}, ErrJobNotFound
	}
	return structure.CancelResult{State: job.State}, fmt.Errorf("%w (%s)", ErrJobFinished, job.State)
}
