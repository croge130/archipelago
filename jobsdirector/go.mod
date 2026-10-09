module github.com/croge130/archipelago/jobsdirector

go 1.23

require (
	github.com/croge130/archipelago/db v0.0.0
	github.com/croge130/archipelago/gatehouse-core v0.0.0
	github.com/croge130/archipelago/jobs v0.0.0
	github.com/croge130/archipelago/jobsauth v0.0.0
	github.com/croge130/archipelago/jobsexec v0.0.0
	github.com/croge130/archipelago/logging v0.0.0
	github.com/croge130/archipelago/peerauth v0.0.0
	github.com/croge130/archipelago/router v0.0.0
	github.com/croge130/archipelago/routerauth v0.0.0
	github.com/croge130/archipelago/transit v0.0.0
	github.com/croge130/archipelago/typedvalue v0.0.0
	github.com/croge130/archipelago/wire v0.0.0
	github.com/google/uuid v1.6.0
)

replace (
	github.com/croge130/archipelago/db => ../db
	github.com/croge130/archipelago/gatehouse-core => ../gatehouse-core
	github.com/croge130/archipelago/jobs => ../jobs
	github.com/croge130/archipelago/jobsauth => ../jobsauth
	github.com/croge130/archipelago/jobsexec => ../jobsexec
	github.com/croge130/archipelago/logging => ../logging
	github.com/croge130/archipelago/peerauth => ../peerauth
	github.com/croge130/archipelago/router => ../router
	github.com/croge130/archipelago/routerauth => ../routerauth
	github.com/croge130/archipelago/transit => ../transit
	github.com/croge130/archipelago/typedvalue => ../typedvalue
	github.com/croge130/archipelago/wire => ../wire
)
