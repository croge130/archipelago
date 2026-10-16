module github.com/croge130/archipelago/sdk

go 1.23

require (
	github.com/croge130/archipelago/alias v0.0.0
	github.com/croge130/archipelago/aliasauth v0.0.0
	github.com/croge130/archipelago/certstore v0.0.0
	github.com/croge130/archipelago/gatehouse-core v0.0.0
	github.com/croge130/archipelago/jobs v0.0.0
	github.com/croge130/archipelago/jobsauth v0.0.0
	github.com/croge130/archipelago/governor v0.0.0
	github.com/croge130/archipelago/noderoles v0.0.0
	github.com/croge130/archipelago/noderolesauth v0.0.0
	github.com/croge130/archipelago/noderolespolicy v0.0.0
	github.com/croge130/archipelago/noderolesvitals v0.0.0
	github.com/croge130/archipelago/policy v0.0.0
	github.com/croge130/archipelago/registrynoderole v0.0.0
	github.com/croge130/archipelago/scheduler v0.0.0
	github.com/croge130/archipelago/vitals v0.0.0
	github.com/croge130/archipelago/vitalsauth v0.0.0
	github.com/croge130/archipelago/vitalsdefaults v0.0.0
	github.com/google/uuid v1.6.0
)

replace (
	github.com/croge130/archipelago/alias => ../alias
	github.com/croge130/archipelago/aliasauth => ../aliasauth
	github.com/croge130/archipelago/certstore => ../certstore
	github.com/croge130/archipelago/gatehouse-core => ../gatehouse-core
	github.com/croge130/archipelago/jobs => ../jobs
	github.com/croge130/archipelago/jobsauth => ../jobsauth
	github.com/croge130/archipelago/governor => ../governor
	github.com/croge130/archipelago/noderoles => ../noderoles
	github.com/croge130/archipelago/noderolesauth => ../noderolesauth
	github.com/croge130/archipelago/noderolespolicy => ../noderolespolicy
	github.com/croge130/archipelago/noderolesvitals => ../noderolesvitals
	github.com/croge130/archipelago/recurrence => ../recurrence
	github.com/croge130/archipelago/registrynoderole => ../registrynoderole
	github.com/croge130/archipelago/scheduler => ../scheduler
	github.com/croge130/archipelago/policy => ../policy
	github.com/croge130/archipelago/vitals => ../vitals
	github.com/croge130/archipelago/vitalsauth => ../vitalsauth
	github.com/croge130/archipelago/vitalsdefaults => ../vitalsdefaults
)
