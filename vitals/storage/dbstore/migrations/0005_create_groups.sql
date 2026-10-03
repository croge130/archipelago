CREATE TABLE IF NOT EXISTS vitals_groups (
	group_id       uuid PRIMARY KEY,
	scope_type     text NOT NULL,
	scope_id       text NOT NULL,
	group_key      text NOT NULL,
	title          text NOT NULL DEFAULT '',
	description    text NOT NULL DEFAULT '',
	sort_order     int NOT NULL DEFAULT 0,
	display_hints  jsonb NOT NULL DEFAULT '{}'::jsonb,
	app_metadata   jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at     timestamptz NOT NULL DEFAULT now(),
	updated_at     timestamptz NOT NULL DEFAULT now(),
	archived_at    timestamptz NULL,
	UNIQUE (scope_type, scope_id, group_key)
);
