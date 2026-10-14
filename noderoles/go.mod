module github.com/croge130/archipelago/noderoles

go 1.23

require (
	github.com/croge130/archipelago/governor v0.0.0
	github.com/croge130/archipelago/recurrence v0.0.0
	github.com/croge130/archipelago/scheduler v0.0.0
)

replace (
	github.com/croge130/archipelago/governor => ../governor
	github.com/croge130/archipelago/recurrence => ../recurrence
	github.com/croge130/archipelago/scheduler => ../scheduler
)
