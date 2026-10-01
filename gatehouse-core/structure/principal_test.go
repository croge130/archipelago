package structure

import (
	"testing"

	"github.com/google/uuid"
)

func validPrincipal() Principal {
	return Principal{
		PrincipalID: uuid.New(),
		Key:         "user.christian",
		Type:        PrincipalTypeUser,
	}
}

func TestPrincipalValidateOK(t *testing.T) {
	if err := validPrincipal().Validate(); err != nil {
		t.Fatalf("expected a valid principal to validate, got: %v", err)
	}
}

func TestPrincipalValidateRejectsMissingID(t *testing.T) {
	p := validPrincipal()
	p.PrincipalID = uuid.Nil
	if err := p.Validate(); err == nil {
		t.Fatal("expected an error for a missing PrincipalID")
	}
}

func TestPrincipalValidateRejectsMissingKey(t *testing.T) {
	p := validPrincipal()
	p.Key = ""
	if err := p.Validate(); err == nil {
		t.Fatal("expected an error for a missing Key")
	}
}

func TestPrincipalValidateRejectsInvalidType(t *testing.T) {
	p := validPrincipal()
	p.Type = PrincipalType("app_client") // dropped, per the model doc
	if err := p.Validate(); err == nil {
		t.Fatal("expected app_client to be rejected as an invalid PrincipalType")
	}
}

func TestPrincipalValidateRejectsSelfOwnership(t *testing.T) {
	p := validPrincipal()
	id := p.PrincipalID
	p.OwnerPrincipalID = &id
	if err := p.Validate(); err == nil {
		t.Fatal("expected a principal owning itself to be rejected")
	}
}

func TestPrincipalTypeValid(t *testing.T) {
	for _, pt := range []PrincipalType{
		PrincipalTypeUser, PrincipalTypeAgent, PrincipalTypeServiceAccount,
		PrincipalTypeSystem, PrincipalTypeExternal, PrincipalTypeAnonymous,
	} {
		if !pt.Valid() {
			t.Errorf("expected %q to be valid", pt)
		}
	}
	if PrincipalType("app_client").Valid() {
		t.Error("app_client should not be valid")
	}
}
