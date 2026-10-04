package structure

import (
	"encoding/json"
	"fmt"
)

// EndpointDefinition is a descriptive record that points at a
// permission key rather than duplicating anything about it — the
// drift fix named in docs/architecture/15-endpoint-advertisement-model.md:
// "which endpoints exist" and "which endpoints need what permission"
// live in exactly one place, this one, registered together by
// facade.RegisterEndpoint.
type EndpointDefinition struct {
	EndpointKey           string
	Description           string
	RequiredPermissionKey string          // "" means no permission is required — publicly listed and callable
	Metadata              json.RawMessage // opaque, app-defined, never authorized on
}

// Validate checks shape only — as minimal as PermissionDefinition's
// own Validate, deliberately not a stricter rule for a second kind of
// key. Whether RequiredPermissionKey names a real, registered
// PermissionDefinition is a cross-row fact facade.RegisterEndpoint
// checks, not this method.
func (e EndpointDefinition) Validate() error {
	if e.EndpointKey == "" {
		return fmt.Errorf("structure: endpoint definition: EndpointKey is required")
	}
	return nil
}
