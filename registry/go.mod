module github.com/croge130/archipelago/registry

go 1.23

require (
	github.com/croge130/archipelago/gatehouse-core v0.0.0
	github.com/croge130/archipelago/peerauth v0.0.0
	github.com/croge130/archipelago/router v0.0.0
	github.com/croge130/archipelago/transit v0.0.0
	github.com/croge130/archipelago/wire v0.0.0
	github.com/google/uuid v1.6.0
)

replace (
	github.com/croge130/archipelago/gatehouse-core => ../gatehouse-core
	github.com/croge130/archipelago/peerauth => ../peerauth
	github.com/croge130/archipelago/router => ../router
	github.com/croge130/archipelago/transit => ../transit
	github.com/croge130/archipelago/wire => ../wire
)
