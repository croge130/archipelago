-- A single row, enforced by the boolean primary key trick (id must be
-- true, and true can only exist once). Two counters, not Lighthouse's
-- three — policy_generation belongs to the Policy base, tracked there.
-- No seed INSERT here deliberately: the row is created lazily on the
-- first bump (an upsert), and a read before any bump has happened
-- treats a missing row as generation zero — avoids a second statement
-- in this migration, which pgx's default exec mode doesn't reliably
-- support in a single call.
CREATE TABLE IF NOT EXISTS gatehouse_authority_generation (
	id                           boolean PRIMARY KEY DEFAULT true,
	permission_schema_generation bigint NOT NULL DEFAULT 0,
	principal_grant_generation   bigint NOT NULL DEFAULT 0,
	updated_at                   timestamptz NOT NULL DEFAULT now(),
	CONSTRAINT singleton_row CHECK (id)
)
