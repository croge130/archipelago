package db

import (
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// MigrationsFromFS builds a []Migration for module from every *.sql file
// directly inside fsys, using the filename convention
// NNNN_description.sql — a sequential integer, an underscore, then the
// description used as the migration's Name. fsys should already be
// rooted at the migrations directory itself (fs.Sub an embed.FS if
// needed), not at a parent containing it.
//
// This is a naming convention for ordering and readability, not a
// versioning scheme with any backward-compatibility guarantee — there's
// nothing deployed yet for a migration history to stay compatible with.
// Editing a migration file in place instead of adding a new one stops
// being safe once it's actually been applied somewhere that matters;
// that discipline starts when a real deployment exists, not before.
func MigrationsFromFS(module string, fsys fs.FS) ([]Migration, error) {
	entries, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("db: glob migrations for %s: %w", module, err)
	}
	sort.Strings(entries)

	migrations := make([]Migration, 0, len(entries))
	seenVersions := make(map[int]string)
	for _, name := range entries {
		version, desc, err := parseMigrationFilename(name)
		if err != nil {
			return nil, fmt.Errorf("db: %s: %w", name, err)
		}
		if other, ok := seenVersions[version]; ok {
			return nil, fmt.Errorf("db: %s and %s both claim version %d in module %s",
				other, name, version, module)
		}
		seenVersions[version] = name

		sqlBytes, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("db: read %s: %w", name, err)
		}
		migrations = append(migrations, Migration{
			Module:  module,
			Version: version,
			Name:    desc,
			SQL:     string(sqlBytes),
		})
	}
	return migrations, nil
}

func parseMigrationFilename(name string) (version int, desc string, err error) {
	base := strings.TrimSuffix(name, ".sql")
	parts := strings.SplitN(base, "_", 2)
	if len(parts) != 2 || parts[1] == "" {
		return 0, "", fmt.Errorf("filename must look like NNNN_description.sql, got %q", name)
	}
	version, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", fmt.Errorf("filename's numeric prefix is not a number: %q", name)
	}
	return version, parts[1], nil
}
