-- The domain-level record of a task kind (20-jobs-model.md). Separate
-- from the handler, which is code on an executor node, and separate
-- from endpoint definitions: being able to perform a kind of job does
-- not require exposing an endpoint for it.
CREATE TABLE IF NOT EXISTS jobs_task_definitions (
	task_key                   text PRIMARY KEY,
	description                text NOT NULL DEFAULT '',
	params                     jsonb NOT NULL DEFAULT '[]'::jsonb,
	default_queue_key          text NOT NULL,
	scope                      jsonb NOT NULL DEFAULT '[]'::jsonb,
	idempotent                 boolean NOT NULL DEFAULT false,
	default_max_attempts       int NOT NULL,
	default_attempt_timeout_ms bigint NOT NULL,
	default_backoff            jsonb NULL,
	priority_cap               smallint NOT NULL,
	metadata                   jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at                 timestamptz NOT NULL DEFAULT now()
);
