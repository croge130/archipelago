CREATE TABLE IF NOT EXISTS policy_contexts (
	policy_context_id uuid PRIMARY KEY,
	key               text NOT NULL UNIQUE,
	description       text NOT NULL DEFAULT '',
	created_at        timestamptz NOT NULL DEFAULT now(),
	updated_at        timestamptz NOT NULL DEFAULT now()
);

-- Membership is Refs only — there is deliberately no column here that
-- could reference another policy_contexts row. Nesting PolicyContexts
-- would recreate a containment tree under a new name.
CREATE TABLE IF NOT EXISTS policy_context_members (
	policy_context_id uuid NOT NULL REFERENCES policy_contexts (policy_context_id),
	ref_kind          text NOT NULL,
	ref_key           text NOT NULL,
	created_at        timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (policy_context_id, ref_kind, ref_key)
);

CREATE INDEX IF NOT EXISTS policy_context_members_ref_idx ON policy_context_members (ref_kind, ref_key);
