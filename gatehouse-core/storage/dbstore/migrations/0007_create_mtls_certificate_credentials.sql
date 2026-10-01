-- No secret storage at all: cert_fingerprint is a reference into
-- certstore's own records, never a copy of the certificate or key.
CREATE TABLE IF NOT EXISTS gatehouse_mtls_certificate_credentials (
	credential_id   uuid PRIMARY KEY REFERENCES gatehouse_credentials (credential_id),
	cert_fingerprint text NOT NULL
)
