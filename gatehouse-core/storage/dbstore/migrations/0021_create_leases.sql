-- No background expiry sweep: an expired lease is simply acquirable by
-- the next caller, per docs/architecture/13-registry-and-leases-model.md.
CREATE TABLE IF NOT EXISTS gatehouse_leases (
	lease_group        text NOT NULL,
	lease_name         text NOT NULL,
	holder_instance_id uuid NOT NULL REFERENCES gatehouse_instances (instance_id),
	acquired_at        timestamptz NOT NULL,
	expires_at         timestamptz NOT NULL,
	PRIMARY KEY (lease_group, lease_name)
);
