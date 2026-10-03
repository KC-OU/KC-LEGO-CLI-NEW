package lego

import "context"

// collectionTables is every table reset.go clears — what you own and its
// history — never the offline catalog (cat_*, ref_*, rb_*), price/reference
// caches (prices, bl_colors, bo_prices, retirements), a sticky preference
// (part_optional), or anything session/messaging-related (live_sessions,
// mobile_sessions, user_messages, handover_note, admin_events, force_logoffs)
// — none of that is "what sets and parts you have."
var collectionTables = []string{
	"owned_parts", "sets", "set_check_lines", "set_checks", "set_state",
	"part_origins", "order_lines", "orders", "job_tickets",
	"accuracy_reports", "accuracy_log", "report_archive", "snapshots", "journal",
	"recent_parts",
}

// CollectionCounts is the current row count of every table ResetCollection
// would clear, keyed by table name — for a confirmation prompt or --dry-run
// to show exactly what's about to go.
func (d *DB) CollectionCounts() (map[string]int, error) {
	counts := make(map[string]int, len(collectionTables))
	for _, tbl := range collectionTables {
		var n int
		if err := d.QueryRow("SELECT COUNT(*) FROM " + tbl).Scan(&n); err != nil {
			return nil, err
		}
		counts[tbl] = n
	}
	return counts, nil
}

// ResetCollection clears every set, owned part, check, order, ticket,
// accuracy record and journal entry — the whole "what you have and what
// happened to it" half of lego.db — leaving the offline catalog, reference
// caches and settings exactly as they were. Callers back up first (see
// DB.BackupCollection); this has no undo of its own beyond that. One journal
// row survives: a note that a reset happened, written after the wipe so
// `wms lego history` shows it.
func (d *DB) ResetCollection(ctx context.Context, note string) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, tbl := range collectionTables {
		if _, err := tx.Exec("DELETE FROM " + tbl); err != nil {
			return err
		}
	}
	d.journal(tx, "reset", "collection", "all", -1, "", 0, 0, note)
	return tx.Commit()
}
