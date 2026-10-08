-- No foreign keys to principals or instances: they belong to
-- Gatehouse-core, a different base this one never imports (the same
-- rule vitals follows for actor_principal_id and source_instance_id).
-- All timestamps are set by the database's own clock (now()), never a
-- node's, so claim and expiry comparisons cannot be skewed by a node
-- whose clock disagrees.
CREATE TABLE IF NOT EXISTS jobs_jobs (
	job_id              uuid PRIMARY KEY,
	task_key            text NOT NULL REFERENCES jobs_task_definitions (task_key),
	queue_key           text NOT NULL,
	params              jsonb NOT NULL DEFAULT '{}'::jsonb,
	params_hash         text NOT NULL,
	state               text NOT NULL,
	priority            smallint NOT NULL,
	target_instance_id  uuid NULL,
	idempotency_key     text NOT NULL DEFAULT '',
	run_at              timestamptz NOT NULL,
	expires_at          timestamptz NULL,
	retryable           boolean NOT NULL,
	max_attempts        int NOT NULL,
	attempt_timeout_ms  bigint NOT NULL,
	backoff_kind        text NOT NULL,
	backoff_base_ms     bigint NOT NULL,
	backoff_max_ms      bigint NOT NULL DEFAULT 0,
	attempt             int NOT NULL DEFAULT 0,
	claimed_by          uuid NULL,
	claimed_until       timestamptz NULL,
	cancel_requested    boolean NOT NULL DEFAULT false,
	requested_by        uuid NOT NULL,
	authority_mode      text NOT NULL,
	run_as              uuid NULL,
	actor_principal_id  uuid NULL,
	assumed_session_id  uuid NULL,
	authority_source_ref text NOT NULL DEFAULT '',
	trace_id            text NULL,
	span_id             text NULL,
	parent_span_id      text NULL,
	result              jsonb NULL,
	last_error          text NOT NULL DEFAULT '',
	created_at          timestamptz NOT NULL,
	updated_at          timestamptz NOT NULL,
	finished_at         timestamptz NULL
);

-- Idempotent submission: one job per (task, key). Rows with no key
-- (the empty string) are exempt.
CREATE UNIQUE INDEX IF NOT EXISTS jobs_jobs_idempotency
	ON jobs_jobs (task_key, idempotency_key)
	WHERE idempotency_key <> '';

-- The claim query: due pending jobs and lapsed claims, by priority
-- then run_at. Terminal rows fall out of the partial index.
CREATE INDEX IF NOT EXISTS jobs_jobs_claimable
	ON jobs_jobs (queue_key, priority DESC, run_at)
	WHERE state IN ('pending', 'claimed');

CREATE INDEX IF NOT EXISTS jobs_jobs_requested_by
	ON jobs_jobs (requested_by, created_at DESC);

CREATE INDEX IF NOT EXISTS jobs_jobs_finished
	ON jobs_jobs (finished_at)
	WHERE finished_at IS NOT NULL;
