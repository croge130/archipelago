package noderolesvitals

import (
	"context"
	"fmt"

	vitalsfacade "github.com/croge130/archipelago/vitals/facade"
	"github.com/croge130/archipelago/vitals/structure"
)

// The Vitals definitions this package owns, in the reserved archipelago
// namespace.
const (
	DefinitionRoleStatus = "archipelago.noderole.status"
	DefinitionGovernor   = "archipelago.noderole.governor"

	definitionVersion = 1
)

// EnsureDefinitions registers both definitions, idempotently. Neither declares
// a typed value: a node role's reading is a state, a fixed summary and a
// details document, not a single measurement.
func EnsureDefinitions(ctx context.Context, r vitalsfacade.Reader, w vitalsfacade.Writer) error {
	for _, d := range []structure.Definition{
		{
			DefinitionKey: DefinitionRoleStatus, DefinitionVersion: definitionVersion,
			Title:       "Node role status",
			Description: "How a node role adopted by a node is doing: what it runs under, what a ceiling reduced, and whether it is losing work",
		},
		{
			DefinitionKey: DefinitionGovernor, DefinitionVersion: definitionVersion,
			Title:       "Node resource governor",
			Description: "Whether the node is holding work back, and how much it is running and waiting",
		},
	} {
		if _, err := vitalsfacade.EnsureDefinition(ctx, r, w, d); err != nil {
			return fmt.Errorf("noderolesvitals: ensure %s: %w", d.DefinitionKey, err)
		}
	}
	return nil
}
