-- Captures notable transitions only — see evaluation.IsNotableTransition.
-- recorded_at is the history row's own insertion time, distinct from
-- updated_at (the reading's own transition timestamp), so listing can
-- paginate on a column that's always monotonically increasing by
-- insert order even if updated_at were ever backdated.
CREATE TABLE IF NOT EXISTS vitals_reading_history (
	history_id          uuid PRIMARY KEY,
	instance_id         uuid NOT NULL REFERENCES vitals_instances (instance_id),
	state               text NOT NULL,
	impact              text NOT NULL DEFAULT '',
	impact_score        int NULL,
	value               jsonb NULL,
	summary             text NOT NULL DEFAULT '',
	reason_code         text NOT NULL DEFAULT '',
	details_json        jsonb NOT NULL DEFAULT '{}'::jsonb,
	observed_at         timestamptz NULL,
	updated_at          timestamptz NOT NULL,
	expires_at          timestamptz NULL,
	source_instance_id  uuid NULL,
	actor_principal_id  uuid NULL,
	trace_id            text NULL,
	span_id             text NULL,
	parent_span_id      text NULL,
	quality_warnings    jsonb NOT NULL DEFAULT '[]'::jsonb,
	revision            bigint NOT NULL,
	recorded_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS vitals_reading_history_instance_recorded_idx
	ON vitals_reading_history (instance_id, recorded_at DESC);
