-- id is a storage-only surrogate key; structure.RolePermission has no
-- ID field of its own since its logical identity is (role_id,
-- permission_key xor child_role_id) — Postgres still wants a primary
-- key to reference rows by, so this exists purely at the storage layer.
CREATE TABLE IF NOT EXISTS gatehouse_role_permissions (
	id             uuid PRIMARY KEY,
	role_id        uuid NOT NULL REFERENCES gatehouse_roles (role_id),
	permission_key text NULL,
	child_role_id  uuid NULL REFERENCES gatehouse_roles (role_id),
	effect         text NOT NULL,
	CONSTRAINT permission_key_xor_child_role_id CHECK (
		(permission_key IS NOT NULL) != (child_role_id IS NOT NULL)
	)
)
