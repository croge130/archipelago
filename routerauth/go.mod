module github.com/croge130/archipelago/routerauth

go 1.23.0

require (
	github.com/croge130/archipelago/db v0.0.0
	github.com/croge130/archipelago/gatehouse-core v0.0.0
	github.com/croge130/archipelago/peerauth v0.0.0
	github.com/croge130/archipelago/router v0.0.0
	github.com/croge130/archipelago/transit v0.0.0
	github.com/croge130/archipelago/wire v0.0.0
	github.com/google/uuid v1.6.0
)

require (
	github.com/croge130/archipelago/logging v0.0.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.7.6 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/text v0.24.0 // indirect
)

replace (
	github.com/croge130/archipelago/db => ../db
	github.com/croge130/archipelago/gatehouse-core => ../gatehouse-core
	github.com/croge130/archipelago/logging => ../logging
	github.com/croge130/archipelago/peerauth => ../peerauth
	github.com/croge130/archipelago/router => ../router
	github.com/croge130/archipelago/transit => ../transit
	github.com/croge130/archipelago/wire => ../wire
)
