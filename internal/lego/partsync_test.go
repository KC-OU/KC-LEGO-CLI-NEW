package lego

import (
	"context"
	"errors"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb/partdbtest"
)

const syncToken = "tcp_sync_test"

// newSyncer wires a LEGO db to a real-schema scratch Part-DB behind a fake REST API.
func newSyncer(t *testing.T) (*PartSyncer, *partdb.DB, *partdbtest.Fake) {
	t.Helper()
	path := partdbtest.New(t)
	partdbtest.Guard(t, path)
	pdb, err := partdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pdb.Close() })
	fake := partdbtest.NewFake(t, path, syncToken)
	w := &partdb.Writer{DB: pdb, API: func() *partdb.API {
		return &partdb.API{BaseURL: fake.URL(), Token: syncToken, HTTP: fake.Server.Client(), Comment: "wms-go"}
	}}
	return &PartSyncer{Lego: newFixture(t), Writer: w}, pdb, fake
}

func TestPartSyncCreatesOnePartPerColourThenUpdates(t *testing.T) {
	s, pdb, _ := newSyncer(t)
	ctx := context.Background()
	_ = s.Lego.AddOwnedPart(OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 10})
	_ = s.Lego.AddOwnedPart(OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 1, ColorName: "Blue", Qty: 5})

	res, err := s.SyncNow(ctx)
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	if res.Created != 2 || res.Updated != 0 || len(res.Errors) != 0 {
		t.Fatalf("first sync: %+v", res)
	}

	red, _ := pdb.FindPartByIPN("3001-4")
	blue, _ := pdb.FindPartByIPN("3001-1")
	if red == 0 || blue == 0 || red == blue {
		t.Fatalf("expected two Part-DB parts (3001-4, 3001-1), got %d and %d", red, blue)
	}
	detail, _ := pdb.GetPart(red)
	if detail.Name != "Brick 2 x 4 - Red" || detail.TotalStock != 10 || detail.Category != "Bricks" {
		t.Errorf("red part = %+v", detail)
	}
	owned, _ := s.Lego.GetOwnedPart("3001", 4, "")
	if owned.SyncedPartID != red {
		t.Errorf("the LEGO row should record Part-DB id %d, got %d", red, owned.SyncedPartID)
	}

	if res, _ = s.SyncNow(ctx); res.Created != 0 || res.Updated != 0 || res.Unchanged != 2 {
		t.Fatalf("an unchanged re-sync should do nothing, got %+v", res)
	}
	_ = s.Lego.AddOwnedPart(OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 40})
	if res, _ = s.SyncNow(ctx); res.Updated != 1 || res.Unchanged != 1 {
		t.Fatalf("one changed quantity should be one update, got %+v", res)
	}
	if d, _ := pdb.GetPart(red); d.TotalStock != 40 {
		t.Errorf("stock after update = %v, want 40", d.TotalStock)
	}
}

func TestPartSyncStopsCleanlyWithoutAToken(t *testing.T) {
	s, _, fake := newSyncer(t)
	_ = s.Lego.AddOwnedPart(OwnedPart{PartNum: "3001", Name: "x", ColorID: 4, ColorName: "Red", Qty: 1})
	s.Writer.API = func() *partdb.API { return &partdb.API{BaseURL: fake.URL(), HTTP: fake.Server.Client()} }
	if _, err := s.SyncNow(context.Background()); !errors.Is(err, partdb.ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
	if len(fake.Requests()) != 0 {
		t.Errorf("no request should be made without a token: %v", fake.Requests())
	}
}
