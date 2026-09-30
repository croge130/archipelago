package db

import "testing"

func TestValidateMigrationsRejectsDuplicateVersion(t *testing.T) {
	err := validateMigrations([]Migration{
		{Module: "gatehouse-core", Version: 1, Name: "init"},
		{Module: "gatehouse-core", Version: 1, Name: "init-again"},
	})
	if err == nil {
		t.Fatal("expected an error for a duplicate (module, version) pair")
	}
}

func TestValidateMigrationsAllowsSameVersionDifferentModules(t *testing.T) {
	err := validateMigrations([]Migration{
		{Module: "gatehouse-core", Version: 1, Name: "init"},
		{Module: "policy", Version: 1, Name: "init"},
	})
	if err != nil {
		t.Fatalf("different modules sharing a version number should be fine: %v", err)
	}
}

func TestValidateMigrationsRejectsMissingModule(t *testing.T) {
	err := validateMigrations([]Migration{
		{Module: "", Version: 1, Name: "no module"},
	})
	if err == nil {
		t.Fatal("expected an error for a migration with no Module")
	}
}

func TestValidateMigrationsAcceptsEmptySet(t *testing.T) {
	if err := validateMigrations(nil); err != nil {
		t.Fatalf("empty migration set should never error: %v", err)
	}
}
