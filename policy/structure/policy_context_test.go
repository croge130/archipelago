package structure

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPolicyContextValidate(t *testing.T) {
	now := time.Now()
	c := PolicyContext{PolicyContextID: uuid.New(), Key: "internet-facing", CreatedAt: now, UpdatedAt: now}
	if err := c.Validate(); err != nil {
		t.Fatalf("expected a valid PolicyContext to validate, got: %v", err)
	}
	c.Key = ""
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for a missing Key")
	}
}

func TestPolicyContextMemberValidate(t *testing.T) {
	m := PolicyContextMember{
		PolicyContextID: uuid.New(),
		Ref:             Ref{Kind: "service", Key: "gamebridge"},
		CreatedAt:       time.Now(),
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected a valid member to validate, got: %v", err)
	}
	m.Ref = Ref{}
	if err := m.Validate(); err == nil {
		t.Fatal("expected an error for an invalid Ref")
	}
}
