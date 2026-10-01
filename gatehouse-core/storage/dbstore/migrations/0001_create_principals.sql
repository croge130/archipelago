CREATE TABLE IF NOT EXISTS gatehouse_principals (
	principal_id       uuid PRIMARY KEY,
	key                text NOT NULL UNIQUE,
	display_name       text NOT NULL DEFAULT '',
	type               text NOT NULL,
	owner_principal_id uuid NULL REFERENCES gatehouse_principals (principal_id),
	metadata           jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at         timestamptz NOT NULL DEFAULT now(),
	updated_at         timestamptz NOT NULL DEFAULT now()
)
