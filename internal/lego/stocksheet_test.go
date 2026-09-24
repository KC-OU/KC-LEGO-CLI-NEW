package lego

import (
	"context"
	"strings"
	"testing"
)

func TestStockSheetUsesLastCheckWhenOneExists(t *testing.T) {
	d := buildDB(t)
	checkedFireStation(t, d, 5)
	title, lines, err := d.StockSheet(context.Background(), nil, "1-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(title, "Fire Station") {
		t.Errorf("title = %q", title)
	}
	total := 0
	for _, l := range lines {
		total += l.Expected
	}
	if total != 100 { // the set's full piece count — the sheet lists what's expected, not what's missing
		t.Errorf("expected total pieces = %d, want 100", total)
	}
	html, err := StockSheetHTML(title, lines)
	if err != nil {
		t.Fatal(err)
	}
	s := string(html)
	if !strings.Contains(s, "Fire Station") || !strings.Contains(s, "3001") || !strings.Contains(s, "Checked by") {
		t.Fatalf("sheet html:\n%s", s)
	}
}

func TestStockSheetFallsBackToCatalogWhenNeverChecked(t *testing.T) {
	d := buildDB(t)
	rb := &Client{}
	title, lines, err := d.StockSheet(context.Background(), rb, "1-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 { // set 1-1: 3001 red x60, 3023 blue x40 (see build_test.go's buildDB fixture)
		t.Fatalf("lines = %+v", lines)
	}
	total := 0
	for _, l := range lines {
		total += l.Expected
	}
	if total != 100 {
		t.Errorf("total = %d, want 100", total)
	}
	if !strings.Contains(title, "Fire Station") {
		t.Errorf("title = %q", title)
	}
}

func TestStockSheetExcludesOptionalLinesFromTheTotalButStillListsThem(t *testing.T) {
	d := buildDB(t)
	for _, q := range []string{
		`INSERT INTO cat_categories (id, name) VALUES (58, 'Stickers')`,
		`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('STK1', 'Sticker Sheet', 58)`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (11, 'STK1', 0, 1)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}
	rb := &Client{}
	title, lines, err := d.StockSheet(context.Background(), rb, "1-1")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range lines {
		if l.PartNum == "STK1" {
			found = true
			if !l.Optional {
				t.Error("the sticker line should be marked optional")
			}
		}
	}
	if !found {
		t.Fatal("the sticker line should still be listed to count against by hand")
	}
	html, err := StockSheetHTML(title, lines)
	if err != nil {
		t.Fatal(err)
	}
	s := string(html)
	if !strings.Contains(s, "100 piece(s) expected") {
		t.Errorf("the printed total must exclude the optional line's piece, got:\n%s", s)
	}
	if !strings.Contains(s, "(optional)") {
		t.Errorf("the sheet should mark the optional line, got:\n%s", s)
	}
}

func TestStockSheetUnknownSetErrors(t *testing.T) {
	d := buildDB(t)
	if _, _, err := d.StockSheet(context.Background(), &Client{}, "99999-1"); err == nil {
		t.Error("an unknown set should error, not return an empty sheet")
	}
}
