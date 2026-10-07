module github.com/croge130/archipelago/sdk

go 1.23

require (
	github.com/croge130/archipelago/alias v0.0.0
	github.com/croge130/archipelago/aliasauth v0.0.0
	github.com/croge130/archipelago/certstore v0.0.0
	github.com/croge130/archipelago/gatehouse-core v0.0.0
	github.com/croge130/archipelago/policy v0.0.0
	github.com/croge130/archipelago/vitals v0.0.0
	github.com/croge130/archipelago/vitalsauth v0.0.0
	github.com/croge130/archipelago/vitalsdefaults v0.0.0
)

replace (
	github.com/croge130/archipelago/alias => ../alias
	github.com/croge130/archipelago/aliasauth => ../aliasauth
	github.com/croge130/archipelago/certstore => ../certstore
	github.com/croge130/archipelago/gatehouse-core => ../gatehouse-core
	github.com/croge130/archipelago/policy => ../policy
	github.com/croge130/archipelago/vitals => ../vitals
	github.com/croge130/archipelago/vitalsauth => ../vitalsauth
	github.com/croge130/archipelago/vitalsdefaults => ../vitalsdefaults
)
