package dbstore

import (
	"embed"
	"io/fs"

	archidb "github.com/croge130/archipelago/db"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations returns certstore's schema migrations, ready to pass to a
// db.Pool.ProvisionSchemas call. Registered under the module name
// "certstore" — scoped independently of every other base's own
// migrations.
func Migrations() ([]archidb.Migration, error) {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	return archidb.MigrationsFromFS("certstore", sub)
}
