module github.com/croge130/archipelago/sdkdb

go 1.23

require (
	github.com/croge130/archipelago/alias v0.0.0
	github.com/croge130/archipelago/certstore v0.0.0
	github.com/croge130/archipelago/db v0.0.0
	github.com/croge130/archipelago/gatehouse-core v0.0.0
	github.com/croge130/archipelago/governor v0.0.0
	github.com/croge130/archipelago/jobs v0.0.0
	github.com/croge130/archipelago/noderoles v0.0.0
	github.com/croge130/archipelago/noderolespolicy v0.0.0
	github.com/croge130/archipelago/policy v0.0.0
	github.com/croge130/archipelago/recurrence v0.0.0
	github.com/croge130/archipelago/scheduler v0.0.0
	github.com/croge130/archipelago/sdk v0.0.0
	github.com/croge130/archipelago/vitals v0.0.0
)

replace (
	github.com/croge130/archipelago/alias => ../alias
	github.com/croge130/archipelago/certstore => ../certstore
	github.com/croge130/archipelago/db => ../db
	github.com/croge130/archipelago/gatehouse-core => ../gatehouse-core
	github.com/croge130/archipelago/governor => ../governor
	github.com/croge130/archipelago/jobs => ../jobs
	github.com/croge130/archipelago/noderoles => ../noderoles
	github.com/croge130/archipelago/noderolespolicy => ../noderolespolicy
	github.com/croge130/archipelago/policy => ../policy
	github.com/croge130/archipelago/recurrence => ../recurrence
	github.com/croge130/archipelago/scheduler => ../scheduler
	github.com/croge130/archipelago/sdk => ../sdk
	github.com/croge130/archipelago/vitals => ../vitals
)
