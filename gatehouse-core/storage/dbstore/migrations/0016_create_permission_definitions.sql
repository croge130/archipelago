-- No CHECK enforcing "wildcard_includable implies not recovery_access"
-- at the DB level — that invariant is already enforced in
-- structure.PermissionDefinition.Validate (construction time) and
-- independently re-checked in evaluation's matching logic (match
-- time); a third enforcement point here would be a third place to
-- keep in sync rather than genuine defense in depth.
CREATE TABLE IF NOT EXISTS gatehouse_permission_definitions (
	permission_key           text PRIMARY KEY,
	required_authority_level text NOT NULL,
	wildcard_includable       boolean NOT NULL DEFAULT false
)
