-- Peer authorization's primary access pattern: resolve a verified
-- mTLS peer's certificate fingerprint to the credential (and from
-- there, the principal) it belongs to, on every connection.
CREATE INDEX IF NOT EXISTS gatehouse_mtls_certificate_credentials_fingerprint_idx
	ON gatehouse_mtls_certificate_credentials (cert_fingerprint);
