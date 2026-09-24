package lego

import (
	"os"
	"path/filepath"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
)

// A report's file in WMS_EXPORT_DIR is swept after ExportDays (7 by default) — fine
// for a one-off download, but "the QR expired, can I still get that report" needs an
// answer. Every report generated (CLI or TUI) also gets a copy here, in its own
// directory with its own, much longer retention (see CleanupArchive) — the frozen
// snapshot of what was generated, not a live-regenerated view.

type ArchiveEntry struct {
	ID                           int64
	Kind, Title, File, CreatedBy string
	CreatedAt                    time.Time
}

// ArchiveReport saves a copy of body into dir (WMS_ARCHIVE_DIR) and records it.
func (d *DB) ArchiveReport(dir, kind, title, createdBy, ext string, body []byte) (int64, error) {
	path, err := exports.Save(dir, createdBy, kind, "", ext, body)
	if err != nil {
		return 0, err
	}
	res, err := d.Exec(`INSERT INTO report_archive (kind, title, file, created_by, created_at) VALUES (?,?,?,?,?)`,
		kind, title, filepath.Base(path), createdBy, time.Now().Format(timeLayout))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListArchive lists archived reports, newest first — one person's own unless all is
// true (an admin browsing everyone's).
func (d *DB) ListArchive(createdBy string, all bool) ([]ArchiveEntry, error) {
	q := `SELECT id, kind, title, file, created_by, created_at FROM report_archive`
	var args []any
	if !all {
		q += ` WHERE created_by = ?`
		args = append(args, createdBy)
	}
	rows, err := d.Query(q+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchiveEntry
	for rows.Next() {
		var e ArchiveEntry
		var created string
		if err := rows.Scan(&e.ID, &e.Kind, &e.Title, &e.File, &e.CreatedBy, &created); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(timeLayout, created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetArchiveEntry is one archived report, or nil if id doesn't exist.
func (d *DB) GetArchiveEntry(id int64) (*ArchiveEntry, error) {
	var e ArchiveEntry
	var created string
	err := d.QueryRow(`SELECT id, kind, title, file, created_by, created_at FROM report_archive WHERE id = ?`, id).
		Scan(&e.ID, &e.Kind, &e.Title, &e.File, &e.CreatedBy, &created)
	if err != nil {
		return nil, err
	}
	e.CreatedAt, _ = time.Parse(timeLayout, created)
	return &e, nil
}

// CleanupArchive deletes archive entries (and their files) older than maxAge,
// returning how many went. Called on the same schedule as exports.Cleanup, with a
// much longer maxAge — the whole point of an archive is outliving the normal
// export/link lifecycle.
func (d *DB) CleanupArchive(dir string, maxAge time.Duration) (int, error) {
	cutoff := time.Now().Add(-maxAge).Format(timeLayout)
	rows, err := d.Query(`SELECT id, file FROM report_archive WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	type victim struct {
		id   int64
		file string
	}
	var victims []victim
	for rows.Next() {
		var v victim
		if err := rows.Scan(&v.id, &v.file); err != nil {
			rows.Close()
			return 0, err
		}
		victims = append(victims, v)
	}
	rows.Close()
	n := 0
	for _, v := range victims {
		_ = os.Remove(filepath.Join(dir, v.file))
		if _, err := d.Exec(`DELETE FROM report_archive WHERE id = ?`, v.id); err == nil {
			n++
		}
	}
	return n, nil
}
