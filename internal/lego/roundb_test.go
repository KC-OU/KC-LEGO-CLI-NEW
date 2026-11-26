package lego

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"
	"time"
)

type fakeFeed struct {
	orders []SalesOrder
	lines  map[string][]SalesLine
	calls  int
}

func (f *fakeFeed) ListOrders(context.Context) ([]SalesOrder, error) { return f.orders, nil }
func (f *fakeFeed) OrderLines(_ context.Context, id string) ([]SalesLine, error) {
	f.calls++
	return f.lines[id], nil
}

func TestSyncSalesOrdersImportsOnceKeepsPickedAndClosesVanishedOrders(t *testing.T) {
	db := loadedCatalog(t)
	feed := &fakeFeed{
		orders: []SalesOrder{{OrderID: "1", Buyer: "ann", Status: "PAID", Total: 5, Currency: "GBP"}, {OrderID: "2", Buyer: "bob", Status: "PAID"}},
		lines:  map[string][]SalesLine{"1": {{ItemNo: "3001", BLColor: 5, Qty: 4, Cond: "U"}, {ItemNo: "3023", Qty: 2, Cond: "N"}}, "2": {{ItemNo: "3001", Qty: 1}}},
	}
	if n, err := db.SyncSalesOrders(context.Background(), feed); err != nil || n != 2 || feed.calls != 2 {
		t.Fatalf("first sync: added=%d calls=%d err=%v", n, feed.calls, err)
	}
	if err := db.SetSalesLinePicked("1", 1, 99); err != nil { // clamps to qty 4
		t.Fatal(err)
	}
	feed.orders = feed.orders[:1] // order 2 is no longer open on BrickLink
	feed.orders[0].Status = "PACKED"
	if n, err := db.SyncSalesOrders(context.Background(), feed); err != nil || n != 0 || feed.calls != 2 {
		t.Fatalf("second sync must not refetch lines: added=%d calls=%d err=%v", n, feed.calls, err)
	}
	o, _ := db.GetSalesOrder("1")
	if o.Status != "PACKED" || o.Pieces != 6 || o.Lines[0].Picked != 4 || o.PickedAt != "" {
		t.Fatalf("order 1 = %+v", o)
	}
	if o2, _ := db.GetSalesOrder("2"); o2.Status != "closed" {
		t.Fatalf("vanished order should be closed, got %q", o2.Status)
	}
	if open, _ := db.ListSalesOrders(true); len(open) != 1 {
		t.Fatalf("open orders = %d, want 1", len(open))
	}
	_ = db.SetSalesLinePicked("1", 2, 2)
	if o, _ := db.GetSalesOrder("1"); o.PickedAt == "" {
		t.Error("picked_at should be stamped once every line is picked")
	}
}

func TestPriceMoversNeedBothPercentAndValue(t *testing.T) {
	db := loadedCatalog(t)
	if err := db.AddOwnedPart(OwnedPart{PartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 10}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	for _, r := range []struct {
		day string
		avg float64
	}{{old, 1.00}, {today(), 1.50}} {
		db.Exec(`INSERT OR REPLACE INTO price_history VALUES ('PART','3001',0,'U',?,?)`, r.day, r.avg)
	}
	got, err := db.PriceMovers("U", 7, 20, 1)
	if err != nil || len(got) != 1 || got[0].Pct != 50 || got[0].Value != 5 {
		t.Fatalf("movers = %+v err=%v", got, err)
	}
	if got, _ := db.PriceMovers("U", 7, 20, 100); len(got) != 0 {
		t.Errorf("a 5.00 change must stay quiet under a 100 value floor: %+v", got)
	}
}

func TestRepriceSuggestionsOnlyFlagLotsOffTheMarket(t *testing.T) {
	db := loadedCatalog(t)
	db.putPrice(PriceRow{ItemType: "PART", ItemNo: "3001", BLColor: 5, Cond: "U", Avg: 1.00, Currency: "GBP"})
	db.putPrice(PriceRow{ItemType: "PART", ItemNo: "3023", BLColor: 5, Cond: "U", Avg: 1.00, Currency: "GBP"})
	if err := db.ReplaceStoreLots([]StoreLotIn{
		{InventoryID: 1, ItemType: "PART", ItemNo: "3001", BLColor: 5, Qty: 3, Cond: "U", UnitPrice: 0.50}, // underpriced
		{InventoryID: 2, ItemType: "PART", ItemNo: "3023", BLColor: 5, Qty: 3, Cond: "U", UnitPrice: 1.02}, // within tolerance
	}); err != nil {
		t.Fatal(err)
	}
	got, err := db.RepriceSuggestions(100, 10)
	if err != nil || len(got) != 1 || got[0].InventoryID != 1 || got[0].Suggested != 1.0 || got[0].DiffPct != -50 {
		t.Fatalf("suggestions = %+v err=%v", got, err)
	}
}

func TestAcquisitionWalksEveryStageThenStops(t *testing.T) {
	db := loadedCatalog(t)
	id, err := db.AddAcquisition(Acquisition{Label: "Job lot", Cost: 40, Currency: "GBP"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range AcquisitionStages[1:] {
		if got, err := db.AdvanceAcquisition(id); err != nil || got != want {
			t.Fatalf("advance = %q %v, want %q", got, err, want)
		}
	}
	if _, err := db.AdvanceAcquisition(id); err == nil {
		t.Error("advancing past listed must fail")
	}
	if _, err := db.AddAcquisition(Acquisition{}); err == nil {
		t.Error("an acquisition needs a label")
	}
}

func tinyXLSX(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	put := func(name, body string) {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	put("xl/sharedStrings.xml", `<sst><si><t>Set #</t></si><si><t>Set Name</t></si><si><t>Retirement Date</t></si><si><t>Theme</t></si><si><t>Falcon</t></si><si><t>Star Wars</t></si></sst>`)
	// 46387 = 2026-12-31
	put("xl/worksheets/sheet1.xml", `<worksheet><sheetData>
		<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c></row>
		<row r="2"><c r="A2" t="inlineStr"><is><t>75192</t></is></c><c r="B2" t="s"><v>4</v></c><c r="C2"><v>46387</v></c><c r="D2" t="s"><v>5</v></c></row>
	</sheetData></worksheet>`)
	zw.Close()
	return buf.Bytes()
}

func TestImportRetirementFileReadsExcelAndConvertsSerialDates(t *testing.T) {
	db := loadedCatalog(t)
	n, err := db.ImportRetirementFile("list.xlsx", tinyXLSX(t))
	if err != nil || n != 1 {
		t.Fatalf("imported %d err=%v", n, err)
	}
	r, _ := db.RetirementFor("75192")
	if r == nil || r.RetiresAt.Format("2006-01-02") != "2026-12-31" || r.Theme != "Star Wars" {
		t.Fatalf("retirement = %+v", r)
	}
	if _, err := db.ImportRetirementFile("bad.xlsx", []byte("not a zip")); err == nil {
		t.Error("garbage must be refused")
	}
}

func TestSaveSetRetailReportsFirstAndMovedExitDatesOnly(t *testing.T) {
	db := loadedCatalog(t)
	row := SetRetail{SetNum: "75192-1", Name: "Falcon"}
	if ch, _ := db.SaveSetRetail([]SetRetail{row}); len(ch) != 0 {
		t.Fatalf("a first sighting with no exit date is silent: %+v", ch)
	}
	row.ExitDate = "2026-12-31"
	if ch, _ := db.SaveSetRetail([]SetRetail{row}); len(ch) != 1 || ch[0].What != "now has an exit date: 2026-12-31" {
		t.Fatalf("first exit date: %+v", ch)
	}
	if ch, _ := db.SaveSetRetail([]SetRetail{row}); len(ch) != 0 {
		t.Fatalf("unchanged must be silent: %+v", ch)
	}
	row.ExitDate = "2027-03-31"
	if ch, _ := db.SaveSetRetail([]SetRetail{row}); len(ch) != 1 {
		t.Fatalf("moved date: %+v", ch)
	}
	view, err := db.RetiredView(2000 * 24 * time.Hour)
	if err != nil || len(view) != 1 || view[0].ExitDate != "2027-03-31" {
		t.Fatalf("view = %+v err=%v", view, err)
	}
}

func TestCycleCountRecordsVarianceCorrectsStockAndDropsOutOfTheDueList(t *testing.T) {
	db := loadedCatalog(t)
	db.AddOwnedPart(OwnedPart{PartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 10})
	db.AddOwnedPart(OwnedPart{PartNum: "3023", ColorID: 4, ColorName: "Red", Qty: 50})
	due, err := db.CountsDue(nil, 10, 30)
	if err != nil || len(due) != 2 || due[0].PartNum != "3023" { // equal "never counted": bigger lot first
		t.Fatalf("due = %+v err=%v", due, err)
	}
	v, err := db.RecordCount("3023", 4, "Red", 47, "alice")
	if err != nil || v != -3 {
		t.Fatalf("variance = %d err=%v", v, err)
	}
	if got, _ := db.GetOwnedPart("3023", 4, "Red"); got.Qty != 47 {
		t.Errorf("stock should be corrected to the count, got %d", got.Qty)
	}
	if due, _ := db.CountsDue(nil, 10, 30); len(due) != 1 || due[0].PartNum != "3001" {
		t.Errorf("a freshly counted lot must leave the due list: %+v", due)
	}
	if _, err := db.RecordCount("3001", 4, "Red", -1, "alice"); err == nil {
		t.Error("negative counts must be refused")
	}
	if _, err := db.RecordCount("nope", 4, "Red", 1, "alice"); err == nil {
		t.Error("counting a part you don't hold must be refused")
	}
}

func TestDataQualityFlagsFreeTextColoursThatAreRealColours(t *testing.T) {
	db := loadedCatalog(t)
	db.AddOwnedPart(OwnedPart{PartNum: "3001", ColorID: -1, ColorName: "red", Qty: 5})
	issues, err := db.DataQuality(nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, i := range issues {
		if i.Kind == IssueFreeColour && i.Part == "3001" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a free-text colour issue, got %+v", issues)
	}
}

func TestBinMapCountsLotsPerLocationAndScalesFillToTheBusiest(t *testing.T) {
	s, pdb, _ := newSyncer(t)
	_ = s.Lego.AddOwnedPart(OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 10})
	_ = s.Lego.AddOwnedPart(OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 1, ColorName: "Blue", Qty: 5})
	if _, err := s.SyncNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := pdb.Exec(`INSERT INTO storelocations (id, name, is_full, only_single_part, limit_to_existing_parts, comment, not_selectable) VALUES (2, 'Shelf B', 0, 0, 0, '', 0)`); err != nil {
		t.Skipf("scratch Part-DB is read-only here: %v", err)
	}
	bins, err := BinMap(pdb)
	if err != nil || len(bins) != 2 {
		t.Fatalf("bins = %+v err=%v", bins, err)
	}
	if bins[0].Name != "Default" || bins[0].Lots != 2 || bins[0].Parts != 2 || bins[0].Pieces != 15 || bins[0].Fill != 1 {
		t.Errorf("busiest bin = %+v", bins[0])
	}
	if bins[1].Name != "Shelf B" || bins[1].Lots != 0 || bins[1].Fill != 0 {
		t.Errorf("empty bin = %+v", bins[1])
	}
}

func TestNLFilterIsCleanedAndSearchBindsItAsData(t *testing.T) {
	db := loadedCatalog(t)
	db.AddOwnedPart(OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", ColorID: 4, ColorName: "Red", Qty: 40})
	db.AddOwnedPart(OwnedPart{PartNum: "3023", Name: "Plate 1 x 2", ColorID: 4, ColorName: "Red", Qty: 9})
	db.putPrice(PriceRow{ItemType: "PART", ItemNo: "3001", BLColor: 0, Cond: "U", Avg: 0.08, Currency: "GBP"})
	f := NLFilter{Words: []string{"brick", "2x4", "x'; DROP TABLE owned_parts;--", "%"}, Color: "red", MaxPrice: 0.10}.Clean()
	if len(f.Words) != 2 || f.Words[1] != "2 x 4" {
		t.Fatalf("hostile words must be dropped and 2x4 normalised: %+v", f.Words)
	}
	hits, err := db.SearchOwnedNL(f, 10)
	if err != nil || len(hits) != 1 || hits[0].PartNum != "3001" || hits[0].Price != 0.08 {
		t.Fatalf("hits = %+v err=%v", hits, err)
	}
	if hits, _ := db.SearchOwnedNL(NLFilter{Words: []string{"plate"}, MaxPrice: 1}, 10); len(hits) != 0 {
		t.Errorf("an unpriced lot must not pass a price cap: %+v", hits)
	}
	if hits, _ := db.SearchOwnedNL(NLFilter{Words: []string{"plate"}}, 10); len(hits) != 1 {
		t.Errorf("without a cap the unpriced plate should match: %+v", hits)
	}
}

func TestBorrowedPartWalksToReplacedThenStops(t *testing.T) {
	db := openScratchDB(t)
	id, err := db.AddBorrowedPart(BorrowedPart{BuildSetNum: "75192-1", NeedPartNum: "3001", NeedElementID: "300126", FromSetNum: "10696-1", FromPartNum: "3001"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range BorrowedStages[1:] {
		if got, err := db.AdvanceBorrowedPart(id); err != nil || got != want {
			t.Fatalf("advance = %q %v, want %q", got, err, want)
		}
	}
	if _, err := db.AdvanceBorrowedPart(id); err == nil {
		t.Error("advancing past replaced must fail")
	}
	if _, err := db.AddBorrowedPart(BorrowedPart{BuildSetNum: "75192-1"}); err == nil {
		t.Error("a note needs the part that is needed")
	}
	list, err := db.ListBorrowedParts()
	if err != nil || len(list) != 1 || list[0].Qty != 1 || list[0].FromSetNum != "10696-1" {
		t.Fatalf("list = %+v err=%v", list, err)
	}
}
