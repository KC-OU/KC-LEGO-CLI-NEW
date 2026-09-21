package lego

import (
	"context"
	"fmt"

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
