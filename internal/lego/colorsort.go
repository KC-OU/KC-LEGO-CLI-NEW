package lego

import (
	"sort"
	"strings"
)

// colorPriority ranks black, red and blue ahead of everything else, in that order —
// a deliberate choice, not a generic colour-wheel scheme; everything else falls
// through to plain alphabetical.
var colorPriority = map[string]int{"black": 0, "red": 1, "blue": 2}

// ColorSortKey is the sort key for "black, red, blue, then alphabetical" — used
// everywhere a parts list groups by colour (Owned Parts, a set's parts check,
// exports). rank breaks the tie first; name (lower-cased) alphabetises the rest, and
// is "" for a prioritised colour so two differently-cased spellings of the same
// priority colour ("Black", "BLACK") still land in one group.
func ColorSortKey(name string) (rank int, name2 string) {
	lower := strings.ToLower(strings.TrimSpace(name))
	if r, ok := colorPriority[lower]; ok {
		return r, ""
	}
	return len(colorPriority), lower
}

// LessColorThenCategory orders two parts by colour first (see ColorSortKey), then
// category alphabetically, then by name — a full, deterministic "so it's lookable"
// order: group by colour, then by what kind of part it is within that colour.
func LessColorThenCategory(colorA, categoryA, nameA, colorB, categoryB, nameB string) bool {
	ra, ka := ColorSortKey(colorA)
	rb, kb := ColorSortKey(colorB)
	if ra != rb {
		return ra < rb
	}
	if ka != kb {
		return ka < kb
	}
	ca, cb := strings.ToLower(categoryA), strings.ToLower(categoryB)
	if ca != cb {
		return ca < cb
	}
	return strings.ToLower(nameA) < strings.ToLower(nameB)
}

// User-chosen sort keys for a part+colour+qty list (InvItem, CheckLine,
// MissingLine) -- an alternative to each type's own fixed default order
// (e.g. sortLinesByColorThenCategory), picked by a user in the dashboard,
// CLI, or TUI. SortColor reuses ColorSortKey so it agrees with every other
// colour-first ordering in the codebase rather than being plain alphabetical.
const (
	SortPartID = "part_id"
	SortColor  = "color"
	SortQty    = "qty"
)

// SortBy reorders items in place by part ID, colour, or quantity (highest
// first). An empty or unrecognized key is a no-op, leaving the caller's
// existing order untouched -- so call sites can pass a raw query/flag value
// straight through without validating it first.
func SortBy[T any](items []T, by string, partNum, colorName func(T) string, qty func(T) int) {
	switch by {
	case SortPartID:
		sort.SliceStable(items, func(i, j int) bool { return partNum(items[i]) < partNum(items[j]) })
	case SortColor:
		sort.SliceStable(items, func(i, j int) bool {
			ra, ka := ColorSortKey(colorName(items[i]))
			rb, kb := ColorSortKey(colorName(items[j]))
			if ra != rb {
				return ra < rb
			}
			return ka < kb
		})
	case SortQty:
		sort.SliceStable(items, func(i, j int) bool { return qty(items[i]) > qty(items[j]) })
	}
}
