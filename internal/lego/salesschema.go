package lego

// Schema for Round B (selling side): sales orders imported from BrickLink,
// price history for movers, the store's own lots for repricing suggestions,
// and acquisitions (purchase -> listed). Applied unconditionally by
// ensureSchema (all CREATE IF NOT EXISTS), so migrate only has to bump the
// version.
var schemaV15 = []string{
	`CREATE TABLE IF NOT EXISTS sales_orders (
		order_id TEXT PRIMARY KEY, buyer TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT '',
		ordered_at TEXT NOT NULL DEFAULT '', total REAL NOT NULL DEFAULT 0, currency TEXT NOT NULL DEFAULT '',
		pieces INTEGER NOT NULL DEFAULT 0, imported_at TEXT NOT NULL, picked_at TEXT NOT NULL DEFAULT '')`,
	`CREATE TABLE IF NOT EXISTS sales_order_lines (
		order_id TEXT NOT NULL, line_no INTEGER NOT NULL, item_type TEXT NOT NULL DEFAULT 'PART', item_no TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '', bl_color INTEGER NOT NULL DEFAULT 0, color_name TEXT NOT NULL DEFAULT '',
		cond TEXT NOT NULL DEFAULT 'U', qty INTEGER NOT NULL, unit_price REAL NOT NULL DEFAULT 0, picked INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (order_id, line_no))`,
	`CREATE TABLE IF NOT EXISTS price_history (
		item_type TEXT NOT NULL, item_no TEXT NOT NULL, bl_color INTEGER NOT NULL, cond TEXT NOT NULL,
		day TEXT NOT NULL, avg REAL NOT NULL, PRIMARY KEY (item_type, item_no, bl_color, cond, day))`,
	`CREATE TABLE IF NOT EXISTS store_lots (
		inventory_id INTEGER PRIMARY KEY, item_type TEXT NOT NULL, item_no TEXT NOT NULL, bl_color INTEGER NOT NULL DEFAULT 0,
		cond TEXT NOT NULL DEFAULT 'U', qty INTEGER NOT NULL DEFAULT 0, unit_price REAL NOT NULL DEFAULT 0,
		description TEXT NOT NULL DEFAULT '', synced_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS set_retail (
		set_num TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', year INTEGER NOT NULL DEFAULT 0, theme TEXT NOT NULL DEFAULT '',
		pieces INTEGER NOT NULL DEFAULT 0, launch_date TEXT NOT NULL DEFAULT '', exit_date TEXT NOT NULL DEFAULT '',
		availability TEXT NOT NULL DEFAULT '', retail_uk REAL NOT NULL DEFAULT 0, retail_us REAL NOT NULL DEFAULT 0,
		last_uk TEXT NOT NULL DEFAULT '', last_us TEXT NOT NULL DEFAULT '', fetched_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS cycle_counts (
		id INTEGER PRIMARY KEY AUTOINCREMENT, part_num TEXT NOT NULL, color_id INTEGER NOT NULL, color_name TEXT NOT NULL DEFAULT '',
		expected INTEGER NOT NULL, counted INTEGER NOT NULL, counted_by TEXT NOT NULL DEFAULT '', counted_at TEXT NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS idx_cycle_counts_part ON cycle_counts(part_num, color_id, counted_at)`,
	`CREATE TABLE IF NOT EXISTS acquisitions (
		id INTEGER PRIMARY KEY AUTOINCREMENT, label TEXT NOT NULL, supplier TEXT NOT NULL DEFAULT '', set_num TEXT NOT NULL DEFAULT '',
		cost REAL NOT NULL DEFAULT 0, currency TEXT NOT NULL DEFAULT '', stage TEXT NOT NULL, stage_at TEXT NOT NULL,
		note TEXT NOT NULL DEFAULT '', created_by TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
	// A part borrowed from one set to finish building another, until the replacement is ordered
	// from LEGO's replacement-parts service and put back.
	`CREATE TABLE IF NOT EXISTS borrowed_parts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		build_set_num TEXT NOT NULL, build_set_name TEXT NOT NULL DEFAULT '', need_part_num TEXT NOT NULL, need_element_id TEXT NOT NULL DEFAULT '',
		from_set_num TEXT NOT NULL DEFAULT '', from_set_name TEXT NOT NULL DEFAULT '', from_part_num TEXT NOT NULL DEFAULT '', from_element_id TEXT NOT NULL DEFAULT '',
		qty INTEGER NOT NULL DEFAULT 1, status TEXT NOT NULL, status_at TEXT NOT NULL,
		color_id INTEGER NOT NULL DEFAULT -1, color_name TEXT NOT NULL DEFAULT '', check_id INTEGER NOT NULL DEFAULT 0,
		note TEXT NOT NULL DEFAULT '', created_by TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
}
