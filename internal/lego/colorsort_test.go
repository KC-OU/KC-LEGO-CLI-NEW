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
