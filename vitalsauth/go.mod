module github.com/croge130/archipelago/vitalsauth

go 1.23

require (
	github.com/croge130/archipelago/gatehouse-core v0.0.0
	github.com/croge130/archipelago/vitals v0.0.0
	github.com/google/uuid v1.6.0
)

replace (
	github.com/croge130/archipelago/gatehouse-core => ../gatehouse-core
	github.com/croge130/archipelago/vitals => ../vitals
)
