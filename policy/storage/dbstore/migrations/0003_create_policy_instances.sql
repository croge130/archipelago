CREATE TABLE IF NOT EXISTS policy_instances (
	policy_instance_id   uuid PRIMARY KEY,
	policy_definition_id uuid NOT NULL REFERENCES policy_definitions (policy_definition_id),
	target_kind          text NOT NULL,
	ref_kind             text NULL,
	ref_key              text NULL,
	policy_context_id    uuid NULL REFERENCES policy_contexts (policy_context_id),
	value                jsonb NOT NULL,
	binding_mode         text NOT NULL,
	lifecycle            text NOT NULL,
	metadata             jsonb NULL,
	created_by           text NOT NULL DEFAULT '',
	updated_by           text NOT NULL DEFAULT '',
	created_at           timestamptz NOT NULL DEFAULT now(),
	updated_at           timestamptz NOT NULL DEFAULT now()
);

-- At most one active instance per exact target — redundant instances
-- targeting the identical thing are never what "multiple applicable
-- sources" means; that comes from different Refs or PolicyContexts,
-- not two rows pointed at the same one. This is a real DB-enforceable
-- invariant, unlike the non-commutative-merge-mode exclusivity rule
-- (which depends on PolicyContext membership and so is Facade's job).
CREATE UNIQUE INDEX IF NOT EXISTS policy_instances_global_idx
	ON policy_instances (policy_definition_id)
	WHERE target_kind = 'global' AND lifecycle = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS policy_instances_ref_idx
	ON policy_instances (policy_definition_id, ref_kind, ref_key)
	WHERE target_kind = 'ref' AND lifecycle = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS policy_instances_policy_context_idx
	ON policy_instances (policy_definition_id, policy_context_id)
	WHERE target_kind = 'policy_context' AND lifecycle = 'active';
