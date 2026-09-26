package lego

import "strings"

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
