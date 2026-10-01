-- Public key material only — not a secret at all, never hashed or
-- encrypted.
CREATE TABLE IF NOT EXISTS gatehouse_passkey_credentials (
	credential_id uuid PRIMARY KEY REFERENCES gatehouse_credentials (credential_id),
	public_key    bytea NOT NULL,
	sign_count    bigint NOT NULL DEFAULT 0,
	transports    text[] NOT NULL DEFAULT '{}',
	backed_up     boolean NOT NULL DEFAULT false
)
