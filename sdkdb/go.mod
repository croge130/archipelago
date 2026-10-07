module github.com/croge130/archipelago/sdkdb

go 1.23

require (
	github.com/croge130/archipelago/alias v0.0.0
	github.com/croge130/archipelago/certstore v0.0.0
	github.com/croge130/archipelago/db v0.0.0
	github.com/croge130/archipelago/gatehouse-core v0.0.0
	github.com/croge130/archipelago/policy v0.0.0
	github.com/croge130/archipelago/sdk v0.0.0
	github.com/croge130/archipelago/vitals v0.0.0
)

replace (
	github.com/croge130/archipelago/alias => ../alias
	github.com/croge130/archipelago/certstore => ../certstore
	github.com/croge130/archipelago/db => ../db
	github.com/croge130/archipelago/gatehouse-core => ../gatehouse-core
	github.com/croge130/archipelago/policy => ../policy
	github.com/croge130/archipelago/sdk => ../sdk
	github.com/croge130/archipelago/vitals => ../vitals
)
