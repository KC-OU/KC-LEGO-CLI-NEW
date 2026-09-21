// Package partdbtest builds a scratch Part-DB SQLite file with the REAL
// structure of the live database (schema.sql, captured from Part-DB v2.14.1),
// so tests exercise the same NOT NULL columns and unique indexes production
// has. Tests must never open the live /root/docker-server/partdb/db/app.db:
// an earlier stripped-down fixture hid that inserts into categories and
// part_lots fail on the real schema.
package partdbtest

import (
	"database/sql"
	_ "embed"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// LiveDBPath is the production file; Guard fails a test that resolves to it.
const LiveDBPath = "/root/docker-server/partdb/db/app.db"

// Schema returns the structure-only DDL, for tests that load it themselves.
func Schema() string { return schema }

// New creates a scratch database in t.TempDir() with the real schema plus the
// few rows Part-DB itself always has (the '(Other)' category with id 7 the
// code falls back to, storage location 1, and the admins/readonly/users groups) and
// returns its path.
func New(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("loading the Part-DB schema fixture: %v", err)
	}
	seed := []string{
		`INSERT INTO categories (id, name, parent_id, partname_hint, partname_regex, disable_footprints, disable_manufacturers,
			disable_autodatasheets, disable_properties, default_description, default_comment, comment, not_selectable)
			VALUES (7, '(Other)', NULL, '', '', 0, 0, 0, 0, '', '', '', 0)`,
		`INSERT INTO storelocations (id, name, is_full, only_single_part, limit_to_existing_parts, comment, not_selectable)
			VALUES (1, 'Default', 0, 0, 0, '', 0)`,
		`INSERT INTO "groups" (id, name, enforce_2fa, comment, not_selectable, permissions_data)
			VALUES (1, 'admins', 0, '', 0, '[]'), (2, 'readonly', 0, '', 0, '[]'), (3, 'users', 0, '', 0, '[]')`,
	}
	for _, s := range seed {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seeding the scratch Part-DB: %v\n%s", err, s)
		}
	}
	return path
}

// Guard fails the test if path is the live Part-DB file.
func Guard(t testing.TB, path string) {
	t.Helper()
	if path == LiveDBPath {
		t.Fatalf("this test resolved PARTDB_DB_PATH to the LIVE Part-DB file; use partdbtest.New(t)")
	}
}
