-- scope is two-way (global/context), not lighthouse/realm/context —
-- see docs/architecture/09-gatehouse-core-model.md's Grant section.
-- context_type/context_id are plain text here, not FKs into
-- gatehouse_contexts: Context create/release operations aren't built
-- yet, and requiring them first isn't needed to prove the evaluator's
-- own matching logic. Revisit once Context CRUD exists.
CREATE TABLE IF NOT EXISTS gatehouse_grants (
	grant_id       uuid PRIMARY KEY,
	subject_type   text NOT NULL,
	subject_id     uuid NOT NULL,
	target_type    text NOT NULL,
	permission_key text NULL,
	role_id        uuid NULL REFERENCES gatehouse_roles (role_id),
	scope          text NOT NULL,
	context_type   text NULL,
	context_id     text NULL,
	effect         text NOT NULL,
	status         text NOT NULL,
	origin         text NOT NULL,
	metadata       jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_by     uuid NULL REFERENCES gatehouse_principals (principal_id),
	created_at     timestamptz NOT NULL DEFAULT now(),
	updated_at     timestamptz NOT NULL DEFAULT now(),
	CONSTRAINT permission_key_xor_role_id CHECK (
		(permission_key IS NOT NULL) != (role_id IS NOT NULL)
	)
)
