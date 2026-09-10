package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const sqlMigrationTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version    INT          NOT NULL,
	filename   VARCHAR(255) NOT NULL,
	applied_at DATETIME(3)  NOT NULL,
	PRIMARY KEY (version)
) ENGINE=InnoDB`

func migrate(db *sql.DB, fsys fs.FS) error {
	if _, err := db.Exec(sqlMigrationTable); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	var applied int
	if err := db.QueryRow(
		"SELECT COALESCE(MAX(version), 0) FROM schema_migrations",
	).Scan(&applied); err != nil {
		return fmt.Errorf("read applied version: %w", err)
	}
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, err := versionOf(name)
		if err != nil {
			return err
		}
		if version <= applied {
			continue
		}

		body, err := fs.ReadFile(fsys, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}

		if _, err := db.Exec(string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := db.Exec(
			"INSERT INTO schema_migrations (version, filename, applied_at) VALUES (?, ?, NOW(3))",
			version, name,
		); err != nil {
			return fmt.Errorf("record %s: %w", name, err)
		}

		log.Printf("[store] migration applied: %s", name)
	}
	return nil
}

func versionOf(filename string) (int, error) {
	prefix := filename
	if i := strings.IndexByte(prefix, '_'); i >= 0 {
		prefix = prefix[:i]
	}
	v, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("migration %q: name must start with a number", filename)
	}
	return v, nil
}
