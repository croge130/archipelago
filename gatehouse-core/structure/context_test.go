package structure

import "testing"

func TestContextTypeValidate(t *testing.T) {
	ct := ContextType{TypeKey: "myapp.readinglist"}
	if err := ct.Validate(); err != nil {
		t.Fatalf("expected valid context type to validate, got: %v", err)
	}
	ct.TypeKey = ""
	if err := ct.Validate(); err == nil {
		t.Fatal("expected an error for a missing TypeKey")
	}
}

func TestContextValidate(t *testing.T) {
	c := Context{Type: "myapp.readinglist", ID: "42", Lifecycle: ContextLifecycleActive}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected valid context to validate, got: %v", err)
	}
}

func TestContextValidateRejectsInvalidLifecycle(t *testing.T) {
	c := Context{Type: "myapp.readinglist", ID: "42", Lifecycle: ContextLifecycle("archived")}
	if err := c.Validate(); err == nil {
		t.Fatal("expected an invalid Lifecycle to be rejected")
	}
}

func TestContextValidateRequiresTypeAndID(t *testing.T) {
	c := Context{Lifecycle: ContextLifecycleActive}
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for a missing Type and ID")
	}
}
