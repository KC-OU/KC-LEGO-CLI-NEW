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
		// A set's parts check (intake when added, recount for a stock check), and what the set is short.
		`CREATE TABLE IF NOT EXISTS set_state (
			set_num TEXT PRIMARY KEY, location TEXT NOT NULL DEFAULT '', condition TEXT NOT NULL DEFAULT '', condition_note TEXT NOT NULL DEFAULT '',
			missing_qty INTEGER NOT NULL DEFAULT 0, last_check_id INTEGER NOT NULL DEFAULT 0, pdb_location_id INTEGER NOT NULL DEFAULT 0,
			image_url TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS set_checks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, set_num TEXT NOT NULL, kind TEXT NOT NULL, status TEXT NOT NULL,
			checked_by TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL, finished_at TEXT NOT NULL DEFAULT '',
			lines INTEGER NOT NULL DEFAULT 0, pieces INTEGER NOT NULL DEFAULT 0, have INTEGER NOT NULL DEFAULT 0,
			missing INTEGER NOT NULL DEFAULT 0, extra INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS idx_set_checks_set ON set_checks(set_num, id)`,
		`CREATE TABLE IF NOT EXISTS set_check_lines (
			check_id INTEGER NOT NULL, part_num TEXT NOT NULL, part_name TEXT NOT NULL DEFAULT '', category TEXT NOT NULL DEFAULT '',
			color_id INTEGER NOT NULL, color_name TEXT NOT NULL DEFAULT '', bl_id TEXT NOT NULL DEFAULT '', bl_color INTEGER NOT NULL DEFAULT 0,
			need INTEGER NOT NULL, have INTEGER NOT NULL, extra INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (check_id, part_num, color_id))`,
		// How many of a loose part came from a set's extras (for "2 spare in 10696").
		`CREATE TABLE IF NOT EXISTS part_origins (
			part_num TEXT NOT NULL, color_id INTEGER NOT NULL, origin_set TEXT NOT NULL, qty INTEGER NOT NULL,
			PRIMARY KEY (part_num, color_id, origin_set))`,
		`CREATE TABLE IF NOT EXISTS orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT, supplier_kind TEXT NOT NULL, supplier TEXT NOT NULL DEFAULT '',
			order_no TEXT NOT NULL DEFAULT '', invoice_no TEXT NOT NULL DEFAULT '', tracking_no TEXT NOT NULL DEFAULT '', carrier TEXT NOT NULL DEFAULT '',
			currency TEXT NOT NULL DEFAULT '', shipping REAL NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'wanted',
			created_at TEXT NOT NULL, ordered_at TEXT NOT NULL DEFAULT '', shipped_at TEXT NOT NULL DEFAULT '', received_at TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '', created_by TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS order_lines (
			id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER NOT NULL, set_num TEXT NOT NULL DEFAULT '',
			part_num TEXT NOT NULL, color_id INTEGER NOT NULL, color_name TEXT NOT NULL DEFAULT '', part_name TEXT NOT NULL DEFAULT '',
			qty INTEGER NOT NULL, unit_price REAL NOT NULL DEFAULT 0, received_qty INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS idx_order_lines_order ON order_lines(order_id)`,
		`CREATE TABLE IF NOT EXISTS bo_prices (
			bl_id TEXT NOT NULL, bl_color INTEGER NOT NULL, boid TEXT NOT NULL DEFAULT '', avg REAL NOT NULL DEFAULT 0, low REAL NOT NULL DEFAULT 0,
			currency TEXT NOT NULL DEFAULT '', missing INTEGER NOT NULL DEFAULT 0, fetched_at TEXT NOT NULL,
			PRIMARY KEY (bl_id, bl_color))`,
		// A community-sourced retirement-date table (see retirement.go), fully replaced on every
		// import/refresh — the source sheet is the source of truth, there is nothing to merge.
		`CREATE TABLE IF NOT EXISTS retirements (
			set_num TEXT PRIMARY KEY, theme TEXT NOT NULL DEFAULT '', subtheme TEXT NOT NULL DEFAULT '',
			set_name TEXT NOT NULL DEFAULT '', retirement_date TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL)`,
		// A permanent-ish copy of every generated report (see archive.go) — separate from
		// WMS_EXPORT_DIR's own short-lived files, so an expired QR/link never means "and it's gone".
		`CREATE TABLE IF NOT EXISTS report_archive (
			id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, title TEXT NOT NULL DEFAULT '',
			file TEXT NOT NULL, created_by TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_report_archive_creator ON report_archive(created_by, id)`,
		// An explicit override of whether a part counts toward missing/completion totals
		// (see IsOptional): a part with no row here defaults to optional only if its
		// category is Stickers, so every sticker is covered with nothing to migrate, and
		// toggling any part (sticker or not) here overrides that default either way.
		`CREATE TABLE IF NOT EXISTS part_optional (part_num TEXT PRIMARY KEY, optional INTEGER NOT NULL)`,
		// A unit of assigned work: an admin points a picker/checker at a set to check or
		// an order to pick, either by name (assigned_to set) or left for the open queue
		// (assigned_to ""). Claiming it just means starting the underlying check/order
		// normally — this table only tracks who's on it and its lifecycle, never a copy
		// of the check/order data itself. token is a barcode claim code (see tickets.go).
		`CREATE TABLE IF NOT EXISTS job_tickets (
			id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, target TEXT NOT NULL, label TEXT NOT NULL DEFAULT '',
			assigned_to TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'queued', priority TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '', token TEXT NOT NULL DEFAULT '', created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, claimed_at TEXT NOT NULL DEFAULT '', done_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_job_tickets_status ON job_tickets(kind, status, assigned_to)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_job_tickets_token ON job_tickets(token) WHERE token != ''`,
		// One row per accuracy-affecting event, scored per role (a picker and checker
		// percentage are separate for someone holding both) and grouped by day (see
		// accuracy.go) — "today" is just "day = today's date", so nothing has to reset at
		// midnight, a new day's rows simply haven't been written yet.
		`CREATE TABLE IF NOT EXISTS accuracy_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL, role TEXT NOT NULL, day TEXT NOT NULL,
			kind TEXT NOT NULL, target TEXT NOT NULL DEFAULT '', pieces INTEGER NOT NULL DEFAULT 0, missing INTEGER NOT NULL DEFAULT 0,
			delta REAL NOT NULL, reason TEXT NOT NULL DEFAULT '', created_by TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_accuracy_log_user_day ON accuracy_log(username, role, day)`,
		// A manager's formal write-up of one 21+-missing escalation (see
		// RecordCheckOutcome/accKindEscalate in accuracy.go) — escalation_id points
		// at that triggering accuracy_log row. An escalation counts as resolved the
		// moment a report referencing it exists (see OpenAccuracyEscalations), so
		// there's no separate status column to drift out of sync with reality.
		`CREATE TABLE IF NOT EXISTS accuracy_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT, escalation_id INTEGER NOT NULL, username TEXT NOT NULL, role TEXT NOT NULL,
			reviewed_by TEXT NOT NULL, summary TEXT NOT NULL, action_taken TEXT NOT NULL,
			talk_requested INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_accuracy_reports_escalation ON accuracy_reports(escalation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_accuracy_reports_user ON accuracy_reports(username, role)`,
		// A one-way admin-to-user note (see messages.go): delivered_at is set the first
		// time the recipient's own session notices it (polling, not a push — sessions are
		// separate processes), read_at when they dismiss the toast.
		`CREATE TABLE IF NOT EXISTS user_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT, from_user TEXT NOT NULL, to_user TEXT NOT NULL, body TEXT NOT NULL,
			created_at TEXT NOT NULL, delivered_at TEXT NOT NULL DEFAULT '', read_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_user_messages_to ON user_messages(to_user, delivered_at)`,
		// A live registry of connected TUI sessions (see sessions.go) — each session
		// upserts its own row on a slow timer and deletes it on exit; a session that
		// crashed instead of exiting cleanly just ages out (admins see last_seen).
		`CREATE TABLE IF NOT EXISTS live_sessions (
			session_id TEXT PRIMARY KEY, username TEXT NOT NULL DEFAULT '', role TEXT NOT NULL DEFAULT '',
			transport TEXT NOT NULL DEFAULT '', remote_addr TEXT NOT NULL DEFAULT '', screen TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL, last_seen TEXT NOT NULL)`,
		// A one-shot flag an admin sets to force a specific live session to sign
		// off at its next idle tick (see ForceLogoff/ConsumeForceLogoff) — kept
		// separate from live_sessions itself, which stays purely diagnostic and
		// never access-related.
		`CREATE TABLE IF NOT EXISTS force_logoffs (
			session_id TEXT PRIMARY KEY, message TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		// A mobile app's signed-in session — a per-shift bearer token, not a
		// per-command credential like the Discord bot's PIN (see internal/
		// mobileapi), since a phone stays signed in rather than re-proving
		// identity on every scan. needs_2fa marks a password-verified-but-
		// not-yet-2FA'd login (see mobileapi.Login): the token exists but
		// isn't valid for anything else until the second step clears the
		// flag and extends expires_at to the full session length.
		`CREATE TABLE IF NOT EXISTS mobile_sessions (
			token TEXT PRIMARY KEY, username TEXT NOT NULL, source TEXT NOT NULL, role TEXT NOT NULL DEFAULT '',
			needs_2fa INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, expires_at TEXT NOT NULL)`,
		// The current shift handover note (see handover.go) — one row, always id 1,
		// overwritten by whoever last saved it.
		`CREATE TABLE IF NOT EXISTS handover_note (
			id INTEGER PRIMARY KEY CHECK (id = 1), body TEXT NOT NULL DEFAULT '', created_by TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT '')`,
		// A structured feed of admin-relevant events (see events.go) for the
		// "Recent Activity" screen — deliberately separate from the tamper-evident
		// audit log (internal/audit), which stays a hash-chained security record.
		`CREATE TABLE IF NOT EXISTS admin_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, actor TEXT NOT NULL DEFAULT '',
			target TEXT NOT NULL DEFAULT '', detail TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_events_created ON admin_events(id)`,
		// Who's scheduled to work which day (see attendance.go) — one row per
		// user per date, start/end are free-text ("09:00") for display only,
		// nothing here enforces them. emergency_override marks a row an admin
		// added on the spot to let someone work a day they weren't on the
		// rota for (the "quick NS override"), rather than a planned shift —
		// IsScheduled treats both the same; the flag is just for the record.
		`CREATE TABLE IF NOT EXISTS rota_entries (
			username TEXT NOT NULL, date TEXT NOT NULL, start_time TEXT NOT NULL DEFAULT '', end_time TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '', emergency_override INTEGER NOT NULL DEFAULT 0,
			created_by TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
			PRIMARY KEY (username, date))`,
		`CREATE INDEX IF NOT EXISTS idx_rota_entries_date ON rota_entries(date)`,
		// One row per clock-in; clock_out_at "" means still clocked in (see
		// attendance.go). A username can have at most one open (clock_out_at
		// = '') row at a time — enforced in Go (ClockIn checks first), not by
		// a partial unique index, to keep the "already clocked in" error
		// message specific rather than a generic constraint failure.
		`CREATE TABLE IF NOT EXISTS clock_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL,
			clock_in_at TEXT NOT NULL, clock_out_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_clock_events_user ON clock_events(username, clock_in_at)`,
		// A paid break within an open shift (see attendance.go's StartBreak/
		// EndBreak/OnBreak) — end_at "" means still on break, same convention
		// as clock_events.clock_out_at. A shift's breaks don't need their own
		// username column: clock_event_id already ties each one to exactly
		// one shift, which already has one.
		`CREATE TABLE IF NOT EXISTS clock_breaks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, clock_event_id INTEGER NOT NULL,
			start_at TEXT NOT NULL, end_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_clock_breaks_event ON clock_breaks(clock_event_id)`,
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
// colour to owned_parts. Version 2 added set_state.image_url (an admin's
// override of a set's catalog picture, read ahead of cat_sets.img_url so it
// survives the next Rebrickable catalog sync — see DB.CatalogSet). Version 3
// added rota_entries/clock_events, version 4 clock_breaks (see attendance.go).
const schemaVersion = 4

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
	}
	if version < 2 {
		var hasImageURL int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('set_state') WHERE name = 'image_url'").Scan(&hasImageURL); err != nil {
			return err
		}
		if hasImageURL == 0 {
			if _, err := conn.ExecContext(ctx, `ALTER TABLE set_state ADD COLUMN image_url TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("migrating set_state: %w", err)
			}
		}
	}
	if version < 3 {
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS rota_entries (
				username TEXT NOT NULL, date TEXT NOT NULL, start_time TEXT NOT NULL DEFAULT '', end_time TEXT NOT NULL DEFAULT '',
				note TEXT NOT NULL DEFAULT '', emergency_override INTEGER NOT NULL DEFAULT 0,
				created_by TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
				PRIMARY KEY (username, date))`,
			`CREATE INDEX IF NOT EXISTS idx_rota_entries_date ON rota_entries(date)`,
			`CREATE TABLE IF NOT EXISTS clock_events (
				id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL,
				clock_in_at TEXT NOT NULL, clock_out_at TEXT NOT NULL DEFAULT '')`,
			`CREATE INDEX IF NOT EXISTS idx_clock_events_user ON clock_events(username, clock_in_at)`,
		}
		for _, s := range stmts {
			if _, err := conn.ExecContext(ctx, s); err != nil {
				return fmt.Errorf("migrating in rota_entries/clock_events: %w (%s)", err, s)
			}
		}
	}
	if version < 4 {
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS clock_breaks (
				id INTEGER PRIMARY KEY AUTOINCREMENT, clock_event_id INTEGER NOT NULL,
				start_at TEXT NOT NULL, end_at TEXT NOT NULL DEFAULT '')`,
			`CREATE INDEX IF NOT EXISTS idx_clock_breaks_event ON clock_breaks(clock_event_id)`,
		}
		for _, s := range stmts {
			if _, err := conn.ExecContext(ctx, s); err != nil {
				return fmt.Errorf("migrating in clock_breaks: %w (%s)", err, s)
			}
		}
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return err
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
