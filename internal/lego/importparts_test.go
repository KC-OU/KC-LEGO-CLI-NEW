package lego

import (
	"strings"
	"testing"
)

func testColors() *ColorTable {
	t := NewColorTable([]Color{{ID: 4, Name: "Red"}, {ID: 1, Name: "Blue"}, {ID: 0, Name: "Black"}})
	t.AddBrickLink([]ColorInfo{
		{ID: 4, Name: "Red", ExternalIDs: map[string]map[string]any{"BrickLink": {"ext_ids": []any{float64(5)}}}},
		{ID: 1, Name: "Blue", ExternalIDs: map[string]map[string]any{"BrickLink": {"ext_ids": []any{float64(7)}}}},
	})
	return t
}

func TestRebrickableCSVParsing(t *testing.T) {
	csv := "\ufeffPart,Color,Quantity,Is Spare\n" +
		"3001,4,10,False\n" +
		"3001,4,2,True\n" + // a spare: skipped
		"3023,Blue,6,False\n" + // colour by name
		"3024,Sparkly,3,\n" + // unknown name: kept as text
		"3062b,9999,1,\n" + // unknown id: kept, with a problem
		"3010,4,x,\n" + // bad quantity
		",4,1,\n" // no part
	rows, problems, err := ParseRebrickableCSV(strings.NewReader(csv), testColors())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %+v", rows)
	}
	if r := rows[0]; r.PartNum != "3001" || r.ColorID != 4 || r.ColorName != "Red" || r.Qty != 10 {
		t.Errorf("row 0 = %+v", r)
	}
	if r := rows[1]; r.ColorID != 1 || r.ColorName != "Blue" {
		t.Errorf("a colour name resolves to its id: %+v", r)
	}
	if r := rows[2]; r.ColorID != NoColor || r.ColorName != "Sparkly" {
		t.Errorf("an unknown colour name stays as text: %+v", r)
	}
	if len(problems) != 3 {
		t.Errorf("problems = %v", problems)
	}
}

func TestRebrickableCSVRejectsOtherFiles(t *testing.T) {
	if _, _, err := ParseRebrickableCSV(strings.NewReader("name,price\nx,1\n"), testColors()); err == nil {
		t.Error("a CSV without Part and Quantity columns must be rejected")
	}
	if _, _, err := ParseRebrickableCSV(strings.NewReader(""), testColors()); err == nil {
		t.Error("an empty file must be rejected")
	}
}

func TestBrickLinkXMLParsing(t *testing.T) {
	x := `<INVENTORY>
	 <ITEM><ITEMTYPE>P</ITEMTYPE><ITEMID>3001</ITEMID><COLOR>5</COLOR><QTY>10</QTY></ITEM>
	 <ITEM><ITEMTYPE>P</ITEMTYPE><ITEMID>3023</ITEMID><COLOR>7</COLOR><MINQTY>6</MINQTY></ITEM>
	 <ITEM><ITEMTYPE>S</ITEMTYPE><ITEMID>75192-1</ITEMID><QTY>1</QTY></ITEM>
	 <ITEM><ITEMTYPE>P</ITEMTYPE><ITEMID>3024</ITEMID><COLOR>999</COLOR><QTY>3</QTY></ITEM>
	 <ITEM><ITEMTYPE>P</ITEMTYPE><ITEMID>3010</ITEMID><QTY>1</QTY></ITEM>
	 <ITEM><ITEMTYPE>P</ITEMTYPE><ITEMID></ITEMID><QTY>1</QTY></ITEM>
	</INVENTORY>`
	rows, problems, err := ParseBrickLinkXML(strings.NewReader(x), testColors())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %+v (problems %v)", rows, problems)
	}
	if rows[0].ColorID != 4 || rows[0].ColorName != "Red" || rows[0].Qty != 10 {
		t.Errorf("BrickLink colour 5 is Rebrickable Red: %+v", rows[0])
	}
	if rows[1].ColorID != 1 || rows[1].Qty != 6 {
		t.Errorf("MINQTY is read when QTY is absent: %+v", rows[1])
	}
	if rows[2].ColorID != NoColor || !strings.Contains(rows[2].ColorName, "999") {
		t.Errorf("an unmapped colour is kept as text: %+v", rows[2])
	}
	if rows[3].ColorID != NoColor || rows[3].ColorName != "" {
		t.Errorf("no colour means no colour: %+v", rows[3])
	}
	if len(problems) != 3 { // the set, the unmapped colour, the blank id
		t.Errorf("problems = %v", problems)
	}
	for _, bad := range []string{"not xml", "<INVENTORY></INVENTORY>"} {
		if _, _, err := ParseBrickLinkXML(strings.NewReader(bad), testColors()); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func catalogWith(t *testing.T) *DB { return loadedCatalog(t) }

func TestPlanImportAddVersusSetAndUnknownParts(t *testing.T) {
	d := catalogWith(t)
	own(t, d, "3001", 4, "Red", 5)
	rows := []ImportRow{
		{Line: 1, PartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 10},
		{Line: 2, PartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 2}, // same part+colour: merged
		{Line: 3, PartNum: "3001", ColorID: 1, ColorName: "Blue", Qty: 1},
		{Line: 4, PartNum: "zz-not-a-part", ColorID: NoColor, Qty: 3},
	}
	add, err := d.PlanImport(rows, "add")
	if err != nil {
		t.Fatal(err)
	}
	if len(add.Items) != 3 || add.New != 2 || add.Changed != 1 || add.Unchanged != 0 || add.Unknown != 1 {
		t.Fatalf("add plan = %+v", add)
	}
	if it := add.Items[0]; it.Was != 5 || it.Becomes != 17 || it.Name != "Brick 2 x 4" || it.Category != "Bricks" {
		t.Errorf("add: %+v", it)
	}
	set, _ := d.PlanImport(rows, "set")
	if it := set.Items[0]; it.Becomes != 12 {
		t.Errorf("set replaces the quantity: %+v", it)
	}
	if _, err := d.PlanImport(rows, "merge"); err == nil {
		t.Error("an unknown mode must be refused")
	}
	if got, _ := d.GetOwnedPart("3001", 4, "Red"); got.Qty != 5 {
		t.Error("planning must not change anything")
	}
	same, _ := d.PlanImport([]ImportRow{{PartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 5}}, "set")
	if same.Unchanged != 1 || same.Changed != 0 {
		t.Errorf("setting the same quantity is unchanged: %+v", same)
	}
}

func TestApplyImportWritesEverythingAndKeepsMinimums(t *testing.T) {
	d := catalogWith(t)
	own(t, d, "3001", 4, "Red", 5)
	_ = d.SetMinQty("3001", 4, "Red", 4)
	plan, _ := d.PlanImport([]ImportRow{
		{PartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 10},
		{PartNum: "3023", ColorID: 1, ColorName: "Blue", Qty: 6},
	}, "add")
	if err := d.ApplyImport(plan); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.GetOwnedPart("3001", 4, "Red"); got.Qty != 15 || got.MinQty != 4 {
		t.Errorf("3001 = %+v", got)
	}
	if got, _ := d.GetOwnedPart("3023", 1, "Blue"); got == nil || got.Qty != 6 || got.Name != "Plate 1 x 2" || got.SyncedPartID != 0 {
		t.Errorf("3023 = %+v", got)
	}
}

func TestApplyImportIsAllOrNothing(t *testing.T) {
	d := catalogWith(t)
	plan := &ImportPlan{Items: []PlanItem{
		{ImportRow: ImportRow{PartNum: "3001", ColorID: 4, ColorName: "Red"}, Name: "ok", Becomes: 1},
		{ImportRow: ImportRow{PartNum: "3023", ColorID: 1, ColorName: "Blue"}, Name: "bad", Becomes: -1 << 62},
	}}
	if _, err := d.Exec(`CREATE TRIGGER refuse BEFORE INSERT ON owned_parts WHEN NEW.part_num = '3023' BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatal(err)
	}
	if err := d.ApplyImport(plan); err == nil {
		t.Fatal("the second line was refused, so the import must fail")
	}
	if got, _ := d.GetOwnedPart("3001", 4, "Red"); got != nil {
		t.Errorf("the first line must have been rolled back: %+v", got)
	}
}

func TestImportRefusesJunkPartNumbersAndFormulas(t *testing.T) {
	for _, ok := range []string{"3001", "970c00pr0001", "3626cpr0001", "x123", "u9001", "32270-old", "sw0001a", "fig_1.2"} {
		if err := CheckPartNum(ok); err != nil {
			t.Errorf("%q is a fine part number: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "3023,x", "=1+1", "3001 ", "a b", "'; DROP TABLE x;--", strings.Repeat("9", 41), "-3001", "3001\n"} {
		if CheckPartNum(bad) == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}
