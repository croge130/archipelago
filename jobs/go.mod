module github.com/croge130/archipelago/jobs

go 1.23

require (
	github.com/croge130/archipelago/db v0.0.0
	github.com/croge130/archipelago/logging v0.0.0
	github.com/croge130/archipelago/typedvalue v0.0.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.6
)

replace (
	github.com/croge130/archipelago/db => ../db
	github.com/croge130/archipelago/logging => ../logging
	github.com/croge130/archipelago/typedvalue => ../typedvalue
)
