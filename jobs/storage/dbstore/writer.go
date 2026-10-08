package dbstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/jobs/structure"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresWriter implements facade.Writer directly against Postgres.
type PostgresWriter struct {
	pool *pgxpool.Pool
}

func NewPostgresWriter(pool *pgxpool.Pool) *PostgresWriter {
	return &PostgresWriter{pool: pool}
}

// msInterval renders "$n milliseconds from now" in SQL; every deadline
// below is computed from the database's now(), never a node's clock.
const msInterval = `* interval '1 millisecond'`

// RegisterTaskDefinition inserts a new task definition — a raw insert;
// facade.RegisterTaskDefinition decides idempotency and conflicts
// before calling it, the split every Register*/Ensure* in this design
// uses.
func (w *PostgresWriter) RegisterTaskDefinition(ctx context.Context, d structure.TaskDefinition) error {
	if err := d.Validate(); err != nil {
		return err
	}
	params, err := marshalList(d.Params)
	if err != nil {
		return fmt.Errorf("dbstore: register task definition: params: %w", err)
	}
	scope, err := marshalList(d.Scope)
	if err != nil {
		return fmt.Errorf("dbstore: register task definition: scope: %w", err)
	}
	var backoff []byte
	if d.DefaultBackoff.Kind != "" {
		if backoff, err = json.Marshal(d.DefaultBackoff); err != nil {
			return fmt.Errorf("dbstore: register task definition: backoff: %w", err)
		}
	}
	_, err = w.pool.Exec(ctx,
		`INSERT INTO jobs_task_definitions (
			task_key, description, params, default_queue_key, scope, idempotent,
			default_max_attempts, default_attempt_timeout_ms, default_backoff, priority_cap, metadata
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		d.TaskKey, d.Description, params, d.DefaultQueueKey, scope, d.Idempotent,
		d.DefaultMaxAttempts, durationMS(d.DefaultAttemptTimeout), backoff, priorityRank(d.PriorityCap), nullableJSON(d.Metadata))
	if err != nil {
		return fmt.Errorf("dbstore: register task definition: %w", err)
	}
	return nil
}

// EnqueueJob inserts j as pending. runAfter delays the earliest start
// and expiresAfter (zero meaning never) bounds how long it may sit
// pending; both are measured from the database's clock, which also
// stamps created_at. created is false when an idempotency key matched
// an existing job: nothing was written, and the caller reads the
// existing row.
func (w *PostgresWriter) EnqueueJob(ctx context.Context, j structure.Job, runAfter, expiresAfter time.Duration) (created bool, err error) {
	if err := j.Validate(); err != nil {
		return false, err
	}
	traceID, spanID, parentSpanID := spanContextToText(j.TraceContext)
	var expiresMS *int64
	if expiresAfter > 0 {
		ms := durationMS(expiresAfter)
		expiresMS = &ms
	}
	var id string
	err = w.pool.QueryRow(ctx,
		`INSERT INTO jobs_jobs (
			job_id, task_key, queue_key, params, params_hash, state, priority, target_instance_id,
			idempotency_key, run_at, expires_at, retryable, max_attempts, attempt_timeout_ms,
			backoff_kind, backoff_base_ms, backoff_max_ms, requested_by, authority_mode, run_as,
			authority_source_ref, trace_id, span_id, parent_span_id, created_at, updated_at
		 ) VALUES (
			$1,$2,$3,$4,$5,'pending',$6,$7,
			$8, now() + ($9::bigint `+msInterval+`),
			CASE WHEN $10::bigint IS NULL THEN NULL ELSE now() + ($10::bigint `+msInterval+`) END,
			$11,$12,$13,
			$14,$15,$16,$17,$18,$19,
			$20,$21,$22,$23, now(), now()
		 )
		 ON CONFLICT (task_key, idempotency_key) WHERE idempotency_key <> '' DO NOTHING
		 RETURNING job_id`,
		uuidToText(j.JobID), j.TaskKey, j.QueueKey, nullableJSON(j.Params), j.ParamsHash, priorityRank(j.Priority), nullableUUIDToText(j.TargetInstanceID),
		j.IdempotencyKey, durationMS(runAfter), expiresMS,
		j.Retryable, j.MaxAttempts, durationMS(j.AttemptTimeout),
		string(j.Backoff.Kind), durationMS(j.Backoff.Base), durationMS(j.Backoff.Max), uuidToText(j.RequestedBy), string(j.AuthorityMode), nullableUUIDToText(j.RunAs),
		j.AuthoritySourceRef, traceID, spanID, parentSpanID,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("dbstore: enqueue job: %w", err)
	}
	return true, nil
}

// ClaimJobs atomically claims up to req.Limit jobs the claimer can
// serve, and returns them. One statement decides everything: candidates
// are selected FOR UPDATE SKIP LOCKED, so many executors claim
// concurrently without blocking or double-claiming, and the same
// statement increments attempt (the claim token) and stamps the claim's
// expiry from the database's clock.
//
// Two kinds of job are claimable: pending jobs whose run_at has come
// (and which have not expired), and jobs whose previous claim lapsed
// — but only if they are retryable, have attempts left, and nobody
// asked to cancel them. A lapsed claim that fails any of those is left
// for ReapJobs to move to its terminal state; it is never silently run
// again.
func (w *PostgresWriter) ClaimJobs(ctx context.Context, req structure.ClaimRequest) ([]structure.Job, error) {
	if len(req.TaskKeys) == 0 {
		return nil, fmt.Errorf("dbstore: claim jobs: TaskKeys is required: a node claims only the kinds it can run")
	}
	if req.Limit <= 0 {
		return nil, fmt.Errorf("dbstore: claim jobs: Limit must be positive")
	}
	if req.ClaimTTL <= 0 {
		return nil, fmt.Errorf("dbstore: claim jobs: ClaimTTL must be positive")
	}
	queues := req.QueueKeys
	if queues == nil {
		queues = []string{}
	}
	rows, err := w.pool.Query(ctx,
		`WITH due AS (
			SELECT job_id FROM jobs_jobs
			WHERE task_key = ANY($1)
			  AND (cardinality($2::text[]) = 0 OR queue_key = ANY($2))
			  AND (target_instance_id IS NULL OR target_instance_id = $3::uuid)
			  AND (
				(state = 'pending' AND run_at <= now() AND (expires_at IS NULL OR expires_at > now()))
				OR
				(state = 'claimed' AND claimed_until < now() AND retryable AND attempt < max_attempts AND NOT cancel_requested)
			  )
			ORDER BY priority DESC, run_at ASC, job_id
			LIMIT $4
			FOR UPDATE SKIP LOCKED
		 )
		 UPDATE jobs_jobs j SET
			state = 'claimed',
			attempt = j.attempt + 1,
			claimed_by = $3::uuid,
			claimed_until = now() + ($5::bigint `+msInterval+`),
			actor_principal_id = $6::uuid,
			assumed_session_id = NULL,
			updated_at = now()
		 FROM due WHERE j.job_id = due.job_id
		 RETURNING `+qualifiedJobFields("j"),
		req.TaskKeys, queues, uuidToText(req.ClaimerInstanceID), req.Limit, durationMS(req.ClaimTTL), nullableUUIDToText(req.ActorPrincipalID))
	if err != nil {
		return nil, fmt.Errorf("dbstore: claim jobs: %w", err)
	}
	defer rows.Close()
	var out []structure.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("dbstore: claim jobs: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dbstore: claim jobs: %w", err)
	}
	// RETURNING order is unspecified; hand them back most urgent first.
	sortByUrgency(out)
	return out, nil
}

// AttachClaimIdentity records, on a job this attempt currently holds,
// the assumed session minted for the claim. It is refused (false) if the
// claim is no longer this attempt's, so a stale claimant cannot attach
// a session to a job someone else now holds.
func (w *PostgresWriter) AttachClaimIdentity(ctx context.Context, jobID uuid.UUID, attempt int, assumedSessionID *uuid.UUID) (bool, error) {
	tag, err := w.pool.Exec(ctx,
		`UPDATE jobs_jobs SET assumed_session_id = $3::uuid, updated_at = now()
		 WHERE job_id = $1 AND state = 'claimed' AND attempt = $2`,
		uuidToText(jobID), attempt, nullableUUIDToText(assumedSessionID))
	if err != nil {
		return false, fmt.Errorf("dbstore: attach claim identity: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// HeartbeatJob extends the claim named by (jobID, attempt) by extendBy
// from now. OK is false when that claim is gone — the executor must
// stop. CancelRequested is returned on every heartbeat, which is how a
// running executor learns it has been asked to stop.
func (w *PostgresWriter) HeartbeatJob(ctx context.Context, jobID uuid.UUID, attempt int, extendBy time.Duration) (structure.HeartbeatResult, error) {
	var cancel bool
	err := w.pool.QueryRow(ctx,
		`UPDATE jobs_jobs SET claimed_until = now() + ($3::bigint `+msInterval+`), updated_at = now()
		 WHERE job_id = $1 AND state = 'claimed' AND attempt = $2
		 RETURNING cancel_requested`,
		uuidToText(jobID), attempt, durationMS(extendBy)).Scan(&cancel)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.HeartbeatResult{}, nil
		}
		return structure.HeartbeatResult{}, fmt.Errorf("dbstore: heartbeat job: %w", err)
	}
	return structure.HeartbeatResult{OK: true, CancelRequested: cancel}, nil
}

// CompleteJob marks the job succeeded, but only if (jobID, attempt) is
// still the live claim — the attempt number is the fence that refuses a
// stale executor whose claim lapsed and was taken by another. A job
// with a cancel pending still succeeds if the executor finished anyway;
// cancellation is cooperative.
func (w *PostgresWriter) CompleteJob(ctx context.Context, jobID uuid.UUID, attempt int, result json.RawMessage) (bool, error) {
	if len(result) > structure.MaxResultBytes {
		return false, fmt.Errorf("dbstore: complete job: result is %d bytes, over the %d limit", len(result), structure.MaxResultBytes)
	}
	var res any
	if len(result) > 0 {
		res = []byte(result)
	}
	tag, err := w.pool.Exec(ctx,
		`UPDATE jobs_jobs SET state = 'succeeded', result = $3, last_error = '',
			claimed_until = NULL, finished_at = now(), updated_at = now()
		 WHERE job_id = $1 AND state = 'claimed' AND attempt = $2`,
		uuidToText(jobID), attempt, res)
	if err != nil {
		return false, fmt.Errorf("dbstore: complete job: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// FailJob reports a failed attempt. The decision — dead, cancelled or
// back to pending after a delay — is one expression in one statement,
// mirroring evaluation.AfterFailure exactly (the integration tests run
// both against every combination). Like CompleteJob, it only applies to
// the live claim.
func (w *PostgresWriter) FailJob(ctx context.Context, req structure.FailRequest) (structure.FailResult, error) {
	var state string
	err := w.pool.QueryRow(ctx,
		`UPDATE jobs_jobs j SET
			state = d.next_state,
			run_at = CASE WHEN d.next_state = 'pending' THEN now() + ($5::bigint `+msInterval+`) ELSE j.run_at END,
			last_error = $4,
			claimed_by = NULL,
			claimed_until = NULL,
			finished_at = CASE WHEN d.next_state = 'pending' THEN NULL ELSE now() END,
			updated_at = now()
		 FROM (
			SELECT job_id,
			       CASE WHEN cancel_requested THEN 'cancelled'
			            WHEN $3::boolean OR NOT retryable OR attempt >= max_attempts THEN 'dead'
			            ELSE 'pending' END AS next_state
			FROM jobs_jobs WHERE job_id = $1
		 ) d
		 WHERE j.job_id = d.job_id AND j.state = 'claimed' AND j.attempt = $2
		 RETURNING j.state`,
		uuidToText(req.JobID), req.Attempt, req.Terminal, structure.TruncateError(req.Error), durationMS(req.RetryDelay)).Scan(&state)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.FailResult{}, nil
		}
		return structure.FailResult{}, fmt.Errorf("dbstore: fail job: %w", err)
	}
	return structure.FailResult{Applied: true, State: structure.State(state)}, nil
}

// ReleaseJob hands a claim back without counting it as an attempt. It is
// for a claimant that took a job but turns out not to be the one to run
// it — for instance an executor that holds the queue's claim permission
// but is not permitted to execute as the job's principal. The job returns
// to pending with its attempt number restored, so the job's retries are
// not spent on a mismatch that says nothing about the work, and
// retryAfter keeps the same executor from immediately taking it again so
// that one who can run it gets the chance. If a cancel was requested in
// the meantime, the job is cancelled instead. Like every claimant
// operation it is refused unless (jobID, attempt) is the live claim.
func (w *PostgresWriter) ReleaseJob(ctx context.Context, jobID uuid.UUID, attempt int, retryAfter time.Duration) (structure.ReleaseResult, error) {
	var state string
	err := w.pool.QueryRow(ctx,
		`UPDATE jobs_jobs SET
			state = CASE WHEN cancel_requested THEN 'cancelled' ELSE 'pending' END,
			attempt = attempt - 1,
			run_at = CASE WHEN cancel_requested THEN run_at ELSE now() + ($3::bigint `+msInterval+`) END,
			claimed_by = NULL, claimed_until = NULL, assumed_session_id = NULL, actor_principal_id = NULL,
			finished_at = CASE WHEN cancel_requested THEN now() ELSE NULL END,
			updated_at = now()
		 WHERE job_id = $1 AND state = 'claimed' AND attempt = $2
		 RETURNING state`,
		uuidToText(jobID), attempt, durationMS(retryAfter)).Scan(&state)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.ReleaseResult{}, nil
		}
		return structure.ReleaseResult{}, fmt.Errorf("dbstore: release job: %w", err)
	}
	return structure.ReleaseResult{Applied: true, State: structure.State(state)}, nil
}

// CancelJob cancels a pending job outright, or flags a claimed one so
// its executor learns on its next heartbeat. A terminal or unknown job
// is not applied. Lighthouse's rule carries over: cancellation is
// cooperative and nothing here kills running code.
func (w *PostgresWriter) CancelJob(ctx context.Context, jobID uuid.UUID) (structure.CancelResult, error) {
	var state string
	err := w.pool.QueryRow(ctx,
		`UPDATE jobs_jobs SET
			state = CASE WHEN state = 'pending' THEN 'cancelled' ELSE state END,
			cancel_requested = true,
			finished_at = CASE WHEN state = 'pending' THEN now() ELSE finished_at END,
			updated_at = now()
		 WHERE job_id = $1 AND state IN ('pending', 'claimed')
		 RETURNING state`,
		uuidToText(jobID)).Scan(&state)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.CancelResult{}, nil
		}
		return structure.CancelResult{}, fmt.Errorf("dbstore: cancel job: %w", err)
	}
	return structure.CancelResult{Applied: true, State: structure.State(state)}, nil
}

// ReapJobs moves jobs that can no longer progress to their terminal
// states: pending jobs past their expiry, and lapsed claims that are
// not eligible for another attempt (cancel pending -> cancelled; not
// retryable or out of attempts -> dead). Claiming reclaims lazily, so
// without this a job whose last claim lapsed at the attempt limit would
// sit in 'claimed' forever. Each statement is idempotent, so any number
// of nodes may run it at once.
func (w *PostgresWriter) ReapJobs(ctx context.Context) (structure.ReapResult, error) {
	var r structure.ReapResult
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return r, fmt.Errorf("dbstore: reap jobs: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful Commit

	steps := []struct {
		dst *int64
		sql string
	}{
		{&r.ExpiredPending,
			`UPDATE jobs_jobs SET state = 'dead', last_error = 'expired before it could run', finished_at = now(), updated_at = now()
			 WHERE state = 'pending' AND expires_at IS NOT NULL AND expires_at <= now()`},
		{&r.LapsedCancelled,
			`UPDATE jobs_jobs SET state = 'cancelled', claimed_by = NULL, claimed_until = NULL, finished_at = now(), updated_at = now()
			 WHERE state = 'claimed' AND claimed_until < now() AND cancel_requested`},
		{&r.LapsedDead,
			`UPDATE jobs_jobs SET state = 'dead', last_error = 'claim expired with no retry available',
				claimed_by = NULL, claimed_until = NULL, finished_at = now(), updated_at = now()
			 WHERE state = 'claimed' AND claimed_until < now() AND (NOT retryable OR attempt >= max_attempts)`},
	}
	for _, s := range steps {
		tag, err := tx.Exec(ctx, s.sql)
		if err != nil {
			return structure.ReapResult{}, fmt.Errorf("dbstore: reap jobs: %w", err)
		}
		*s.dst = tag.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return structure.ReapResult{}, fmt.Errorf("dbstore: reap jobs: %w", err)
	}
	return r, nil
}

// PruneFinishedJobs deletes finished jobs older than olderThan by the
// database's clock. Idempotent, so many nodes may run it.
func (w *PostgresWriter) PruneFinishedJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := w.pool.Exec(ctx,
		`DELETE FROM jobs_jobs
		 WHERE state IN ('succeeded', 'dead', 'cancelled')
		   AND finished_at IS NOT NULL
		   AND finished_at < now() - ($1::bigint `+msInterval+`)`,
		durationMS(olderThan))
	if err != nil {
		return 0, fmt.Errorf("dbstore: prune finished jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}
