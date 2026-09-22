package lego

import (
	"context"
	"fmt"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
)

// PartSyncer pushes owned LEGO parts into the shared Part-DB inventory through
// its REST API — one-way (LEGO to Part-DB), used both right after an add and,
// for anything that could not be pushed then (no token yet, Part-DB down), by
// `wms lego sync-parts`. Sets never take part: only individual parts do.
type PartSyncer struct {
	Lego   *DB
	Writer *partdb.Writer
}

type PartSyncResult struct {
	Created   int
	Updated   int
	Unchanged int
	Errors    []error
}

// Push makes one owned part exist in Part-DB with the quantity you hold, and
// records the Part-DB id on the LEGO row.
func (s *PartSyncer) Push(ctx context.Context, p OwnedPart) (*partdb.UpsertResult, error) {
	catID, err := s.Writer.ResolveCategory(ctx, CategoryPath(p.Category))
	if err != nil {
		return nil, err
	}
	return s.PushIn(ctx, p, catID)
}

// PushIn is Push into a specific Part-DB category (one the user picked, or
// already resolved) instead of Lego > <the part's category>.
func (s *PartSyncer) PushIn(ctx context.Context, p OwnedPart, catID int) (*partdb.UpsertResult, error) {
	res, err := s.Writer.UpsertPart(ctx, SpecFor(p, catID), float64(p.Qty))
	if err != nil {
		return nil, err
	}
	if err := s.Lego.SetSyncedPartID(p.ID, res.PartID); err != nil {
		return res, fmt.Errorf("saved to Part-DB but could not record that: %w", err)
	}
	return res, nil
}

// SyncNow pushes every owned part. Idempotent: re-running with nothing changed
// creates and updates nothing. Without a Part-DB API token it stops at once
// with one clear error instead of one per part.
func (s *PartSyncer) SyncNow(ctx context.Context) (*PartSyncResult, error) {
	if !s.Writer.API().Enabled() {
		return nil, partdb.ErrNoToken
	}
	owned, err := s.Lego.ListOwnedParts()
	if err != nil {
		return nil, err
	}
	res := &PartSyncResult{}
	for _, p := range owned {
		r, err := s.Push(ctx, p)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("part %s %s: %w", p.PartNum, p.ColorName, err))
			continue
		}
		switch {
		case r.Created:
			res.Created++
		case r.PrevQty != r.NewQty:
			res.Updated++
		default:
			res.Unchanged++
		}
	}
	return res, nil
}

// SetPushResult is what PushSet did.
type SetPushResult struct {
	LocationID     int
	Lines, Created int
	Changed        int
	Errors         []error
}

// PushSet makes Part-DB hold a checked set's parts in the set's own storage
// location ("LEGO / Sets / <num> <name>"): every part+colour gets a lot there with
// the count you have, so a stock check can count the set on its own. Parts are
// shared with your loose stock (same IPN); only the set's lot is touched.
func (s *PartSyncer) PushSet(ctx context.Context, c *SetCheck, setName string) (*SetPushResult, error) {
	if !s.Writer.API().Enabled() {
		return nil, partdb.ErrNoToken
	}
	name := strings.TrimSpace(c.SetNum + " " + setName)
	loc, err := s.Writer.ResolveLocation(ctx, append(append([]string{}, partdb.SetsLocationPath...), name))
	if err != nil {
		return nil, err
	}
	_ = s.Lego.SetPDBLocation(c.SetNum, loc)
	res := &SetPushResult{LocationID: loc}
	cats := map[string]int{}
	for _, l := range c.Lines {
		res.Lines++
		catID, ok := cats[l.Category]
		if !ok {
			if catID, err = s.Writer.ResolveCategory(ctx, CategoryPath(l.Category)); err != nil {
				res.Errors = append(res.Errors, fmt.Errorf("%s: %w", l.PartNum, err))
				continue
			}
			cats[l.Category] = catID
		}
		spec := SpecFor(OwnedPart{PartNum: l.PartNum, Name: l.PartName, Category: l.Category, ColorID: l.ColorID, ColorName: l.ColorName}, catID)
		spec.MinAmount = nil // a set's parts don't change the part's minimum
		id, created, err := s.Writer.EnsurePart(ctx, spec)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("%s: %w", l.PartNum, err))
			continue
		}
		if created {
			res.Created++
		}
		prev, err := s.Writer.SetLotAt(ctx, id, loc, float64(min(l.Have, l.Need)))
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("%s: %w", l.PartNum, err))
			continue
		}
		if prev != float64(min(l.Have, l.Need)) {
			res.Changed++
		}
	}
	return res, nil
}
