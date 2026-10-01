-- The list-then-confirm ceremony's own record. csr is stored raw
-- (PEM/DER) since nothing here needs to parse it beyond what Subject
-- already denormalizes. confirmed_by is a plain operator identifier,
-- never a gatehouse-core PrincipalID — certstore doesn't depend on
-- that base.
CREATE TABLE IF NOT EXISTS certstore_enrollments (
	enrollment_id uuid PRIMARY KEY,
	purpose       text NOT NULL,
	subject       text NOT NULL,
	csr           bytea NOT NULL,
	device_ref    text NOT NULL DEFAULT '',
	status        text NOT NULL,
	created_at    timestamptz NOT NULL DEFAULT now(),
	expires_at    timestamptz NULL,
	confirmed_at  timestamptz NULL,
	confirmed_by  text NOT NULL DEFAULT '',
	rejected_at   timestamptz NULL
);

CREATE INDEX IF NOT EXISTS certstore_enrollments_status_idx ON certstore_enrollments (status);
