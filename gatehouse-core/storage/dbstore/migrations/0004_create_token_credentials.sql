-- Fast hash, no cost parameters, never rehashed — deliberately the
-- opposite shape from password_credentials.
CREATE TABLE IF NOT EXISTS gatehouse_token_credentials (
	credential_id uuid PRIMARY KEY REFERENCES gatehouse_credentials (credential_id),
	hash          bytea NOT NULL,
	purpose       text NOT NULL,
	scope         text NOT NULL DEFAULT '',
	expires_at    timestamptz NULL
)
