module github.com/croge130/archipelago/jobsexec

go 1.23

require (
	github.com/croge130/archipelago/logging v0.0.0
	github.com/croge130/archipelago/router v0.0.0
	github.com/croge130/archipelago/wire v0.0.0
	github.com/google/uuid v1.6.0
)

replace (
	github.com/croge130/archipelago/logging => ../logging
	github.com/croge130/archipelago/router => ../router
	github.com/croge130/archipelago/transit => ../transit
	github.com/croge130/archipelago/wire => ../wire
)
