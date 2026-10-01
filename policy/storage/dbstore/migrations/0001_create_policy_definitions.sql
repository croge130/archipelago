-- value_type and constraints are serialized typedvalue.Definition /
-- typeconstraints.Set — jsonb, never persisted as a derived dimension
-- vector (typedvalue's own "derive, don't persist" rule).
CREATE TABLE IF NOT EXISTS policy_definitions (
	policy_definition_id uuid PRIMARY KEY,
	policy_key           text NOT NULL UNIQUE,
	value_type           jsonb NOT NULL,
	constraints          jsonb NOT NULL,
	merge_mode           text NOT NULL,
	activation           text NOT NULL,
	default_binding      text NOT NULL,
	lifecycle            text NOT NULL,
	created_by           text NOT NULL DEFAULT '',
	updated_by           text NOT NULL DEFAULT '',
	created_at           timestamptz NOT NULL DEFAULT now(),
	updated_at           timestamptz NOT NULL DEFAULT now()
);
