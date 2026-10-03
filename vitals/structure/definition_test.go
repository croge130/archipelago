package structure

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func validDefinition() Definition {
	now := time.Now()
	return Definition{
		DefinitionID:      uuid.New(),
		DefinitionKey:     "vitals.service.liveness",
		DefinitionVersion: 1,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func TestDefinitionValidateOK(t *testing.T) {
	if err := validDefinition().Validate(); err != nil {
		t.Fatalf("expected a valid definition to validate, got: %v", err)
	}
}

func TestDefinitionValidateRejectsMissingID(t *testing.T) {
	d := validDefinition()
	d.DefinitionID = uuid.Nil
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for a missing DefinitionID")
	}
}

func TestDefinitionValidateRejectsBadKey(t *testing.T) {
	d := validDefinition()
	d.DefinitionKey = "Not A Valid Key!"
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for an invalid DefinitionKey")
	}
}

func TestDefinitionValidateRejectsNonPositiveVersion(t *testing.T) {
	d := validDefinition()
	d.DefinitionVersion = 0
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for a non-positive DefinitionVersion")
	}
}

func TestDefinitionValidateRejectsDuplicateAllowedStates(t *testing.T) {
	d := validDefinition()
	d.AllowedStates = []State{StateOK, StateOK}
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for duplicate AllowedStates")
	}
}

func TestDefinitionValidateRejectsUnknownDefaultImportance(t *testing.T) {
	d := validDefinition()
	d.DefaultImportance = Importance("extreme")
	if err := d.Validate(); err == nil {
		t.Fatal("expected an error for an unknown DefaultImportance")
	}
}
