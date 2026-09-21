package lego

import (
	"encoding/json"
	"strings"
	"testing"
)

func exportDB(t *testing.T) *DB {
	d := loadedCatalog(t)
	d.SetBLColors([]BLColorRow{{RBID: 4, BLID: 5, Name: "Red"}, {RBID: 1, BLID: 7, Name: "Blue"}})
	own(t, d, "3001", 4, "Red", 10)
	own(t, d, "3023", 1, "Blue", 6)
	own(t, d, "9999", -1, "Sparkly Teal", 3) // typed colour: no colour id
	_ = d.SetMinQty("3001", 4, "Red", 4)
	_ = d.UpsertSet(Set{SetNum: "75192", Name: "Millennium Falcon", Theme: "Star Wars", Year: 2017, Qty: 1, PartsQty: 7541})
	return d
}

func TestRebrickableCSVRoundTripsThroughTheImporter(t *testing.T) {
	d := exportDB(t)
	data, err := d.ExportOwned()
	if err != nil {
		t.Fatal(err)
	}
	b, warn := RebrickableCSV(data)
	if len(warn) != 1 || !strings.Contains(warn[0], "typed colour") {
		t.Errorf("the typed-colour line is left out and said so: %v", warn)
	}
	rows, problems, err := ParseRebrickableCSV(strings.NewReader(string(b)), testColors())
	if err != nil || len(problems) != 0 {
		t.Fatalf("re-import: %v %v\n%s", err, problems, b)
	}
	got := map[string]int{}
	for _, r := range rows {
		got[r.PartNum+"/"+itoa(r.ColorID)] = r.Qty
	}
	if len(got) != 2 || got["3001/4"] != 10 || got["3023/1"] != 6 {
		t.Errorf("round trip = %v", got)
	}
}

func TestBrickLinkXMLRoundTripsThroughTheImporterAndFlagsMissingColours(t *testing.T) {
	d := exportDB(t)
	data, _ := d.ExportOwned()
	b, warn, err := BrickLinkXML(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(warn) != 1 || !strings.Contains(warn[0], "1 line(s) have no BrickLink colour") {
		t.Errorf("warnings = %v", warn)
	}
	rows, _, err := ParseBrickLinkXML(strings.NewReader(string(b)), testColors())
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	got := map[string]int{}
	for _, r := range rows {
		got[r.PartNum+"/"+itoa(r.ColorID)] = r.Qty
	}
	if got["3001/4"] != 10 || got["3023/1"] != 6 || got["9999/-1"] != 3 {
		t.Errorf("round trip = %v\n%s", got, b)
	}
	if !strings.Contains(string(b), "<QTY>10</QTY>") || !strings.Contains(string(b), "<COLOR>5</COLOR>") || strings.Contains(string(b), "MINQTY") {
		t.Errorf("an inventory uses QTY:\n%s", b)
	}
}

func TestSpreadsheetCSVNeutralisesFormulas(t *testing.T) {
	data := &ExportData{Rows: []ExportRow{
		{PartNum: "=HYPERLINK(\"http://evil\")", Name: "+cmd|' /C calc'!A0", Category: "@SUM(A1)", ColorName: "-2+3", ColorID: 4, Qty: 1},
		{PartNum: "3001", Name: "Brick, 2 x 4 \"big\"", ColorID: -1, Qty: 2},
	}}
	out := string(SpreadsheetCSV(data))
	if !strings.HasPrefix(out, "\xef\xbb\xbf") {
		t.Error("Excel needs the UTF-8 byte-order mark")
	}
	for _, evil := range []string{"\n=HYPERLINK", ",+cmd", ",@SUM", ",-2+3"} {
		if strings.Contains(out, evil) {
			t.Errorf("an unescaped formula cell %q in:\n%s", evil, out)
		}
	}
	if !strings.Contains(out, `'=HYPERLINK`) || !strings.Contains(out, `"Brick, 2 x 4 ""big"""`) {
		t.Errorf("formulas are neutralised and normal text is quoted properly:\n%s", out)
	}
}

func TestJSONAndSetsCSVAndHTML(t *testing.T) {
	d := exportDB(t)
	data, _ := d.ExportOwned()
	b, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Parts []struct {
			Part     string `json:"part"`
			Quantity int    `json:"quantity"`
			BL       int    `json:"bricklink_colour"`
		} `json:"parts"`
		Sets []struct{ Set string } `json:"sets"`
	}
	if err := json.Unmarshal(b, &doc); err != nil || len(doc.Parts) != 3 || len(doc.Sets) != 1 || doc.Parts[0].Part != "3001" || doc.Parts[0].BL != 5 {
		t.Errorf("json = %v %s", err, b)
	}
	if s := string(SetsCSV(data)); !strings.Contains(s, "75192,Millennium Falcon,Star Wars,2017,1,7541,no") {
		t.Errorf("sets csv:\n%s", s)
	}
	h, err := HTML(&ExportData{Title: "<script>alert(1)</script>", Rows: []ExportRow{{PartNum: "3001", Name: `<img src=x onerror=alert(2)>`, ColorName: "Red", Qty: 4}}, Sets: data.Sets})
	if err != nil {
		t.Fatal(err)
	}
	page := string(h)
	if strings.Contains(page, "<script>alert(1)") || strings.Contains(page, "<img src=x") {
		t.Errorf("HTML output must escape everything:\n%s", page)
	}
	for _, want := range []string{"&lt;script&gt;", "Millennium Falcon", "1 part line(s), 4 piece(s)", "@media print"} {
		if !strings.Contains(page, want) {
			t.Errorf("html lacks %q", want)
		}
	}
}

func TestExportMissingUsesTheShortfall(t *testing.T) {
	d := exportDB(t)
	items := []InvItem{{PartNum: "3001", PartName: "Brick 2 x 4", ColorID: 4, ColorName: "Red", Qty: 25}}
	r, _ := d.MissingForWith(items, 1, EquivNone)
	data := d.ExportMissing("75192", r)
	if len(data.Rows) != 1 || data.Rows[0].Qty != 15 || data.Rows[0].BLColor != 5 || !strings.Contains(data.Title, "75192") {
		t.Fatalf("missing export = %+v", data)
	}
	b, _ := RebrickableCSV(data)
	if !strings.Contains(string(b), "3001,4,15") {
		t.Errorf("csv:\n%s", b)
	}
}

func TestEmptyCollectionExportsCleanly(t *testing.T) {
	data, err := openScratchDB(t).ExportOwned()
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := RebrickableCSV(data); string(b) != "Part,Color,Quantity\n" {
		t.Errorf("empty csv = %q", b)
	}
	if b, _, err := BrickLinkXML(data); err != nil || !strings.Contains(string(b), "<INVENTORY>") {
		t.Errorf("empty xml = %q %v", b, err)
	}
	if b, err := JSON(data); err != nil || !strings.Contains(string(b), `"parts": []`) {
		t.Errorf("empty json = %s %v", b, err)
	}
	if _, err := HTML(data); err != nil {
		t.Error(err)
	}
}
