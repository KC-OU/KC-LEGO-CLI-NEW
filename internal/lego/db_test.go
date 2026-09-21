package lego

import (
	"path/filepath"
	"testing"
)

func newFixture(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lego.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lego.db")
	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	db1.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open against existing file: %v", err)
	}
	defer db2.Close()

	if err := db2.UpsertSet(Set{SetNum: "1", Name: "x"}); err != nil {
		t.Fatalf("schema not usable after reopen: %v", err)
	}
}
