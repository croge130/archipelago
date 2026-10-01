CREATE TABLE IF NOT EXISTS gatehouse_credentials (
	credential_id uuid PRIMARY KEY,
	principal_id  uuid NOT NULL REFERENCES gatehouse_principals (principal_id),
	kind          text NOT NULL,
	status        text NOT NULL,
	created_at    timestamptz NOT NULL DEFAULT now(),
	revoked_at    timestamptz NULL
)
