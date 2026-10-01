module github.com/croge130/archipelago/mtls

go 1.23

require (
	github.com/croge130/archipelago/certstore v0.0.0
	github.com/croge130/archipelago/transit v0.0.0
	github.com/croge130/archipelago/transit/websocket v0.0.0
	github.com/croge130/archipelago/wire v0.0.0
)

replace (
	github.com/croge130/archipelago/certstore => ../certstore
	github.com/croge130/archipelago/transit => ../transit
	github.com/croge130/archipelago/transit/websocket => ../transit/websocket
	github.com/croge130/archipelago/wire => ../wire
)
