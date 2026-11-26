package lego

import (
	"sort"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
)

// Bin is one Part-DB storage location and how full it is.
type Bin struct {
	ID     int
	Name   string
	Lots   int     // part lots stored here
	Parts  int     // distinct parts
	Pieces float64 // total amount across lots
	Fill   float64 // Pieces relative to the fullest bin, 0..1 -- a picture, not a capacity
}

// BinMap lists every storage location with how much is in it, fullest first,
// so empty bins (where new stock can go) are the last rows. Capacity is not
// stored anywhere, so Fill is relative to the busiest bin.
func BinMap(pdb *partdb.DB) ([]Bin, error) {
	locs, err := pdb.AllStoreLocations()
	if err != nil {
		return nil, err
	}
	lots, err := pdb.AllPartLots()
	if err != nil {
		return nil, err
	}
	byID := map[int]*Bin{}
	parts := map[int]map[int]bool{}
	out := make([]*Bin, 0, len(locs))
	for _, l := range locs {
		b := &Bin{ID: l.ID, Name: l.Name}
		byID[l.ID] = b
		parts[l.ID] = map[int]bool{}
		out = append(out, b)
	}
	for _, lot := range lots {
		if !lot.StoreLocationID.Valid {
			continue
		}
		id := int(lot.StoreLocationID.Int64)
		if b := byID[id]; b != nil {
			b.Lots++
			b.Pieces += lot.Amount
			parts[id][lot.PartID] = true
		}
	}
	var most float64
	for _, b := range out {
		b.Parts = len(parts[b.ID])
		most = max(most, b.Pieces)
	}
	res := make([]Bin, 0, len(out))
	for _, b := range out {
		if most > 0 {
			b.Fill = b.Pieces / most
		}
		res = append(res, *b)
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].Pieces > res[j].Pieces })
	return res, nil
}
