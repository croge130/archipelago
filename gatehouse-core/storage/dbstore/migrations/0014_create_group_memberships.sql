CREATE TABLE IF NOT EXISTS gatehouse_group_memberships (
	group_id     uuid NOT NULL REFERENCES gatehouse_groups (group_id),
	principal_id uuid NOT NULL REFERENCES gatehouse_principals (principal_id),
	created_at   timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (group_id, principal_id)
)
