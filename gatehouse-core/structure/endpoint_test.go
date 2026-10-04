package structure

import "testing"

func TestEndpointDefinitionValidateOK(t *testing.T) {
	e := EndpointDefinition{
		EndpointKey:           "myapp.importer.run",
		Description:           "Trigger an import run",
		RequiredPermissionKey: "myapp.importer.write",
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("expected a valid endpoint definition to validate, got: %v", err)
	}
}

func TestEndpointDefinitionAllowsEmptyRequiredPermissionKey(t *testing.T) {
	e := EndpointDefinition{EndpointKey: "myapp.health.check"}
	if err := e.Validate(); err != nil {
		t.Fatalf("expected an endpoint with no permission requirement to validate, got: %v", err)
	}
}

func TestEndpointDefinitionRejectsEmptyKey(t *testing.T) {
	e := EndpointDefinition{RequiredPermissionKey: "myapp.importer.write"}
	if err := e.Validate(); err == nil {
		t.Fatal("expected a missing EndpointKey to be rejected")
	}
}
