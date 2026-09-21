package lego

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

// legacySet matches 0.json / kc_sets_export_*.json's field names exactly —
// the legacy KC-LEGO-CLI's personal-collection export schema.
type legacySet struct {
	SetID                 string          `json:"set_id"`
	SetName               string          `json:"set_name"`
	SetTheme              string          `json:"set_theme"`
	SetYear               string          `json:"set_year"`
	InstructionBookNumber string          `json:"instruction_book_number"`
	InstructionBookCount  int             `json:"instruction_book_count"`
	SetQty                int             `json:"set_qty"`
	PartsQty              int             `json:"parts_qty"`
	PartOut               bool            `json:"part_out"`
	PartOutInfo           json.RawMessage `json:"part_out_info"`
	CreatedAt             string          `json:"created_at"`
	UpdatedAt             string          `json:"updated_at"`
}

// ImportCollection reads a legacy collection export (0.json or a
// kc_sets_export_*.json snapshot) and upserts each set, keyed by set_num —
// safe to run repeatedly, or against multiple export files in sequence, with
// the last file imported winning for any set present in more than one.
func ImportCollection(db *DB, path string) (imported, skipped int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, fmt.Errorf("reading %s: %w", path, err)
	}
	var legacy []legacySet
	if err := json.Unmarshal(data, &legacy); err != nil {
		return 0, 0, fmt.Errorf("parsing %s: %w", path, err)
	}

	for _, l := range legacy {
		if l.SetID == "" {
			skipped++
			continue
		}
		year, _ := strconv.Atoi(l.SetYear)
		partOutInfo := "{}"
		if len(l.PartOutInfo) > 0 {
			partOutInfo = string(l.PartOutInfo)
		}
		s := Set{
			SetNum:                l.SetID,
			Name:                  l.SetName,
			Theme:                 l.SetTheme,
			Year:                  year,
			InstructionBookNumber: l.InstructionBookNumber,
			InstructionBookCount:  l.InstructionBookCount,
			Qty:                   l.SetQty,
			PartsQty:              l.PartsQty,
			PartedOut:             l.PartOut,
			PartedOutInfo:         partOutInfo,
			CreatedAt:             parseLegacyTime(l.CreatedAt),
		}
		if err := db.UpsertSet(s); err != nil {
			return imported, skipped, err
		}
		imported++
	}
	return imported, skipped, nil
}

func parseLegacyTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(timeLayout, s); err == nil {
		return t
	}
	// Fall back to RFC3339 in case a future export drops the microsecond suffix.
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

type legacyRefSet struct {
	SetID       string `json:"Set ID"`
	SetName     string `json:"Set Name"`
	Year        string `json:"Year"`
	Theme       string `json:"Theme"`
	TotalPieces string `json:"Total Pieces"`
}

// ImportRefSets bulk-loads the legolookup.json Rebrickable catalog snapshot
// into ref_sets, used as the local search fallback. Runs as one transaction
// with a prepared statement — the file is tens of thousands of rows but
// comfortably fits in memory for a single json.Unmarshal, no streaming
// decoder needed for a one-time import.
func ImportRefSets(db *DB, path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}
	var rows []legacyRefSet
	if err := json.Unmarshal(data, &rows); err != nil {
		return 0, fmt.Errorf("parsing %s: %w", path, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO ref_sets (set_num, name, year, theme, total_pieces) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	n := 0
	for _, r := range rows {
		if r.SetID == "" {
			continue
		}
		if _, err := stmt.Exec(r.SetID, r.SetName, r.Year, r.Theme, r.TotalPieces); err != nil {
			return n, fmt.Errorf("inserting ref set %s: %w", r.SetID, err)
		}
		n++
	}
	return n, tx.Commit()
}

type legacyRefPart struct {
	PartNumber string `json:"part_number"`
	PartName   string `json:"part_name"`
	Category   string `json:"part_category"`
}

// ImportRefParts bulk-loads the legolookup-part.json catalog snapshot into
// ref_parts, same shape as ImportRefSets.
func ImportRefParts(db *DB, path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}
	var rows []legacyRefPart
	if err := json.Unmarshal(data, &rows); err != nil {
		return 0, fmt.Errorf("parsing %s: %w", path, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO ref_parts (part_num, name, category) VALUES (?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	n := 0
	for _, r := range rows {
		if r.PartNumber == "" {
			continue
		}
		if _, err := stmt.Exec(r.PartNumber, r.PartName, r.Category); err != nil {
			return n, fmt.Errorf("inserting ref part %s: %w", r.PartNumber, err)
		}
		n++
	}
	return n, tx.Commit()
}
