-- scope_type/scope_id are plain strings, deliberately unvalidated
-- against any enum here — see 14-vitals-model.md's "no Scope type".
CREATE TABLE IF NOT EXISTS vitals_instances (
	instance_id       uuid PRIMARY KEY,
	scope_type        text NOT NULL,
	scope_id          text NOT NULL,
	instance_key      text NOT NULL,
	definition_id     uuid NOT NULL REFERENCES vitals_definitions (definition_id),
	subject_type      text NOT NULL DEFAULT '',
	subject_key       text NOT NULL DEFAULT '',
	display_name      text NOT NULL DEFAULT '',
	description       text NOT NULL DEFAULT '',
	category_path     text[] NOT NULL DEFAULT '{}',
	expected_states   text[] NOT NULL DEFAULT '{}',
	importance        text NOT NULL DEFAULT '',
	reference_ranges  jsonb NOT NULL DEFAULT '{}'::jsonb,
	display_hints     jsonb NOT NULL DEFAULT '{}'::jsonb,
	resource_uri      text NOT NULL DEFAULT '',
	external_uri      text NOT NULL DEFAULT '',
	detail_route      text NOT NULL DEFAULT '',
	app_metadata      jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at        timestamptz NOT NULL DEFAULT now(),
	updated_at        timestamptz NOT NULL DEFAULT now(),
	archived_at       timestamptz NULL,
	UNIQUE (scope_type, scope_id, instance_key)
);
