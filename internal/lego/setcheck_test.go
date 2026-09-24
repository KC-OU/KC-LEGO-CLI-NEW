package lego

import (
	"context"
	"testing"
)

func TestIntakeCheckMissingExtraAndOrders(t *testing.T) {
	d := buildDB(t)
	_ = d.UpsertSet(Set{SetNum: "1", Name: "Fire Station", Qty: 1, PartsQty: 100})
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Lines) != 2 {
		t.Fatalf("lines = %+v", c.Lines)
	}
	for i := range c.Lines {
		switch c.Lines[i].PartNum {
		case "3001":
			c.Lines[i].Have = 57 // 3 missing
		case "3023":
			c.Lines[i].Extra = 2
		}
	}
	extras, err := d.FinishCheck(c)
	if err != nil {
		t.Fatal(err)
	}
	st := d.GetSetState("1-1")
	if st.MissingQty != 3 || !st.Incomplete() || st.LastCheck == nil || st.LastCheck.CheckedBy != "kc" {
		t.Fatalf("state = %+v", st)
	}
	if len(extras) != 1 || extras[0].Qty != 2 {
		t.Fatalf("extras = %+v", extras)
	}
	sp, _ := d.SparesFor("3023", 1)
	if len(sp) != 1 || sp[0].OriginSet != "1-1" || sp[0].Qty != 2 {
		t.Fatalf("spares = %+v", sp)
	}

	// Order the missing parts, receive them: the set becomes complete.
	o := &Order{SupplierKind: "bricklink", Supplier: "Brick Store", Currency: "GBP", Shipping: 3}
	if err := d.OrderFromMissing("1-1", o, func(CheckLine) float64 { return 0.1 }); err != nil {
		t.Fatal(err)
	}
	o, _ = d.GetOrder(o.ID)
	if len(o.Lines) != 1 || o.Lines[0].Qty != 3 || o.Total() < 3.29 {
		t.Fatalf("order = %+v", o)
	}
	if _, err := d.SetOrderStatus(o.ID, "ordered"); err != nil {
		t.Fatal(err)
	}
	if d.GetSetState("1-1").OnOrderQty != 3 {
		t.Error("3 on order")
	}
	done, err := d.SetOrderStatus(o.ID, "received")
	if err != nil || len(done) != 1 || done[0] != "1-1" {
		t.Fatalf("received: %v %v", done, err)
	}
	if st := d.GetSetState("1-1"); st.MissingQty != 0 || st.Incomplete() {
		t.Fatalf("after receiving: %+v", st)
	}
	spend, _ := d.Spend("set")
	if len(spend) != 1 || spend[0].Pieces != 3 || spend[0].Shipping != 3 {
		t.Fatalf("spend = %+v", spend)
	}
}

func TestTakeSpareFromAnotherSetsExtras(t *testing.T) {
	d := buildDB(t)
	// set 4-1 (newest inventory: 3001 red 25, 3023 blue 25) checked with 5 blue missing
	c, _ := d.NewCheck(context.Background(), nil, "4-1", CheckIntake, "kc")
	for i := range c.Lines {
		if c.Lines[i].PartNum == "3023" {
			c.Lines[i].Have = 20
		}
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	// set 1-1 has 4 blue extras
	c1, _ := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	for i := range c1.Lines {
		if c1.Lines[i].PartNum == "3023" {
			c1.Lines[i].Extra = 4
		}
	}
	_, _ = d.FinishCheck(c1)
	p, err := d.TakeSpare("4-1", "3023", 1, 4)
	if err != nil || p.Qty != 0 {
		t.Fatalf("take: %+v %v", p, err)
	}
	if st := d.GetSetState("4-1"); st.MissingQty != 1 {
		t.Fatalf("4-1 should be 1 short, got %d", st.MissingQty)
	}
	if sp, _ := d.SparesFor("3023", 1); len(sp) != 0 {
		t.Fatalf("no spares left: %+v", sp)
	}
	if _, err := d.TakeSpare("4-1", "3023", 1, 1); err == nil {
		t.Error("nothing left to take")
	}
}

func TestDraftResumesAndRecountStartsFromLastCheck(t *testing.T) {
	d := buildDB(t)
	c, _ := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	c.Lines[0].Have = 1
	if err := d.SaveCheck(c); err != nil {
		t.Fatal(err)
	}
	again, _ := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if again.ID != c.ID || again.Lines[0].Have != 1 {
		t.Fatalf("draft not resumed: %+v", again)
	}
	_, _ = d.FinishCheck(again)
	rc, _ := d.NewCheck(context.Background(), nil, "1-1", CheckRecount, "bob")
	if rc.ID != 0 || rc.Lines[0].Have != 1 || rc.CheckedBy != "bob" {
		t.Fatalf("recount = %+v", rc)
	}
}

func TestStickersDefaultToOptionalAndDontCountAsMissing(t *testing.T) {
	d := buildDB(t)
	for _, q := range []string{
		`INSERT INTO cat_categories (id, name) VALUES (58, 'Stickers')`,
		`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('STK1', 'Sticker Sheet', 58)`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (11, 'STK1', 0, 1)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	sticker := -1
	for i := range c.Lines {
		if c.Lines[i].PartNum == "STK1" {
			sticker = i
			c.Lines[i].Have = 0 // never counted it: still shouldn't show as missing
		}
	}
	if sticker < 0 || !c.Lines[sticker].Optional {
		t.Fatalf("a Stickers-category part should default to optional: %+v", c.Lines)
	}
	if m := c.Lines[sticker].Missing(); m != 0 {
		t.Errorf("an optional line must never report missing, got %d", m)
	}
	pieces, _, missing, _, missingLines := c.Totals()
	if pieces != 100 || missing != 0 || missingLines != 0 {
		t.Errorf("optional lines must be excluded from totals: pieces=%d missing=%d missingLines=%d", pieces, missing, missingLines)
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	if st := d.GetSetState("1-1"); st.MissingQty != 0 || st.Incomplete() {
		t.Fatalf("a set short only an optional part must read complete: %+v", st)
	}
	if lines, err := d.ShoppingList("1-1"); err != nil || len(lines) != 0 {
		t.Fatalf("the shopping list must not carry an optional line: %v %v", lines, err)
	}

	// SetOptional overrides the category default, either way.
	if err := d.SetOptional("STK1", false); err != nil {
		t.Fatal(err)
	}
	rc, err := d.NewCheck(context.Background(), nil, "1-1", CheckRecount, "kc")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range rc.Lines {
		if l.PartNum == "STK1" && l.Optional {
			t.Error("SetOptional(false) should override the Stickers default")
		}
	}
	if err := d.SetOptional("3001", true); err != nil {
		t.Fatal(err)
	}
	if optional, err := d.IsOptional("3001", ""); err != nil || !optional {
		t.Errorf("SetOptional(true) should work on any part, not just Stickers: optional=%v err=%v", optional, err)
	}
}
