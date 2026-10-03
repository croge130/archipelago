-- One row per instance_id, upserted on every write. No FK on
-- source_instance_id/actor_principal_id: they reference registry's
-- Instance and Gatehouse-core's Principal, different bases Vitals
-- itself never imports — see 14-vitals-model.md.
CREATE TABLE IF NOT EXISTS vitals_current_readings (
	instance_id         uuid PRIMARY KEY REFERENCES vitals_instances (instance_id),
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
	revision            bigint NOT NULL DEFAULT 1
);
