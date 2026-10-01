CREATE TABLE IF NOT EXISTS gatehouse_roles (
	role_id     uuid PRIMARY KEY,
	key         text NOT NULL UNIQUE,
	description text NOT NULL DEFAULT '',
	created_at  timestamptz NOT NULL DEFAULT now(),
	updated_at  timestamptz NOT NULL DEFAULT now()
)
