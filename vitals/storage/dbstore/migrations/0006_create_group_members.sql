-- Composition (a group may include child groups), no inheritance.
-- Cycle rejection is enforced in writer.go via a recursive query over
-- child_group_id edges before an insert/update is allowed through.
CREATE TABLE IF NOT EXISTS vitals_group_members (
	group_id           uuid NOT NULL REFERENCES vitals_groups (group_id),
	member_key         text NOT NULL,
	member_kind        text NOT NULL,
	vital_instance_id  uuid NULL REFERENCES vitals_instances (instance_id),
	child_group_id     uuid NULL REFERENCES vitals_groups (group_id),
	label              text NOT NULL DEFAULT '',
	sort_order         int NOT NULL DEFAULT 0,
	required           boolean NOT NULL DEFAULT false,
	display_hints      jsonb NOT NULL DEFAULT '{}'::jsonb,
	app_metadata       jsonb NOT NULL DEFAULT '{}'::jsonb,
	PRIMARY KEY (group_id, member_key)
);
