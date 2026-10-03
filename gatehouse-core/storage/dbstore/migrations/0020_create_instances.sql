-- Liveness is heartbeat-based, read at query time with a caller-chosen
-- cutoff — no background sweep deletes a stale row, per
-- docs/architecture/13-registry-and-leases-model.md.
CREATE TABLE IF NOT EXISTS gatehouse_instances (
	instance_id       uuid PRIMARY KEY,
	principal_id      uuid NOT NULL REFERENCES gatehouse_principals (principal_id),
	instance_group    text NOT NULL,
	metadata          jsonb NOT NULL DEFAULT '{}'::jsonb,
	registered_at     timestamptz NOT NULL DEFAULT now(),
	last_heartbeat_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS gatehouse_instances_group_heartbeat_idx
	ON gatehouse_instances (instance_group, last_heartbeat_at);
