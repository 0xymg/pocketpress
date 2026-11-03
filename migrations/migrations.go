// Package migrations embeds PocketPress's SQL schema migrations so they
// ship inside the compiled binary instead of depending on files present on
// disk at runtime.
//
// Each migration is a pair of files named "NNNN_description.up.sql" and
// "NNNN_description.down.sql", where NNNN is a zero-padded, monotonically
// increasing version (e.g. "0001_init"). internal/store's migration runner
// (internal/store/migrate.go) is the intended consumer of this package.
package migrations

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed *.sql
var files embed.FS

// Migration is one embedded schema migration: a version identifier plus
// its up and down SQL scripts. Both scripts may contain multiple
// semicolon-separated statements; the migration runner executes each
// script as a whole inside a single transaction.
type Migration struct {
	Version string
	Up      string
	Down    string
}

// All returns every embedded migration, sorted by ascending version. Each
// "NNNN_description.up.sql" file must have a matching
// "NNNN_description.down.sql" file; a missing counterpart is an error.
func All() ([]Migration, error) {
	upFiles, err := fs.Glob(files, "*.up.sql")
	if err != nil {
		return nil, fmt.Errorf("glob migration files: %w", err)
	}
	sort.Strings(upFiles)

	all := make([]Migration, 0, len(upFiles))
	for _, upName := range upFiles {
		version := strings.TrimSuffix(upName, ".up.sql")

		upSQL, err := files.ReadFile(upName)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", upName, err)
		}

		downName := version + ".down.sql"
		downSQL, err := files.ReadFile(downName)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", downName, err)
		}

		all = append(all, Migration{
			Version: version,
			Up:      string(upSQL),
			Down:    string(downSQL),
		})
	}

	return all, nil
}
