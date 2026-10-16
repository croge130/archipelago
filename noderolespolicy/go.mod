module github.com/croge130/archipelago/noderolespolicy

go 1.23

require (
	github.com/croge130/archipelago/db v0.0.0
	github.com/croge130/archipelago/governor v0.0.0
	github.com/croge130/archipelago/noderoles v0.0.0
	github.com/croge130/archipelago/policy v0.0.0
	github.com/croge130/archipelago/recurrence v0.0.0
	github.com/croge130/archipelago/scheduler v0.0.0
	github.com/croge130/archipelago/typeconstraints v0.0.0
	github.com/croge130/archipelago/typedvalue v0.0.0
)

replace (
	github.com/croge130/archipelago/db => ../db
	github.com/croge130/archipelago/governor => ../governor
	github.com/croge130/archipelago/noderoles => ../noderoles
	github.com/croge130/archipelago/policy => ../policy
	github.com/croge130/archipelago/recurrence => ../recurrence
	github.com/croge130/archipelago/scheduler => ../scheduler
	github.com/croge130/archipelago/typeconstraints => ../typeconstraints
	github.com/croge130/archipelago/typedvalue => ../typedvalue
)
