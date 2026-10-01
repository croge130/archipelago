module github.com/croge130/archipelago/policy

go 1.23

require (
	github.com/croge130/archipelago/typeconstraints v0.0.0
	github.com/croge130/archipelago/typedvalue v0.0.0
	github.com/google/uuid v1.6.0
)

replace (
	github.com/croge130/archipelago/typeconstraints => ../typeconstraints
	github.com/croge130/archipelago/typedvalue => ../typedvalue
)
