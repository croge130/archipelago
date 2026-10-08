module github.com/croge130/archipelago/routere2e

go 1.23

require (
	github.com/croge130/archipelago/router v0.0.0
	github.com/croge130/archipelago/transit v0.0.0
	github.com/croge130/archipelago/transit/websocket v0.0.0
	github.com/croge130/archipelago/wire v0.0.0
)

replace (
	github.com/croge130/archipelago/logging => ../logging
	github.com/croge130/archipelago/router => ../router
	github.com/croge130/archipelago/transit => ../transit
	github.com/croge130/archipelago/transit/websocket => ../transit/websocket
	github.com/croge130/archipelago/wire => ../wire
)
