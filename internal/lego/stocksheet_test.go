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

func TestStockSheetUnknownSetErrors(t *testing.T) {
	d := buildDB(t)
	if _, _, err := d.StockSheet(context.Background(), &Client{}, "99999-1"); err == nil {
		t.Error("an unknown set should error, not return an empty sheet")
	}
}
