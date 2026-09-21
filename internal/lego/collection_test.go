package lego

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func own(t *testing.T, d *DB, num string, color int, colorName string, qty int) {
	t.Helper()
	if err := d.AddOwnedPart(OwnedPart{PartNum: num, Name: "Part " + num, Category: "Bricks", ColorID: color, ColorName: colorName, Qty: qty}); err != nil {
		t.Fatal(err)
	}
}

func TestLowStockNeedsAMinimumAndFallingBelowIt(t *testing.T) {
	d := openScratchDB(t)
	own(t, d, "3001", 4, "Red", 3)
	own(t, d, "3002", 4, "Red", 10)
	own(t, d, "3003", 1, "Blue", 0)
	if low, _ := d.LowStock(); len(low) != 0 {
		t.Fatalf("with no minimums nothing is low: %v", low)
	}
	if err := d.SetMinQty("3001", 4, "Red", 5); err != nil {
		t.Fatal(err)
	}
	_ = d.SetMinQty("3002", 4, "Red", 10) // exactly at the minimum is not low
	_ = d.SetMinQty("3003", 1, "Blue", 2)
	low, err := d.LowStock()
	if err != nil || len(low) != 2 {
		t.Fatalf("low = %v %v", low, err)
	}
	if low[0].PartNum != "3001" || low[1].PartNum != "3003" { // both are 2 short: ties go by part number
		t.Errorf("order = %s, %s", low[0].PartNum, low[1].PartNum)
	}
	if !low[1].IsLow() {
		t.Error("IsLow")
	}
	if err := d.SetMinQty("3001", 4, "Red", -1); err == nil {
		t.Error("a negative minimum must be refused")
	}
	if err := d.SetMinQty("9999", 4, "Red", 1); err == nil {
		t.Error("setting a minimum for a part you do not hold must fail")
	}
	own(t, d, "3001", 4, "Red", 8) // restocking clears it, and re-adding keeps the minimum
	if low, _ := d.LowStock(); len(low) != 1 || low[0].PartNum != "3003" {
		t.Errorf("after restock: %v", low)
	}
	if got, _ := d.GetOwnedPart("3001", 4, "Red"); got.MinQty != 5 {
		t.Errorf("AddOwnedPart must keep the minimum, got %d", got.MinQty)
	}
}

func TestStats(t *testing.T) {
	d := openScratchDB(t)
	_ = d.UpsertSet(Set{SetNum: "75192", Name: "Falcon", Theme: "Star Wars", Qty: 1, PartsQty: 7541})
	_ = d.UpsertSet(Set{SetNum: "75257", Name: "Falcon 2", Theme: "Star Wars", Qty: 2, PartsQty: 1000})
	_ = d.UpsertSet(Set{SetNum: "10497", Name: "Galileo", Theme: "Ideas", Qty: 1, PartsQty: 500})
	_ = d.MarkPartedOut("10497", true)
	own(t, d, "3001", 4, "Red", 30)
	own(t, d, "3001", 1, "Blue", 20)
	own(t, d, "3023", 4, "Red", 5)
	_ = d.SetMinQty("3023", 4, "Red", 9)
	s, err := d.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if s.SetTitles != 3 || s.SetCopies != 4 || s.SetPieces != 7541+2000 || s.PartedOut != 1 {
		t.Errorf("sets: %+v", s)
	}
	if s.PartLines != 3 || s.DistinctParts != 2 || s.LoosePieces != 55 || s.LowStock != 1 {
		t.Errorf("parts: %+v", s)
	}
	if s.Themes[0] != (Count{"Star Wars", 3}) || s.Colours[0] != (Count{"Red", 35}) || s.Categories[0] != (Count{"Bricks", 55}) {
		t.Errorf("breakdowns: %+v %+v %+v", s.Themes, s.Colours, s.Categories)
	}
	empty, err := openScratchDB(t).Stats()
	if err != nil || empty.SetCopies != 0 || empty.LoosePieces != 0 || len(empty.Themes) != 0 {
		t.Errorf("an empty collection must give zeros: %+v %v", empty, err)
	}
}

// inventoryServer serves a two-page inventory for set 75192-1.
func inventoryServer(t *testing.T) (*Client, *int) {
	t.Helper()
	hits := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/sets/75192-1/parts/" {
			w.WriteHeader(404)
			return
		}
		row := func(part, name string, color int, cname string, bl int, blPart string, qty int, spare bool) string {
			return fmt.Sprintf(`{"part":{"part_num":%q,"name":%q,"external_ids":{"BrickLink":[%q]}},"color":{"id":%d,"name":%q,"rgb":"C91A09","external_ids":{"BrickLink":{"ext_ids":[%d]}}},"quantity":%d,"is_spare":%v}`,
				part, name, blPart, color, cname, bl, qty, spare)
		}
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprintf(w, `{"next":null,"results":[%s]}`, row("3023", "Plate 1 x 2", 1, "Blue", 7, "3023", 6, false))
			return
		}
		fmt.Fprintf(w, `{"next":%q,"results":[%s,%s,%s]}`, srv.URL+"/sets/75192-1/parts/?page=2&page_size=1000",
			row("3001", "Brick 2 x 4", 4, "Red", 5, "3001", 10, false),
			row("3957a", "Antenna", 0, "Black", 11, "3957a", 2, false),
			row("3001", "Brick 2 x 4", 4, "Red", 5, "3001", 4, true)) // a spare: ignored
	}))
	t.Cleanup(srv.Close)
	return &Client{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}, &hits
}

func TestSetInventoryFollowsPagesSkipsSparesAndReadsBrickLinkIDs(t *testing.T) {
	c, _ := inventoryServer(t)
	items, err := c.GetSetInventory(context.Background(), "75192")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].PartNum != "3001" || items[0].Qty != 10 || items[0].BrickLinkID != "3001" || items[0].BLColor != 5 || items[0].RGB != "C91A09" {
		t.Errorf("first item = %+v", items[0])
	}
	if items[2].PartNum != "3023" || items[2].BLColor != 7 {
		t.Errorf("page 2 item = %+v", items[2])
	}
}

func TestSetInventoryIsCachedPerPage(t *testing.T) {
	c, hits := inventoryServer(t)
	c.Store = openScratchDB(t)
	for i := 0; i < 3; i++ {
		if _, err := c.GetSetInventory(context.Background(), "75192-1"); err != nil {
			t.Fatal(err)
		}
	}
	if *hits != 2 { // two pages, fetched once
		t.Errorf("server hit %d times, want 2", *hits)
	}
}

func TestSetInventoryUnknownSetAndForeignNextLink(t *testing.T) {
	c, _ := inventoryServer(t)
	if _, err := c.GetSetInventory(context.Background(), "99999"); err != ErrNotFound {
		t.Errorf("unknown set = %v", err)
	}
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"next":"https://evil.example/steal?key=1","results":[]}`)
	}))
	defer evil.Close()
	if _, err := (&Client{APIKey: "k", BaseURL: evil.URL, HTTP: evil.Client()}).GetSetInventory(context.Background(), "1-1"); err == nil {
		t.Error("a next link off the API host must not be followed (the API key would go with it)")
	}
}

func TestMissingPartsMatchOnPartAndColourID(t *testing.T) {
	d := openScratchDB(t)
	own(t, d, "3001", 4, "Red", 6)          // need 10: short 4
	own(t, d, "3001", 1, "Blue", 100)       // wrong colour: irrelevant
	own(t, d, "3023", 1, "Blue", 6)         // need 6: complete
	own(t, d, "3957a", -1, "black-ish", 50) // free-text colour cannot be matched: still missing
	c, _ := inventoryServer(t)
	items, _ := c.GetSetInventory(context.Background(), "75192")
	r, err := d.MissingFor(items, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.Lines != 3 || r.Complete != 1 || len(r.Missing) != 2 || r.PiecesNeeded != 18 || r.PiecesHeld != 12 || r.Percent() != 66 {
		t.Fatalf("report = %+v", r)
	}
	if m := r.Missing[0]; m.PartNum != "3001" || m.Short != 4 || m.Have != 6 || m.Need != 10 {
		t.Errorf("worst first: %+v", m)
	}
	if m := r.Missing[1]; m.PartNum != "3957a" || m.Short != 2 {
		t.Errorf("free-text colour is not matched: %+v", m)
	}
	two, _ := d.MissingFor(items, 2) // two copies need twice as much
	if two.PiecesNeeded != 36 || two.Missing[0].Short != 14 {
		t.Errorf("two copies: %+v", two)
	}
	all, _ := d.MissingFor(nil, 1)
	if all.Percent() != 100 || len(all.Missing) != 0 {
		t.Errorf("an empty set is complete: %+v", all)
	}
}

func TestWantedXMLIsBrickLinksUploadFormat(t *testing.T) {
	c, _ := inventoryServer(t)
	items, _ := c.GetSetInventory(context.Background(), "75192")
	r, _ := openScratchDB(t).MissingFor(items, 1)
	wanted, noPart, noColor := WantedFromMissing(r)
	if noPart != 0 || noColor != 0 {
		t.Errorf("fallbacks = %d %d", noPart, noColor)
	}
	b, err := WantedXML(wanted)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	for _, want := range []string{"<INVENTORY>", "<ITEMTYPE>P</ITEMTYPE>", "<ITEMID>3001</ITEMID>", "<COLOR>5</COLOR>", "<MINQTY>10</MINQTY>"} {
		if !strings.Contains(out, want) {
			t.Errorf("XML missing %s:\n%s", want, out)
		}
	}
	var back struct {
		Items []struct {
			ItemID string `xml:"ITEMID"`
		} `xml:"ITEM"`
	}
	if err := xml.Unmarshal(b, &back); err != nil || len(back.Items) != 3 {
		t.Errorf("must parse back as XML with 3 items: %v %+v", err, back)
	}
}

func TestWantedXMLFallbacksAndEscaping(t *testing.T) {
	r := &MissingReport{Missing: []MissingLine{
		{InvItem: InvItem{PartNum: "3001", BrickLinkID: "", BLColor: 0}, Short: 2},
		{InvItem: InvItem{PartNum: "a<b&c", BrickLinkID: "a<b&c", BLColor: 5}, Short: 1},
	}}
	items, noPart, noColor := WantedFromMissing(r)
	if noPart != 1 || noColor != 1 || items[0].ItemID != "3001" {
		t.Fatalf("items=%+v noPart=%d noColor=%d", items, noPart, noColor)
	}
	b, _ := WantedXML(items)
	if strings.Contains(string(b), "<COLOR>0") || !strings.Contains(string(b), "a&lt;b&amp;c") {
		t.Errorf("an unknown colour is left out and text is escaped:\n%s", b)
	}
	b, _ = WantedXML([]WantedItem{{ItemID: "", MinQty: 1}, {ItemID: "x", MinQty: 0}})
	if strings.Contains(string(b), "<ITEM>") {
		t.Errorf("empty ids and zero quantities are skipped:\n%s", b)
	}
}

func TestRecentsAreMostRecentFirstUniqueAndBounded(t *testing.T) {
	d := openScratchDB(t)
	for _, n := range []string{"3001", "3023", "3001", " 3024 ", ""} {
		if err := d.AddRecent(n); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.Recents(10)
	if err != nil || strings.Join(got, ",") != "3024,3001,3023" {
		t.Fatalf("recents = %v %v", got, err)
	}
	for i := 0; i < recentKeep+10; i++ {
		_ = d.AddRecent(fmt.Sprintf("p%d", i))
	}
	if all, _ := d.Recents(1000); len(all) != recentKeep || all[0] != fmt.Sprintf("p%d", recentKeep+9) {
		t.Errorf("only the newest %d are kept: %d %v", recentKeep, len(all), all[:1])
	}
	if two, _ := d.Recents(2); len(two) != 2 {
		t.Errorf("limit: %v", two)
	}
}

func TestBackupCollectionKeepsYoursAndDropsTheCatalog(t *testing.T) {
	d := loadedCatalog(t)
	own(t, d, "3001", 4, "Red", 7)
	_ = d.UpsertSet(Set{SetNum: "75192", Name: "Falcon", Qty: 1, PartsQty: 7541})
	dir := t.TempDir()
	path, err := d.BackupCollection(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if got, _ := b.GetOwnedPart("3001", 4, "Red"); got == nil || got.Qty != 7 {
		t.Errorf("the backup must hold your parts: %+v", got)
	}
	if s, _ := b.GetSetByNum("75192"); s == nil {
		t.Error("the backup must hold your sets")
	}
	var n int
	if err := b.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'cat\_%' ESCAPE '\' AND name NOT IN ('cat_parts','cat_sets','cat_themes','cat_colors','cat_categories','cat_elements','cat_minifigs','cat_inventories','cat_inventory_parts','cat_inventory_sets','cat_inventory_minifigs','cat_part_relations','cat_element_ids','cat_meta')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	// Open() recreates empty schema tables, so the test is that they are EMPTY, not absent.
	for _, tbl := range []string{"cat_parts", "cat_sets", "cat_inventory_parts"} {
		if c := b.count(tbl); c != 0 {
			t.Errorf("%s has %d rows in the backup; the catalog should not be copied", tbl, c)
		}
	}
	if _, err := d.BackupCollection(dir); err != nil {
		t.Errorf("a second backup in the same dir must work: %v", err)
	}
}

func TestCatalogSetInventoryUsesTheNewestVersionAndSkipsSpares(t *testing.T) {
	db := loadedCatalog(t)
	db.SetBLColors([]BLColorRow{{RBID: 4, BLID: 5, Name: "Red"}})
	// 30008-1 has inventory versions 1 (id 2, no parts in the fixture) and 2 (id 3: 3001 red x4, 3023 blue x2)
	items, err := db.CatalogSetInventory("30008")
	if err != nil || len(items) != 2 {
		t.Fatalf("items = %+v %v", items, err)
	}
	if it := items[0]; it.PartNum != "3001" || it.Qty != 4 || it.ColorName != "Red" || it.PartName != "Brick 2 x 4" || it.BLColor != 5 || it.BrickLinkID != "" {
		t.Errorf("first = %+v", it)
	}
	if it := items[1]; it.PartNum != "3023" || it.Qty != 2 || it.BLColor != 0 {
		t.Errorf("second (no BrickLink colour mapped) = %+v", it)
	}
	// set 75192-1: 3001 red x10 and 3023 blue x6; the spare 3001 x2 is not stored
	items, _ = db.CatalogSetInventory("75192-1")
	if len(items) != 2 || items[0].Qty != 10 {
		t.Errorf("75192 = %+v", items)
	}
	if items, err := db.CatalogSetInventory("99999"); err != nil || items != nil {
		t.Errorf("an unknown set is empty, not an error: %v %v", items, err)
	}
}

func TestSetInventoryChainOfflineThenLive(t *testing.T) {
	ctx := context.Background()
	db := loadedCatalog(t)
	got := db.LookupSetInventory(ctx, &Client{}, "75192")
	if len(got.Items) != 2 || !strings.HasPrefix(got.Source, "offline catalog") {
		t.Fatalf("offline = %+v", got)
	}
	// a set the catalog lacks falls through to live Rebrickable
	c, _ := inventoryServer(t)
	empty := openScratchDB(t)
	got = empty.LookupSetInventory(ctx, c, "75192")
	if len(got.Items) != 3 || got.Source != "live — Rebrickable" {
		t.Fatalf("live = %+v", got)
	}
	// neither: explains how to fix it
	got = empty.LookupSetInventory(ctx, &Client{}, "75192")
	if len(got.Items) != 0 || len(got.Notes) != 1 || !strings.Contains(got.Notes[0], "catalog refresh") {
		t.Errorf("nothing = %+v", got)
	}
	if got := db.LookupSetInventory(ctx, &Client{}, "00000"); len(got.Items) != 0 || !strings.Contains(strings.Join(got.Notes, " "), "no parts list") {
		t.Errorf("a loaded catalog without this set = %+v", got)
	}
}

func TestSetsUsingPartCountsFromTheCatalog(t *testing.T) {
	db := loadedCatalog(t)
	total, ex, err := db.SetsUsingPart("3001", 5)
	if err != nil || total != 2 || len(ex) != 2 { // 75192-1 and 30008-1
		t.Fatalf("total=%d examples=%+v err=%v", total, ex, err)
	}
	if ex[0].Year < ex[1].Year {
		t.Errorf("newest first: %+v", ex)
	}
	if total, ex, _ := db.SetsUsingPart("nope", 5); total != 0 || ex != nil {
		t.Errorf("unknown part: %d %v", total, ex)
	}
}

func equivDB(t *testing.T) *DB {
	d := loadedCatalog(t) // has relations: M 3001a<->3001, A 3023<->3023b, P 3001pr0001<->3001
	return d
}

func TestEquivalentPartsCountAsHeldWhenAsked(t *testing.T) {
	d := equivDB(t)
	own(t, d, "3001", 4, "Red", 3)
	own(t, d, "3001a", 4, "Red", 4)      // a mould variant
	own(t, d, "3001pr0001", 4, "Red", 2) // a printed version
	own(t, d, "3023b", 1, "Blue", 6)     // an alternate of 3023
	own(t, d, "3001a", 1, "Blue", 100)   // wrong colour: never counts for red
	items := []InvItem{{PartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 10}, {PartNum: "3023", ColorID: 1, ColorName: "Blue", Qty: 6}}

	def, err := d.MissingFor(items, 1) // alternates + moulds
	if err != nil {
		t.Fatal(err)
	}
	if def.PiecesHeld != 7+6 || def.SubstitutedPieces != 4+6 || def.Complete != 1 || len(def.Missing) != 1 {
		t.Fatalf("default = %+v", def)
	}
	if m := def.Missing[0]; m.PartNum != "3001" || m.Have != 7 || m.Short != 3 || len(m.Substituted) != 1 || m.Substituted[0] != "4 x 3001a" {
		t.Errorf("the mould covers 4 and is named so it can be checked: %+v", m)
	}

	exact, _ := d.MissingForWith(items, 1, EquivNone)
	if exact.PiecesHeld != 3 || exact.SubstitutedPieces != 0 || exact.Complete != 0 {
		t.Errorf("exact only = %+v", exact)
	}
	withPrint, _ := d.MissingForWith(items, 1, EquivWithPrint)
	if withPrint.Missing[0].Short != 1 || withPrint.SubstitutedPieces != 4+2+6 {
		t.Errorf("prints on = %+v", withPrint)
	}
}

func TestEachHeldPieceIsUsedOnlyOnce(t *testing.T) {
	d := equivDB(t)
	own(t, d, "3001a", 4, "Red", 4) // the only stock is the mould variant
	// The set needs the base part AND the mould variant, 4 of each: the 4 held pieces cover one line, not both.
	items := []InvItem{{PartNum: "3001", ColorID: 4, Qty: 4}, {PartNum: "3001a", ColorID: 4, Qty: 4}}
	r, _ := d.MissingFor(items, 1)
	if r.PiecesHeld != 4 || r.Complete != 1 || len(r.Missing) != 1 || r.Missing[0].PartNum != "3001" {
		t.Errorf("a piece must not be counted for two lines: %+v", r)
	}
}

func TestParseEquivalents(t *testing.T) {
	for in, want := range map[string]Equivalents{"": EquivDefault, "default": EquivDefault, "none": EquivNone, "off": EquivNone, "alt": "A", "mold,print": "MP", "Alt + Mould": "AM", "a m p": "AMP"} {
		if got, err := ParseEquivalents(in); err != nil || got != want {
			t.Errorf("ParseEquivalents(%q) = %q %v, want %q", in, got, err, want)
		}
	}
	if _, err := ParseEquivalents("twin"); err == nil {
		t.Error("an unknown kind is refused")
	}
	if EquivNone.Label() != "exact part numbers only" || EquivDefault.Label() != "alternates + moulds" {
		t.Errorf("labels: %q %q", EquivNone.Label(), EquivDefault.Label())
	}
}
