-- No owner_kind/owner_app_id column: ownership is the reserved-
-- namespace convention, not a stored field. See 14-vitals-model.md.
CREATE TABLE IF NOT EXISTS vitals_definitions (
	definition_id            uuid PRIMARY KEY,
	definition_key           text NOT NULL,
	definition_version       int NOT NULL,
	schema_hash              text NOT NULL DEFAULT '',
	title                    text NOT NULL DEFAULT '',
	description              text NOT NULL DEFAULT '',
	value_metadata           jsonb NULL,
	app_value_metadata       jsonb NOT NULL DEFAULT '{}'::jsonb,
	allowed_states           text[] NOT NULL DEFAULT '{}',
	default_expected_states  text[] NOT NULL DEFAULT '{}',
	default_importance       text NOT NULL DEFAULT '',
	default_ttl_seconds      bigint NULL,
	default_display_hints    jsonb NOT NULL DEFAULT '{}'::jsonb,
	app_metadata             jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at               timestamptz NOT NULL DEFAULT now(),
	updated_at               timestamptz NOT NULL DEFAULT now(),
	archived_at              timestamptz NULL,
	UNIQUE (definition_key, definition_version)
);
