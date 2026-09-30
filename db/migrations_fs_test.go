package db

import (
	"testing"
	"testing/fstest"
)

func TestMigrationsFromFSOrdersByVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_create_credentials.sql": &fstest.MapFile{Data: []byte("CREATE TABLE credentials ()")},
		"0001_create_principals.sql":  &fstest.MapFile{Data: []byte("CREATE TABLE principals ()")},
		"0010_create_grants.sql":      &fstest.MapFile{Data: []byte("CREATE TABLE grants ()")},
	}

	migrations, err := MigrationsFromFS("gatehouse-core", fsys)
	if err != nil {
		t.Fatalf("MigrationsFromFS: %v", err)
	}
	if len(migrations) != 3 {
		t.Fatalf("expected 3 migrations, got %d", len(migrations))
	}

	wantOrder := []int{1, 2, 10}
	for i, m := range migrations {
		if m.Version != wantOrder[i] {
			t.Errorf("migrations[%d].Version = %d, want %d (lexical sort would put 0010 before 0002)", i, m.Version, wantOrder[i])
		}
		if m.Module != "gatehouse-core" {
			t.Errorf("migrations[%d].Module = %q, want gatehouse-core", i, m.Module)
		}
	}
	if migrations[0].Name != "create_principals" {
		t.Errorf("migrations[0].Name = %q, want create_principals", migrations[0].Name)
	}
	if migrations[0].SQL != "CREATE TABLE principals ()" {
		t.Errorf("migrations[0].SQL = %q, want the embedded file contents", migrations[0].SQL)
	}
}

func TestMigrationsFromFSRejectsBadFilename(t *testing.T) {
	fsys := fstest.MapFS{
		"not-the-right-shape.sql": &fstest.MapFile{Data: []byte("SELECT 1")},
	}
	if _, err := MigrationsFromFS("gatehouse-core", fsys); err == nil {
		t.Fatal("expected an error for a filename with no NNNN_ prefix")
	}
}

func TestMigrationsFromFSRejectsNonNumericPrefix(t *testing.T) {
	fsys := fstest.MapFS{
		"abcd_create_principals.sql": &fstest.MapFile{Data: []byte("SELECT 1")},
	}
	if _, err := MigrationsFromFS("gatehouse-core", fsys); err == nil {
		t.Fatal("expected an error for a non-numeric version prefix")
	}
}

func TestMigrationsFromFSRejectsDuplicateVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_create_principals.sql": &fstest.MapFile{Data: []byte("SELECT 1")},
		"0001_also_claims_one.sql":   &fstest.MapFile{Data: []byte("SELECT 1")},
	}
	if _, err := MigrationsFromFS("gatehouse-core", fsys); err == nil {
		t.Fatal("expected an error for two files claiming the same version")
	}
}

func TestMigrationsFromFSIgnoresNonSQLFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_create_principals.sql": &fstest.MapFile{Data: []byte("SELECT 1")},
		"README.md":                  &fstest.MapFile{Data: []byte("not a migration")},
	}
	migrations, err := MigrationsFromFS("gatehouse-core", fsys)
	if err != nil {
		t.Fatalf("MigrationsFromFS: %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("expected non-.sql files to be ignored, got %d migrations", len(migrations))
	}
}

func TestMigrationsFromFSEmptyIsFine(t *testing.T) {
	migrations, err := MigrationsFromFS("gatehouse-core", fstest.MapFS{})
	if err != nil {
		t.Fatalf("empty migrations dir should not error: %v", err)
	}
	if len(migrations) != 0 {
		t.Errorf("expected 0 migrations, got %d", len(migrations))
	}
}

// Feeding MigrationsFromFS's output straight into ProvisionSchemas is
// the real intended usage; confirm they compose without a DB, via the
// validation path alone.
func TestMigrationsFromFSOutputPassesValidation(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_create_principals.sql": &fstest.MapFile{Data: []byte("SELECT 1")},
		"0002_create_grants.sql":     &fstest.MapFile{Data: []byte("SELECT 1")},
	}
	migrations, err := MigrationsFromFS("gatehouse-core", fsys)
	if err != nil {
		t.Fatalf("MigrationsFromFS: %v", err)
	}
	if err := validateMigrations(migrations); err != nil {
		t.Fatalf("validateMigrations rejected well-formed output: %v", err)
	}
}
