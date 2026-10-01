-- Memory-hard hash parameters and lockout/rehash tracking live here,
-- not on a shared "secrets" table — see docs/architecture/
-- 09-gatehouse-core-model.md's Credential storage section for why
-- password and token credentials deliberately don't share a shape.
CREATE TABLE IF NOT EXISTS gatehouse_password_credentials (
	credential_id    uuid PRIMARY KEY REFERENCES gatehouse_credentials (credential_id),
	hash_algorithm   text NOT NULL,
	hash             bytea NOT NULL,
	memory_kib       integer NOT NULL,
	iterations       integer NOT NULL,
	parallelism      integer NOT NULL,
	failed_attempts  integer NOT NULL DEFAULT 0,
	locked_until     timestamptz NULL,
	last_rehashed_at timestamptz NULL
)
