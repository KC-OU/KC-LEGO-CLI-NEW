package lego

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

type DB struct {
	*sql.DB
	file  string
	actor string // who is making changes, recorded in the journal
}

// Open opens (creating if needed) the LEGO collection SQLite file and
// ensures its schema exists. There is no migration framework anywhere in
// this repo — like internal/partdb, schema management here is just
// idempotent CREATE TABLE IF NOT EXISTS on every open.
func Open(path string) (*DB, error) {
	if path == "" {
		path = config.Get(config.LegoDBPath)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("creating lego db directory: %w", err)
	}
	// Every telnet/web session is its own process opening this file. WAL lets them keep reading (search,
	// detail pages) while another process holds a long write, such as the catalog refresh; writers
	// still take turns, so a write waits up to 30 s for a lock instead of failing at once.
	sqlDB, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("opening lego sqlite file %s: %w", path, err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("connecting to lego sqlite file %s: %w", path, err)
	}
	if err := ensureSchema(sqlDB); err != nil {
		return nil, fmt.Errorf("ensuring lego schema: %w", err)
	}
	return &DB{DB: sqlDB, file: path}, nil
}

func ensureSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			set_num TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			theme TEXT NOT NULL DEFAULT '',
			year INTEGER NOT NULL DEFAULT 0,
			instruction_book_number TEXT NOT NULL DEFAULT '',
			instruction_book_count INTEGER NOT NULL DEFAULT 0,
			qty INTEGER NOT NULL DEFAULT 0,
			parts_qty INTEGER NOT NULL DEFAULT 0,
			parted_out INTEGER NOT NULL DEFAULT 0,
			parted_out_info TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ref_sets (
			set_num TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			year TEXT NOT NULL DEFAULT '',
			theme TEXT NOT NULL DEFAULT '',
			total_pieces TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ref_sets_name ON ref_sets(name)`,
		`CREATE TABLE IF NOT EXISTS ref_parts (
			part_num TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ref_parts_name ON ref_parts(name)`,
		// owned_parts is individual LEGO parts (spares, bulk lots, parted-out
		// stock) tracked separately from sets. One row per part AND colour: a
		// red and a blue 3001 are different stock. color_id is Rebrickable's
		// colour id, -1 when the colour is unknown or free text (then
		// color_name says what it is). synced_part_id links to the Part-DB
		// part (0 = not there yet); min_qty drives the low-stock badge.
		`CREATE TABLE IF NOT EXISTS owned_parts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			part_num TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT '',
			color_id INTEGER NOT NULL DEFAULT -1,
			color_name TEXT NOT NULL DEFAULT '',
			qty INTEGER NOT NULL DEFAULT 0,
			min_qty INTEGER NOT NULL DEFAULT 0,
			synced_part_id INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		// Shared Rebrickable rate-limit state and response cache (see rebrickable.go).
		`CREATE TABLE IF NOT EXISTS rb_meta (key TEXT PRIMARY KEY, value INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS rb_cache (
			key TEXT PRIMARY KEY,
			status INTEGER NOT NULL,
			body BLOB,
			fetched_at INTEGER NOT NULL
		)`,
		// Offline copy of Rebrickable's downloadable catalog (see catalog.go).
		`CREATE TABLE IF NOT EXISTS cat_parts (part_num TEXT PRIMARY KEY, name TEXT NOT NULL, part_cat_id INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS cat_categories (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS cat_colors (id INTEGER PRIMARY KEY, name TEXT NOT NULL, rgb TEXT NOT NULL DEFAULT '', is_trans INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS cat_elements (part_num TEXT NOT NULL, color_id INTEGER NOT NULL, PRIMARY KEY (part_num, color_id))`,
		`CREATE TABLE IF NOT EXISTS cat_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		// The rest of Rebrickable's bulk data, so sets, minifigures, set contents and
		// part equivalences work with no API and no internet (see catalogload.go).
		`CREATE TABLE IF NOT EXISTS cat_sets (set_num TEXT PRIMARY KEY, name TEXT NOT NULL, year INTEGER NOT NULL DEFAULT 0, theme_id INTEGER NOT NULL DEFAULT 0, num_parts INTEGER NOT NULL DEFAULT 0, img_url TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS cat_themes (id INTEGER PRIMARY KEY, name TEXT NOT NULL, parent_id INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS cat_minifigs (fig_num TEXT PRIMARY KEY, name TEXT NOT NULL, num_parts INTEGER NOT NULL DEFAULT 0, img_url TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS cat_inventories (id INTEGER PRIMARY KEY, version INTEGER NOT NULL DEFAULT 1, set_num TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_cat_inventories_set ON cat_inventories(set_num, version)`,
		`CREATE TABLE IF NOT EXISTS cat_inventory_parts (inventory_id INTEGER NOT NULL, part_num TEXT NOT NULL, color_id INTEGER NOT NULL, quantity INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_cat_invparts_inv ON cat_inventory_parts(inventory_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cat_invparts_part ON cat_inventory_parts(part_num, color_id)`,
		`CREATE TABLE IF NOT EXISTS cat_inventory_sets (inventory_id INTEGER NOT NULL, set_num TEXT NOT NULL, quantity INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE IF NOT EXISTS cat_inventory_minifigs (inventory_id INTEGER NOT NULL, fig_num TEXT NOT NULL, quantity INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE IF NOT EXISTS cat_part_relations (rel_type TEXT NOT NULL, child_part TEXT NOT NULL, parent_part TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_cat_rel_child ON cat_part_relations(child_part)`,
		`CREATE INDEX IF NOT EXISTS idx_cat_rel_parent ON cat_part_relations(parent_part)`,
		`CREATE TABLE IF NOT EXISTS cat_element_ids (element_id TEXT PRIMARY KEY, part_num TEXT NOT NULL, color_id INTEGER NOT NULL)`,
		// Full-text indexes over the catalog (prefix and multi-word search). External-content
		// tables: they index cat_* without copying it, and are rebuilt after each load.
		`CREATE VIRTUAL TABLE IF NOT EXISTS fts_parts USING fts5(name, part_num, content='cat_parts', content_rowid='rowid', tokenize='unicode61 remove_diacritics 2', prefix='2 3')`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS fts_sets USING fts5(name, set_num, content='cat_sets', content_rowid='rowid', tokenize='unicode61 remove_diacritics 2', prefix='2 3')`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS fts_minifigs USING fts5(name, fig_num, content='cat_minifigs', content_rowid='rowid', tokenize='unicode61 remove_diacritics 2', prefix='2 3')`,
		`CREATE TABLE IF NOT EXISTS recent_parts (part_num TEXT PRIMARY KEY, used_at TEXT NOT NULL)`,
		// BrickLink: colour numbers, the last known price of each item, and what you asked to be told about.
		// History: a journal of every change to your collection, and daily restore points.
		`CREATE TABLE IF NOT EXISTS journal (
			id INTEGER PRIMARY KEY AUTOINCREMENT, at TEXT NOT NULL, actor TEXT NOT NULL DEFAULT '', action TEXT NOT NULL,
			item_type TEXT NOT NULL DEFAULT 'part', item TEXT NOT NULL, color_id INTEGER NOT NULL DEFAULT -1, color_name TEXT NOT NULL DEFAULT '',
			qty_before INTEGER NOT NULL DEFAULT 0, qty_after INTEGER NOT NULL DEFAULT 0, note TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_journal_item ON journal(item, at)`,
		`CREATE TABLE IF NOT EXISTS snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT, at TEXT NOT NULL, day TEXT NOT NULL, label TEXT NOT NULL DEFAULT '',
			pieces INTEGER NOT NULL DEFAULT 0, lines INTEGER NOT NULL DEFAULT 0, sets INTEGER NOT NULL DEFAULT 0, low INTEGER NOT NULL DEFAULT 0,
			value REAL NOT NULL DEFAULT 0, priced INTEGER NOT NULL DEFAULT 0, data TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_snapshots_day ON snapshots(day)`,
		`CREATE TABLE IF NOT EXISTS bl_colors (rb_id INTEGER PRIMARY KEY, bl_id INTEGER NOT NULL, name TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS prices (
			item_type TEXT NOT NULL, item_no TEXT NOT NULL, bl_color INTEGER NOT NULL DEFAULT 0, cond TEXT NOT NULL DEFAULT 'U',
			avg REAL NOT NULL DEFAULT 0, currency TEXT NOT NULL DEFAULT '', missing INTEGER NOT NULL DEFAULT 0, fetched_at TEXT NOT NULL,
			PRIMARY KEY (item_type, item_no, bl_color, cond))`,
		`CREATE TABLE IF NOT EXISTS watchlist (
			id INTEGER PRIMARY KEY AUTOINCREMENT, item_type TEXT NOT NULL, item_no TEXT NOT NULL, color_id INTEGER NOT NULL DEFAULT -1,
			color_name TEXT NOT NULL DEFAULT '', cond TEXT NOT NULL DEFAULT 'U', max_price REAL NOT NULL,
			last_price REAL NOT NULL DEFAULT 0, last_checked TEXT NOT NULL DEFAULT '', last_alert TEXT NOT NULL DEFAULT '', alert_price REAL NOT NULL DEFAULT 0,
			UNIQUE (item_type, item_no, color_id, cond))`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("running %q: %w", s, err)
		}
	}
	if err := migrate(db); err != nil {
		return err
	}
	// A known colour is identified by its id alone (its display name may
	// vary); only free-text colours (id -1) are told apart by name. Created
	// after the migration because a legacy table has no color_id yet.
	_, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_owned_parts_key ON owned_parts(part_num, color_id, (CASE WHEN color_id >= 0 THEN '' ELSE color_name END))`)
	return err
}

// schemaVersion is the PRAGMA user_version this code expects. Version 1 added
// colour to owned_parts.
const schemaVersion = 1

// migrate brings an older lego.db up to schemaVersion. There is no migration
// framework in this repo, so this is deliberately small: it takes the write
// lock first (several processes may open the file at once, and only one may
// rebuild), re-checks the version under the lock, and rebuilds owned_parts in
// one transaction. user_version rolls back with the transaction.
func migrate(db *sql.DB) error {
	ctx := context.Background()
	// Nothing to do on an up-to-date file, and that must not need the write lock: every process
	// opens this database, and one of them may be in the middle of a long catalog refresh.
	var current int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err == nil && current >= schemaVersion {
		return nil
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("locking lego db for migration: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version < 1 {
		var hasColor int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('owned_parts') WHERE name = 'color_id'").Scan(&hasColor); err != nil {
			return err
		}
		if hasColor == 0 { // a pre-colour table: UNIQUE(part_num) can't be changed in place, so rebuild it
			rebuild := []string{
				`DROP INDEX IF EXISTS idx_owned_parts_key`,
				`ALTER TABLE owned_parts RENAME TO owned_parts_old`,
				`CREATE TABLE owned_parts (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					part_num TEXT NOT NULL,
					name TEXT NOT NULL DEFAULT '',
					category TEXT NOT NULL DEFAULT '',
					color_id INTEGER NOT NULL DEFAULT -1,
					color_name TEXT NOT NULL DEFAULT '',
					qty INTEGER NOT NULL DEFAULT 0,
					min_qty INTEGER NOT NULL DEFAULT 0,
					synced_part_id INTEGER NOT NULL DEFAULT 0,
					updated_at TEXT NOT NULL
				)`,
				`INSERT INTO owned_parts (id, part_num, name, category, color_id, color_name, qty, min_qty, synced_part_id, updated_at)
					SELECT id, part_num, name, category, -1, '', qty, 0, synced_part_id, updated_at FROM owned_parts_old`,
				`DROP TABLE owned_parts_old`,
				`CREATE UNIQUE INDEX idx_owned_parts_key ON owned_parts(part_num, color_id, (CASE WHEN color_id >= 0 THEN '' ELSE color_name END))`,
			}
			for _, q := range rebuild {
				if _, err := conn.ExecContext(ctx, q); err != nil {
					return fmt.Errorf("migrating owned_parts: %w (%s)", err, q)
				}
			}
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return err
		}
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

// path is the database file's location (tests open a second handle on it).
func (d *DB) path() string { return d.file }

// SetActor names who is making changes from now on (a user name, or "cli"); it is
// written to the journal beside each change.
func (d *DB) SetActor(name string) { d.actor = name }
