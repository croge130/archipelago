-- Deliberately no connection-reference column: connection-binding is
-- the Sessions Layer 2 integration's job (gatehouse-core + transit),
-- never a field the base record carries.
CREATE TABLE IF NOT EXISTS gatehouse_sessions (
	session_id               uuid PRIMARY KEY,
	principal_id             uuid NOT NULL REFERENCES gatehouse_principals (principal_id),
	credential_id            uuid NULL REFERENCES gatehouse_credentials (credential_id),
	kind                     text NOT NULL,
	authority_level          text NOT NULL,
	authentication_method    text NOT NULL DEFAULT '',
	asserted_by_principal_id uuid NULL REFERENCES gatehouse_principals (principal_id),
	metadata                 jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at               timestamptz NOT NULL DEFAULT now(),
	expires_at               timestamptz NULL,
	last_seen                timestamptz NOT NULL DEFAULT now(),
	revoked_at               timestamptz NULL
)
