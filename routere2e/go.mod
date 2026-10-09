module github.com/croge130/archipelago/routere2e

go 1.23

require (
	github.com/croge130/archipelago/certstore v0.0.0
	github.com/croge130/archipelago/db v0.0.0
	github.com/croge130/archipelago/gatehouse-core v0.0.0
	github.com/croge130/archipelago/logging v0.0.0
	github.com/croge130/archipelago/mtls v0.0.0
	github.com/croge130/archipelago/registry v0.0.0
	github.com/croge130/archipelago/router v0.0.0
	github.com/croge130/archipelago/routerauth v0.0.0
	github.com/croge130/archipelago/sso v0.0.0
	github.com/croge130/archipelago/traceagg v0.0.0
	github.com/croge130/archipelago/transit v0.0.0
	github.com/croge130/archipelago/transit/websocket v0.0.0
	github.com/croge130/archipelago/wire v0.0.0
	github.com/google/uuid v1.6.0
)

replace (
	github.com/croge130/archipelago/certstore => ../certstore
	github.com/croge130/archipelago/db => ../db
	github.com/croge130/archipelago/gatehouse-core => ../gatehouse-core
	github.com/croge130/archipelago/logging => ../logging
	github.com/croge130/archipelago/mtls => ../mtls
	github.com/croge130/archipelago/peerauth => ../peerauth
	github.com/croge130/archipelago/registry => ../registry
	github.com/croge130/archipelago/router => ../router
	github.com/croge130/archipelago/routerauth => ../routerauth
	github.com/croge130/archipelago/sso => ../sso
	github.com/croge130/archipelago/traceagg => ../traceagg
	github.com/croge130/archipelago/transit => ../transit
	github.com/croge130/archipelago/transit/websocket => ../transit/websocket
	github.com/croge130/archipelago/wire => ../wire
)
