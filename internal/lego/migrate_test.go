package lego

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleCollectionJSON = `[
  {
    "set_id": "71788",
    "set_name": "Lloyd's Ninja Street Bike",
    "set_theme": "Ninjago Core",
    "set_year": "2023",
    "instruction_book_number": "6449669",
    "instruction_book_count": 1,
    "set_qty": 1,
    "parts_qty": 56,
    "part_out": false,
    "part_out_info": {},
    "created_at": "2025-07-09T07:23:32.365745",
    "updated_at": "2025-07-09T07:24:19.613674"
  },
  {
    "set_id": "75344",
    "set_name": "Boba Fett's Starship Microfighter",
    "set_theme": "Star Wars",
    "set_year": "2023",
    "instruction_book_number": "6417983/6417984",
    "instruction_book_count": 2,
    "set_qty": 1,
    "parts_qty": 80,
    "part_out": false,
    "part_out_info": {},
    "created_at": "2025-07-09T07:27:05.281800",
    "updated_at": "2025-07-09T07:27:05.281828"
  }
]`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing fixture %s: %v", name, err)
	}
	return path
}

func TestImportCollection(t *testing.T) {
	db := newFixture(t)
	path := writeTemp(t, "0.json", sampleCollectionJSON)

	imported, skipped, err := ImportCollection(db, path)
	if err != nil {
		t.Fatalf("ImportCollection: %v", err)
	}
	if imported != 2 || skipped != 0 {
		t.Fatalf("expected 2 imported/0 skipped, got %d/%d", imported, skipped)
	}

	s, err := db.GetSetByNum("75344")
	if err != nil {
		t.Fatalf("GetSetByNum: %v", err)
	}
	if s.InstructionBookNumber != "6417983/6417984" {
		t.Errorf("multi-value instruction book number not preserved: %q", s.InstructionBookNumber)
	}
	if s.Year != 2023 {
		t.Errorf("expected year 2023, got %d", s.Year)
	}

	// Re-import must be idempotent: no duplicate rows.
	imported, _, err = ImportCollection(db, path)
	if err != nil {
		t.Fatalf("second ImportCollection: %v", err)
	}
	if imported != 2 {
		t.Fatalf("expected second import to still report 2 upserts, got %d", imported)
	}
	all, err := db.SearchSets("")
	if err != nil {
		t.Fatalf("SearchSets: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected exactly 2 sets after re-import, got %d", len(all))
	}
}

func TestImportRefSetsAndParts(t *testing.T) {
	db := newFixture(t)
	setsPath := writeTemp(t, "legolookup.json", `[
		{"Set ID": "001-1", "Set Name": "Gears", "Year": "1965", "Theme": "Samsonite", "Total Pieces": "43"}
	]`)
	partsPath := writeTemp(t, "legolookup-part.json", `[
		{"part_number": "3001", "part_name": "Brick 2x4", "part_category": "Bricks"}
	]`)

	n, err := ImportRefSets(db, setsPath)
	if err != nil {
		t.Fatalf("ImportRefSets: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 ref set imported, got %d", n)
	}
	refSets, err := db.SearchRefSets("Gears")
	if err != nil {
		t.Fatalf("SearchRefSets: %v", err)
	}
	if len(refSets) != 1 || refSets[0].SetNum != "001-1" {
		t.Fatalf("unexpected ref set search result: %+v", refSets)
	}

	n, err = ImportRefParts(db, partsPath)
	if err != nil {
		t.Fatalf("ImportRefParts: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 ref part imported, got %d", n)
	}
	refParts, err := db.SearchRefParts("Brick")
	if err != nil {
		t.Fatalf("SearchRefParts: %v", err)
	}
	if len(refParts) != 1 || refParts[0].PartNum != "3001" {
		t.Fatalf("unexpected ref part search result: %+v", refParts)
	}
}
