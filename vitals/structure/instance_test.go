package structure

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func validInstance() Instance {
	now := time.Now()
	return Instance{
		InstanceID:   uuid.New(),
		ScopeType:    "gatehouse.context",
		ScopeID:      "myapp",
		InstanceKey:  "core.services.importer.status",
		DefinitionID: uuid.New(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func TestInstanceValidateOK(t *testing.T) {
	if err := validInstance().Validate(); err != nil {
		t.Fatalf("expected a valid instance to validate, got: %v", err)
	}
}

func TestInstanceValidateRejectsMissingScopeType(t *testing.T) {
	i := validInstance()
	i.ScopeType = ""
	if err := i.Validate(); err == nil {
		t.Fatal("expected an error for a missing ScopeType")
	}
}

func TestInstanceValidateRejectsMissingScopeID(t *testing.T) {
	i := validInstance()
	i.ScopeID = ""
	if err := i.Validate(); err == nil {
		t.Fatal("expected an error for a missing ScopeID")
	}
}

func TestInstanceValidateRejectsBadInstanceKey(t *testing.T) {
	i := validInstance()
	i.InstanceKey = "Not Valid!"
	if err := i.Validate(); err == nil {
		t.Fatal("expected an error for an invalid InstanceKey")
	}
}

func TestInstanceValidateRejectsMissingDefinitionID(t *testing.T) {
	i := validInstance()
	i.DefinitionID = uuid.Nil
	if err := i.Validate(); err == nil {
		t.Fatal("expected an error for a missing DefinitionID")
	}
}

func TestInstanceValidateRejectsOverdeepCategoryPath(t *testing.T) {
	i := validInstance()
	i.CategoryPath = make([]string, MaxCategoryPathDepth+1)
	for idx := range i.CategoryPath {
		i.CategoryPath[idx] = "x"
	}
	if err := i.Validate(); err == nil {
		t.Fatal("expected an error for a category path deeper than the max")
	}
}

func TestExpectedStatesOrDefault(t *testing.T) {
	instance := validInstance()
	def := Definition{}

	if got := ExpectedStatesOrDefault(instance, def); len(got) != 1 || got[0] != StateOK {
		t.Fatalf("expected [ok] as the ultimate fallback, got %v", got)
	}

	def.DefaultExpectedStates = []State{StateRunning}
	if got := ExpectedStatesOrDefault(instance, def); len(got) != 1 || got[0] != StateRunning {
		t.Fatalf("expected the definition's default to apply, got %v", got)
	}

	instance.ExpectedStates = []State{StateOffline}
	if got := ExpectedStatesOrDefault(instance, def); len(got) != 1 || got[0] != StateOffline {
		t.Fatalf("expected the instance's own expected states to win, got %v", got)
	}
}
