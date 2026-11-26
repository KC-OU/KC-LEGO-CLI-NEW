package lego

import (
	"sort"
	"testing"
)

func TestColorSortKeyOrdersBlackRedBlueThenAlphabetical(t *testing.T) {
	names := []string{"Green", "Blue", "Yellow", "Black", "White", "Red"}
	sort.Slice(names, func(i, j int) bool {
		ri, ki := ColorSortKey(names[i])
		rj, kj := ColorSortKey(names[j])
		if ri != rj {
			return ri < rj
		}
		return ki < kj
	})
	want := []string{"Black", "Red", "Blue", "Green", "White", "Yellow"}
	for i, w := range want {
		if names[i] != w {
			t.Fatalf("order = %v, want %v", names, want)
		}
	}
}

func TestColorSortKeyIsCaseInsensitive(t *testing.T) {
	r1, k1 := ColorSortKey("BLACK")
	r2, k2 := ColorSortKey("black")
	if r1 != r2 || k1 != k2 {
		t.Errorf("BLACK vs black: (%d,%q) vs (%d,%q)", r1, k1, r2, k2)
	}
}

func TestSortByOrdersOnPartIDColorAndQty(t *testing.T) {
	type row struct {
		part, color string
		qty         int
	}
	fresh := func() []row {
		return []row{
			{"3023", "Blue", 4},
			{"3001", "Black", 10},
			{"3010", "Red", 1},
		}
	}
	part := func(r row) string { return r.part }
	color := func(r row) string { return r.color }
	qty := func(r row) int { return r.qty }

	byPart := fresh()
	SortBy(byPart, SortPartID, part, color, qty)
	if got := []string{byPart[0].part, byPart[1].part, byPart[2].part}; got[0] != "3001" || got[1] != "3010" || got[2] != "3023" {
		t.Fatalf("SortBy(part_id) = %v", got)
	}

	byColor := fresh()
	SortBy(byColor, SortColor, part, color, qty)
	if got := []string{byColor[0].color, byColor[1].color, byColor[2].color}; got[0] != "Black" || got[1] != "Red" || got[2] != "Blue" {
		t.Fatalf("SortBy(color) = %v, want colour-priority order (Black, Red, Blue)", got)
	}

	byQty := fresh()
	SortBy(byQty, SortQty, part, color, qty)
	if got := []int{byQty[0].qty, byQty[1].qty, byQty[2].qty}; got[0] != 10 || got[1] != 4 || got[2] != 1 {
		t.Fatalf("SortBy(qty) = %v, want highest first", got)
	}

	unchanged := fresh()
	SortBy(unchanged, "", part, color, qty)
	if unchanged[0].part != "3023" {
		t.Fatalf("SortBy(\"\") should be a no-op, got %v", unchanged)
	}
	SortBy(unchanged, "bogus", part, color, qty)
	if unchanged[0].part != "3023" {
		t.Fatalf("SortBy(unrecognized key) should be a no-op, got %v", unchanged)
	}
}

func TestLessColorThenCategoryGroupsCategoryWithinColour(t *testing.T) {
	type row struct{ color, category, name string }
	rows := []row{
		{"Blue", "Plates", "Plate 1x3"},
		{"Black", "Plates", "Plate 1x2"},
		{"Red", "Bricks", "Brick 2x4"},
		{"Blue", "Bricks", "Brick 1x2"},
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		return LessColorThenCategory(a.color, a.category, a.name, b.color, b.category, b.name)
	})
	want := []row{
		{"Black", "Plates", "Plate 1x2"},
		{"Red", "Bricks", "Brick 2x4"},
		{"Blue", "Bricks", "Brick 1x2"},
		{"Blue", "Plates", "Plate 1x3"},
	}
	for i, w := range want {
		if rows[i] != w {
			t.Fatalf("rows = %+v, want %+v", rows, want)
		}
	}
}
