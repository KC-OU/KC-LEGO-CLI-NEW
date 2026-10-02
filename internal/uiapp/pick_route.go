package uiapp

import (
	"sort"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// Pick-route ordering: a check/order's lines are shown in shelf order instead
// of catalog order, so working through one walks the shelves in a single
// pass. A part with no known Part-DB location (not stocked there yet, or no
// synced link) sorts after every located part, keeping its original order.

// locationNames resolves each part+colour's shelf location the same way
// owned_parts links to Part-DB (see partsync.go) — "" when unresolved.
func (a *App) locationNames(partNum []string, colorID []int, colorName []string) []string {
	out := make([]string, len(partNum))
	if a.pdb == nil {
		return out
	}
	lots, err := a.pdb.AllPartLots()
	if err != nil {
		return out
	}
	locByPart := map[int]int{} // Part-DB part id -> store location id
	for _, l := range lots {
		if l.StoreLocationID.Valid {
			if _, ok := locByPart[l.PartID]; !ok {
				locByPart[l.PartID] = int(l.StoreLocationID.Int64)
			}
		}
	}
	locs, err := a.pdb.AllStoreLocations()
	if err != nil {
		return out
	}
	nameByLoc := map[int]string{}
	for _, s := range locs {
		nameByLoc[s.ID] = s.Name
	}
	for i := range partNum {
		op, err := a.legoDB.GetOwnedPart(partNum[i], colorID[i], colorName[i])
		if err != nil || op == nil {
			continue
		}
		if lid, ok := locByPart[op.SyncedPartID]; ok {
			out[i] = nameByLoc[lid]
		}
	}
	return out
}

// locLess sorts known locations alphabetically, ahead of anywhere unknown.
func locLess(a, b string) bool {
	if a == "" {
		return false
	}
	if b == "" {
		return true
	}
	return a < b
}

// sortCheckLinesByLocation reorders lines in place — called once, when a
// check is opened, so the undo history's line indices stay meaningful.
func (a *App) sortCheckLinesByLocation(lines []lego.CheckLine) {
	if len(lines) < 2 {
		return
	}
	part, color, colorName := make([]string, len(lines)), make([]int, len(lines)), make([]string, len(lines))
	for i, l := range lines {
		part[i], color[i], colorName[i] = l.PartNum, l.ColorID, l.ColorName
	}
	loc := a.locationNames(part, color, colorName)
	type pair struct {
		line lego.CheckLine
		loc  string
	}
	pairs := make([]pair, len(lines))
	for i, l := range lines {
		pairs[i] = pair{l, loc[i]}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return locLess(pairs[i].loc, pairs[j].loc) })
	for i, p := range pairs {
		lines[i] = p.line
	}
}

// sortedOrderLines returns a copy of lines in shelf order — called on every
// render (order lines can be edited/received in place), so it never mutates
// the caller's slice.
func (a *App) sortedOrderLines(lines []lego.OrderLine) []lego.OrderLine {
	if len(lines) < 2 {
		return lines
	}
	part, color, colorName := make([]string, len(lines)), make([]int, len(lines)), make([]string, len(lines))
	for i, l := range lines {
		part[i], color[i], colorName[i] = l.PartNum, l.ColorID, l.ColorName
	}
	loc := a.locationNames(part, color, colorName)
	type pair struct {
		line lego.OrderLine
		loc  string
	}
	pairs := make([]pair, len(lines))
	for i, l := range lines {
		pairs[i] = pair{l, loc[i]}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return locLess(pairs[i].loc, pairs[j].loc) })
	out := make([]lego.OrderLine, len(lines))
	for i, p := range pairs {
		out[i] = p.line
	}
	return out
}
