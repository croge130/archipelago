-- "table" is reserved in SQL, hence table_name. (table_name, name) is
-- the natural primary key: there's nothing to identify beyond the pair
-- itself, no surrogate ID. target is kept even after release, for
-- debugging an alias's history, not cleared on release.
CREATE TABLE IF NOT EXISTS alias_aliases (
	table_name  text NOT NULL,
	name        text NOT NULL,
	target      text NOT NULL,
	lifecycle   text NOT NULL,
	created_at  timestamptz NOT NULL DEFAULT now(),
	updated_at  timestamptz NOT NULL DEFAULT now(),
	released_at timestamptz NULL,
	PRIMARY KEY (table_name, name)
)
