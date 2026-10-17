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

// TestSchemaV13AddsKanbanTablesAndColumns confirms version 13's migration
// block actually lands: the three set_state backlog columns, set_check_lines'
// deprioritized column, and the two new kanban tables.
func TestSchemaV13AddsKanbanTablesAndColumns(t *testing.T) {
	d := newFixture(t)

	checkCol := func(table, col string) {
		t.Helper()
		var has int
		if err := d.QueryRow("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", table, col).Scan(&has); err != nil {
			t.Fatalf("checking %s.%s: %v", table, col, err)
		}
		if has == 0 {
			t.Errorf("%s.%s is missing", table, col)
		}
	}
	checkCol("set_state", "backlog_status")
	checkCol("set_state", "backlog_released_by")
	checkCol("set_state", "backlog_released_at")
	checkCol("set_check_lines", "deprioritized")

	checkTable := func(table string) {
		t.Helper()
		var has int
		if err := d.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?", table).Scan(&has); err != nil {
			t.Fatalf("checking table %s: %v", table, err)
		}
		if has == 0 {
			t.Errorf("table %s is missing", table)
		}
	}
	checkTable("kanban_notes")
	checkTable("kanban_card_moves")
}
