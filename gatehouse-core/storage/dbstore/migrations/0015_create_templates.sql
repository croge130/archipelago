CREATE TABLE IF NOT EXISTS gatehouse_templates (
	template_id uuid PRIMARY KEY,
	key         text NOT NULL UNIQUE,
	description text NOT NULL DEFAULT '',
	recipe      jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at  timestamptz NOT NULL DEFAULT now(),
	updated_at  timestamptz NOT NULL DEFAULT now()
)
