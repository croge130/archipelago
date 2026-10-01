-- An issued certificate's record. serial_number is X.509's own
-- arbitrary-precision integer, stored as hex text per structure.Cert's
-- own comment — never a numeric column. fingerprint is the
-- correlation handle used elsewhere (e.g. gatehouse-core's
-- MTLSCertCredDetail).
CREATE TABLE IF NOT EXISTS certstore_certs (
	serial_number     text PRIMARY KEY,
	enrollment_id     uuid NOT NULL REFERENCES certstore_enrollments (enrollment_id),
	purpose           text NOT NULL,
	subject           text NOT NULL,
	fingerprint       text NOT NULL,
	not_before        timestamptz NOT NULL,
	not_after         timestamptz NOT NULL,
	status            text NOT NULL,
	revoked_at        timestamptz NULL,
	revocation_reason text NOT NULL DEFAULT '',
	issued_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS certstore_certs_fingerprint_idx ON certstore_certs (fingerprint);
CREATE INDEX IF NOT EXISTS certstore_certs_status_idx ON certstore_certs (status);
