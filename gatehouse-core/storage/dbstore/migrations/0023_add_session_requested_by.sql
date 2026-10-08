-- B in an assumed session (09-gatehouse-core-model.md, "Assumed
-- sessions"): who asked for the work. Only meaningful when kind =
-- 'assumed'; structure.Session.Validate enforces that, the column itself
-- stays nullable and unconstrained so other kinds are unaffected.
ALTER TABLE gatehouse_sessions
	ADD COLUMN IF NOT EXISTS requested_by_principal_id uuid NULL REFERENCES gatehouse_principals (principal_id);
