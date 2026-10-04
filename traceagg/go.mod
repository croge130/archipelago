module github.com/croge130/archipelago/traceagg

go 1.23

require (
	github.com/croge130/archipelago/logging v0.0.0
	github.com/croge130/archipelago/transit v0.0.0
	github.com/croge130/archipelago/wire v0.0.0
	github.com/google/uuid v1.6.0
)

replace (
	github.com/croge130/archipelago/logging => ../logging
	github.com/croge130/archipelago/transit => ../transit
	github.com/croge130/archipelago/wire => ../wire
)
