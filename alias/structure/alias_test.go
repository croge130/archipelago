package structure

import (
	"testing"
	"time"
)

func validAlias() Alias {
	now := time.Now()
	return Alias{Table: "documents", Name: "readme", Target: "doc-42", Lifecycle: LifecycleActive, CreatedAt: now, UpdatedAt: now}
}

func TestAliasValidateOK(t *testing.T) {
	if err := validAlias().Validate(); err != nil {
		t.Fatalf("expected a valid alias to validate, got: %v", err)
	}
}

func TestAliasValidateRejectsMissingTable(t *testing.T) {
	a := validAlias()
	a.Table = ""
	if err := a.Validate(); err == nil {
		t.Fatal("expected an error for a missing Table")
	}
}

func TestAliasValidateRejectsMissingName(t *testing.T) {
	a := validAlias()
	a.Name = ""
	if err := a.Validate(); err == nil {
		t.Fatal("expected an error for a missing Name")
	}
}

func TestAliasValidateRejectsMissingTarget(t *testing.T) {
	a := validAlias()
	a.Target = ""
	if err := a.Validate(); err == nil {
		t.Fatal("expected an error for a missing Target")
	}
}

func TestAliasValidateRejectsInvalidLifecycle(t *testing.T) {
	a := validAlias()
	a.Lifecycle = Lifecycle("archived")
	if err := a.Validate(); err == nil {
		t.Fatal("expected an error for an invalid Lifecycle")
	}
}

func TestAliasValidateRejectsActiveWithReleasedAt(t *testing.T) {
	a := validAlias()
	now := time.Now()
	a.ReleasedAt = &now
	if err := a.Validate(); err == nil {
		t.Fatal("expected an error for an active alias carrying ReleasedAt")
	}
}

func TestAliasValidateRejectsReleasedWithoutReleasedAt(t *testing.T) {
	a := validAlias()
	a.Lifecycle = LifecycleReleased
	if err := a.Validate(); err == nil {
		t.Fatal("expected an error for a released alias missing ReleasedAt")
	}
}

func TestAliasValidateAllowsReleasedWithReleasedAt(t *testing.T) {
	a := validAlias()
	a.Lifecycle = LifecycleReleased
	now := time.Now()
	a.ReleasedAt = &now
	if err := a.Validate(); err != nil {
		t.Fatalf("expected a properly released alias to validate, got: %v", err)
	}
}
