package dbstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/jobs/structure"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresReader implements facade.Reader directly against Postgres.
type PostgresReader struct {
	pool *pgxpool.Pool
}

func NewPostgresReader(pool *pgxpool.Pool) *PostgresReader {
	return &PostgresReader{pool: pool}
}

const taskDefinitionFields = `task_key, description, params, default_queue_key, scope, idempotent,
	default_max_attempts, default_attempt_timeout_ms, default_backoff, priority_cap, metadata`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTaskDefinition(row rowScanner) (structure.TaskDefinition, error) {
	var d structure.TaskDefinition
	var params, scope, metadata []byte
	var backoff []byte
	var timeoutMS int64
	var priorityCap int16
	if err := row.Scan(&d.TaskKey, &d.Description, &params, &d.DefaultQueueKey, &scope, &d.Idempotent,
		&d.DefaultMaxAttempts, &timeoutMS, &backoff, &priorityCap, &metadata); err != nil {
		return structure.TaskDefinition{}, err
	}
	if err := json.Unmarshal(params, &d.Params); err != nil {
		return structure.TaskDefinition{}, fmt.Errorf("params: %w", err)
	}
	if err := json.Unmarshal(scope, &d.Scope); err != nil {
		return structure.TaskDefinition{}, fmt.Errorf("scope: %w", err)
	}
	if len(backoff) > 0 {
		if err := json.Unmarshal(backoff, &d.DefaultBackoff); err != nil {
			return structure.TaskDefinition{}, fmt.Errorf("default_backoff: %w", err)
		}
	}
	d.DefaultAttemptTimeout = msDuration(timeoutMS)
	pc, err := structure.PriorityFromRank(int(priorityCap))
	if err != nil {
		return structure.TaskDefinition{}, err
	}
	d.PriorityCap = pc
	d.Metadata = metadata
	return d, nil
}

func (r *PostgresReader) GetTaskDefinition(ctx context.Context, key string) (structure.TaskDefinition, bool, error) {
	d, err := scanTaskDefinition(r.pool.QueryRow(ctx,
		`SELECT `+taskDefinitionFields+` FROM jobs_task_definitions WHERE task_key = $1`, key))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.TaskDefinition{}, false, nil
		}
		return structure.TaskDefinition{}, false, fmt.Errorf("dbstore: get task definition: %w", err)
	}
	return d, true, nil
}

func (r *PostgresReader) ListTaskDefinitions(ctx context.Context) ([]structure.TaskDefinition, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+taskDefinitionFields+` FROM jobs_task_definitions ORDER BY task_key`)
	if err != nil {
		return nil, fmt.Errorf("dbstore: list task definitions: %w", err)
	}
	defer rows.Close()
	var out []structure.TaskDefinition
	for rows.Next() {
		d, err := scanTaskDefinition(rows)
		if err != nil {
			return nil, fmt.Errorf("dbstore: list task definitions: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// jobFields is the column list every job read and the claim's RETURNING
// share, so scanJob has exactly one shape to scan.
const jobFields = `job_id, task_key, queue_key, params, params_hash, state, priority, target_instance_id,
	idempotency_key, run_at, expires_at, retryable, max_attempts, attempt_timeout_ms,
	backoff_kind, backoff_base_ms, backoff_max_ms, attempt, claimed_by, claimed_until, cancel_requested,
	requested_by, authority_mode, run_as, actor_principal_id, assumed_session_id, authority_source_ref,
	trace_id, span_id, parent_span_id, result, last_error, created_at, updated_at, finished_at`

func scanJob(row rowScanner) (structure.Job, error) {
	var j structure.Job
	var jobID, requestedBy string
	var target, claimedBy, runAs, actor, session, traceID, spanID, parentSpanID *string
	var state, authorityMode, backoffKind string
	var priority int16
	var timeoutMS, backoffBaseMS, backoffMaxMS int64
	var params, result []byte
	if err := row.Scan(&jobID, &j.TaskKey, &j.QueueKey, &params, &j.ParamsHash, &state, &priority, &target,
		&j.IdempotencyKey, &j.RunAt, &j.ExpiresAt, &j.Retryable, &j.MaxAttempts, &timeoutMS,
		&backoffKind, &backoffBaseMS, &backoffMaxMS, &j.Attempt, &claimedBy, &j.ClaimedUntil, &j.CancelRequested,
		&requestedBy, &authorityMode, &runAs, &actor, &session, &j.AuthoritySourceRef,
		&traceID, &spanID, &parentSpanID, &result, &j.LastError, &j.CreatedAt, &j.UpdatedAt, &j.FinishedAt); err != nil {
		return structure.Job{}, err
	}
	var err error
	if j.JobID, err = parseUUID(jobID); err != nil {
		return structure.Job{}, fmt.Errorf("job_id: %w", err)
	}
	if j.RequestedBy, err = parseUUID(requestedBy); err != nil {
		return structure.Job{}, fmt.Errorf("requested_by: %w", err)
	}
	for _, f := range []struct {
		dst  **uuid.UUID
		src  *string
		name string
	}{
		{&j.TargetInstanceID, target, "target_instance_id"}, {&j.ClaimedBy, claimedBy, "claimed_by"},
		{&j.RunAs, runAs, "run_as"}, {&j.ActorPrincipalID, actor, "actor_principal_id"},
		{&j.AssumedSessionID, session, "assumed_session_id"},
	} {
		if *f.dst, err = parseNullableUUID(f.src); err != nil {
			return structure.Job{}, fmt.Errorf("%s: %w", f.name, err)
		}
	}
	if j.Priority, err = structure.PriorityFromRank(int(priority)); err != nil {
		return structure.Job{}, err
	}
	if j.TraceContext, err = spanContextFromText(traceID, spanID, parentSpanID); err != nil {
		return structure.Job{}, err
	}
	j.Params = params
	j.Result = result
	j.State = structure.State(state)
	j.AuthorityMode = structure.AuthorityMode(authorityMode)
	j.AttemptTimeout = msDuration(timeoutMS)
	j.Backoff = structure.BackoffPolicy{Kind: structure.BackoffKind(backoffKind), Base: msDuration(backoffBaseMS), Max: msDuration(backoffMaxMS)}
	return j, nil
}

func (r *PostgresReader) GetJob(ctx context.Context, id uuid.UUID) (structure.Job, bool, error) {
	j, err := scanJob(r.pool.QueryRow(ctx, `SELECT `+jobFields+` FROM jobs_jobs WHERE job_id = $1`, uuidToText(id)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Job{}, false, nil
		}
		return structure.Job{}, false, fmt.Errorf("dbstore: get job: %w", err)
	}
	return j, true, nil
}

func (r *PostgresReader) GetJobByIdempotencyKey(ctx context.Context, taskKey, key string) (structure.Job, bool, error) {
	if key == "" {
		return structure.Job{}, false, nil
	}
	j, err := scanJob(r.pool.QueryRow(ctx,
		`SELECT `+jobFields+` FROM jobs_jobs WHERE task_key = $1 AND idempotency_key = $2`, taskKey, key))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Job{}, false, nil
		}
		return structure.Job{}, false, fmt.Errorf("dbstore: get job by idempotency key: %w", err)
	}
	return j, true, nil
}

func (r *PostgresReader) ListJobs(ctx context.Context, f structure.ListFilter) ([]structure.Job, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	states := make([]string, len(f.States))
	for i, s := range f.States {
		states[i] = string(s)
	}
	queues := f.QueueKeys
	if queues == nil {
		queues = []string{}
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+jobFields+` FROM jobs_jobs
		 WHERE (cardinality($1::text[]) = 0 OR queue_key = ANY($1))
		   AND (cardinality($2::text[]) = 0 OR state = ANY($2))
		   AND ($3::uuid IS NULL OR requested_by = $3::uuid)
		 ORDER BY created_at DESC, job_id
		 LIMIT $4`,
		queues, states, nullableUUIDToText(f.RequestedBy), limit)
	if err != nil {
		return nil, fmt.Errorf("dbstore: list jobs: %w", err)
	}
	defer rows.Close()
	var out []structure.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("dbstore: list jobs: %w", err)
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// CountJobsByState counts one queue's jobs per state, for reporting
// queue depth to Vitals. States with no jobs are absent from the map.
func (r *PostgresReader) CountJobsByState(ctx context.Context, queueKey string) (map[structure.State]int, error) {
	rows, err := r.pool.Query(ctx, `SELECT state, count(*) FROM jobs_jobs WHERE queue_key = $1 GROUP BY state`, queueKey)
	if err != nil {
		return nil, fmt.Errorf("dbstore: count jobs by state: %w", err)
	}
	defer rows.Close()
	out := map[structure.State]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, fmt.Errorf("dbstore: count jobs by state: %w", err)
		}
		out[structure.State(s)] = n
	}
	return out, rows.Err()
}
