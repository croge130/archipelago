-- required_permission_key is not a foreign key against
-- gatehouse_permission_definitions: RegisterEndpoint checks the
-- reference exists at write time (structure checks shape, facade
-- checks relationships, same split used throughout this codebase), not
-- the database — a permission can also legitimately be registered by
-- a different module than the one registering the endpoint pointing
-- at it, same loose coupling gatehouse-core uses elsewhere for
-- cross-concept string keys.
CREATE TABLE IF NOT EXISTS gatehouse_endpoint_definitions (
	endpoint_key             text PRIMARY KEY,
	description              text NOT NULL DEFAULT '',
	required_permission_key  text NOT NULL DEFAULT '',
	metadata                 jsonb NOT NULL DEFAULT '{}'::jsonb
)
