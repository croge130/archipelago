-- Encrypted, reversible — the one credential shape that must be
-- decryptable, since the code computes the current code from the
-- secret rather than comparing hashes.
CREATE TABLE IF NOT EXISTS gatehouse_totp_credentials (
	credential_id    uuid PRIMARY KEY REFERENCES gatehouse_credentials (credential_id),
	encrypted_secret bytea NOT NULL,
	key_version      integer NOT NULL
)
