module github.com/croge130/archipelago/vitalsdefaults

go 1.23

require (
	github.com/croge130/archipelago/policy v0.0.0
	github.com/croge130/archipelago/typeconstraints v0.0.0
	github.com/croge130/archipelago/typedvalue v0.0.0
	github.com/croge130/archipelago/vitals v0.0.0
)

replace (
	github.com/croge130/archipelago/policy => ../policy
	github.com/croge130/archipelago/typeconstraints => ../typeconstraints
	github.com/croge130/archipelago/typedvalue => ../typedvalue
	github.com/croge130/archipelago/vitals => ../vitals
)
