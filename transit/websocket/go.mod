module github.com/croge130/archipelago/transit/websocket

go 1.23

replace (
	github.com/croge130/archipelago/transit => ../../transit
	github.com/croge130/archipelago/wire => ../../wire
)

require (
	github.com/coder/websocket v1.8.15
	github.com/croge130/archipelago/transit v0.0.0
	github.com/croge130/archipelago/wire v0.0.0
)

require github.com/google/uuid v1.6.0 // indirect
