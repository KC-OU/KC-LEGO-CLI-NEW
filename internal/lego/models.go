// Package lego stores and looks up a personal LEGO collection: owned sets
// (in their own SQLite file, never touching Part-DB or ModernWMS) and
// individual LEGO parts (which sync one-way into the existing Part-DB, the
// same inventory the rest of this app already manages). It replaces the
// legacy bash/jq/Python KC-LEGO-CLI tool with native Go: no shell-outs, no
// jq filter strings built from user input.
package lego

import "time"

// Set mirrors the legacy KC-LEGO-CLI collection JSON schema (0.json /
// kc_sets_export_*.json) field-for-field, plus an auto-increment ID.
type Set struct {
	ID                    int64
	SetNum                string // legacy "set_id", e.g. "71788"
	Name                  string
	Theme                 string
	Year                  int
	InstructionBookNumber string // free text; legacy data has multi-value strings like "6417983/6417984"
	InstructionBookCount  int
	Qty                   int // "set_qty" — how many copies owned
	PartsQty              int
	PartedOut             bool
	PartedOutInfo         string // raw JSON text; every legacy record has "{}" and no set uses structured sub-fields yet
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// RefSet is one row of the imported legolookup.json Rebrickable catalog
// snapshot, used as a local fallback when no API key is configured or the
// live API call fails.
type RefSet struct {
	SetNum      string // "Set ID"
	Name        string // "Set Name"
	Year        string // kept as text — legacy data includes non-numeric years for some multi-packs
	Theme       string
	TotalPieces string
}

// RefPart is one row of the imported legolookup-part.json catalog snapshot.
type RefPart struct {
	PartNum  string // "part_number"
	Name     string // "part_name"
	Category string // "part_category"
}
