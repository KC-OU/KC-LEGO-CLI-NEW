package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

const falconInventory = `{"next":null,"results":[
 {"part":{"part_num":"3001","name":"Brick 2 x 4","external_ids":{"BrickLink":["3001"]}},"color":{"id":4,"name":"Red","rgb":"C91A09","external_ids":{"BrickLink":{"ext_ids":[5]}}},"quantity":10,"is_spare":false},
 {"part":{"part_num":"3023","name":"Plate 1 x 2","external_ids":{"BrickLink":["3023"]}},"color":{"id":1,"name":"Blue","rgb":"0055BF","external_ids":{"BrickLink":{"ext_ids":[7]}}},"quantity":6,"is_spare":false}]}`

func TestMinimumIsSetInTheFlowShownAsLowAndClearedByRestocking(t *testing.T) {
	app, fake := flowApp(t)
	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Red")
	confirm(app, scrPartConfirm, map[int]string{cfQty: "3", cfMin: "5", cfConfirm: "yes"})

	got, _ := app.legoDB.GetOwnedPart("3001", 4, "Red")
	if got == nil || got.MinQty != 5 || !got.IsLow() {
		t.Fatalf("row = %+v (msg %q)", got, app.message)
	}
	var minamount float64
	if err := app.pdb.QueryRow("SELECT minamount FROM parts WHERE ipn = '3001-4'").Scan(&minamount); err != nil || minamount != 5 {
		t.Errorf("Part-DB's minimum amount should follow: %v %v", minamount, err)
	}
	_ = fake

	app.cur, app.stack = scrHub, nil
	app.goTo(scrLegoHub)
	if out := plain(app.View()); !strings.Contains(out, "LOW STOCK: 1 part(s)") {
		t.Errorf("the hub must show the low-stock badge:\n%s", out)
	}
	app.goTo(scrLegoPartOwned)
	if out := plain(app.View()); !strings.Contains(out, "3 LOW (min 5)") {
		t.Errorf("the owned list must mark it LOW:\n%s", out)
	}
	app.cur, app.stack = scrHub, nil
	app.goTo(scrOverview)
	if out := plain(app.View()); !strings.Contains(out, "LEGO Parts Below Minimum") || !strings.Contains(out, "1 LOW") {
		t.Errorf("Overview must show it:\n%s", out)
	}

	// Updating: the existing minimum is the default, and restocking clears the badge.
	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Red")
	if got := app.screens[scrPartConfirm].ActiveForm().Fields[cfMin].Value; got != "5" {
		t.Errorf("the existing minimum should be the default, got %q", got)
	}
	confirm(app, scrPartConfirm, map[int]string{cfQty: "9", cfConfirm: "yes"})
	if low, _ := app.legoDB.LowStock(); len(low) != 0 {
		t.Errorf("restocked above the minimum: %v", low)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyF9}) // undo puts the quantity AND the minimum back
	if got, _ := app.legoDB.GetOwnedPart("3001", 4, "Red"); got.Qty != 3 || got.MinQty != 5 {
		t.Errorf("undo = %+v", got)
	}
}

func TestMinimumIsValidatedAndNotAskedForManualParts(t *testing.T) {
	app, _ := flowApp(t)
	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Red")
	for _, bad := range []string{"x", "-1", "99999999"} {
		confirm(app, scrPartConfirm, map[int]string{cfQty: "3", cfMin: bad, cfConfirm: "yes"})
		if app.cur != scrPartConfirm || !app.messageErr || !strings.Contains(app.message, "minimum") {
			t.Errorf("minimum %q must be rejected: cur=%q msg=%q", bad, app.cur, app.message)
		}
	}
	confirm(app, scrPartConfirm, map[int]string{cfQty: "3", cfMin: "", cfConfirm: "yes"}) // blank = don't track
	if got, _ := app.legoDB.GetOwnedPart("3001", 4, "Red"); got == nil || got.MinQty != 0 {
		t.Errorf("a blank minimum means none: %+v", got)
	}

	enter(t, app, scrPartDBCreate, "LM358")
	pickText(t, app, scrPick, "(Other)")
	if f := app.screens[scrPartConfirm].ActiveForm().Fields[cfMin]; !f.Protected {
		t.Error("the minimum is for LEGO parts only")
	}
}

func TestStatsScreenSummarisesTheCollection(t *testing.T) {
	app := newTestApp(t)
	app.enterHub()
	_ = app.legoDB.AddOwnedPart(ownedFor("3001", "Brick 2 x 4", "Bricks"))
	app.cur, app.stack = scrLegoHub, nil
	app.goTo(scrLegoStats)
	out := plain(app.View())
	for _, want := range []string{"Collection at a glance", "Loose parts", "5 piece(s)", "Red 5", "Bricks 5"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats missing %q:\n%s", want, out)
		}
	}
}

func TestMissingPartsScreenComparesASetWithWhatYouHold(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = fakeRB(t, map[string]string{"/sets/75192-1/parts/": falconInventory}, nil)
	_ = app.legoDB.AddOwnedPart(ownedFor("3001", "Brick 2 x 4", "Bricks")) // 5 of the 10 red bricks

	app.cur, app.stack = scrLegoHub, nil
	app.goTo(scrLegoMissingAsk)
	app.screens[scrLegoMissingAsk].(*formScreen).submit(app, []string{"75192", ""})
	if app.cur != scrLegoMissing {
		t.Fatalf("expected the result, on %q (msg %q)", app.cur, app.message)
	}
	out := plain(app.View())
	for _, want := range []string{"75192 x1", "you hold 31%", "5 of 16 pieces", "0 of 2 lines complete", "Brick 2 x 4", "Plate 1 x 2", "wms lego wanted --set 75192"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing-parts screen lacks %q:\n%s", want, out)
		}
	}
	app.cur, app.stack = scrLegoHub, nil
	app.goTo(scrLegoMissingAsk)
	app.screens[scrLegoMissingAsk].(*formScreen).submit(app, []string{"75192", "2"})
	if out := plain(app.View()); !strings.Contains(out, "x2") || !strings.Contains(out, "5 of 32 pieces") {
		t.Errorf("two copies need twice the parts:\n%s", out)
	}
}

func TestMissingPartsErrorsAreExplained(t *testing.T) {
	app, _ := flowApp(t)
	ask := func(num, copies string) {
		app.cur, app.stack = scrLegoHub, nil
		app.goTo(scrLegoMissingAsk)
		app.screens[scrLegoMissingAsk].(*formScreen).submit(app, []string{num, copies})
	}
	ask("", "")
	if !app.messageErr || app.cur != scrLegoMissingAsk {
		t.Error("a blank set number is refused")
	}
	for _, c := range []string{"0", "x", "100", "-2"} {
		ask("75192", c)
		if !app.messageErr || !strings.Contains(app.message, "Copies") {
			t.Errorf("copies %q: %q", c, app.message)
		}
	}
	ask("99999", "")
	if !strings.Contains(app.message, `no set "99999"`) {
		t.Errorf("unknown set: %q", app.message)
	}
	app.rebrick.APIKey = ""
	ask("75192", "")
	if !strings.Contains(app.message, "catalog refresh") {
		t.Errorf("no key and no catalog: %q", app.message)
	}
	app.legoMissing = nil
	app.cur = scrLegoHub
	app.goTo(scrLegoMissing)
	_ = app.View() // survives being reached with no result
}

func TestRecentPartsAreOfferedAndReusedWithABang(t *testing.T) {
	app, _ := flowApp(t)
	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Red")
	confirm(app, scrPartConfirm, map[int]string{cfQty: "1", cfConfirm: "yes"})

	app.cur, app.stack = scrLegoHub, nil
	app.goTo(scrLegoPartAdd)
	if out := plain(app.View()); !strings.Contains(out, "Recent: !1 3001") {
		t.Errorf("the prompt should list recent parts:\n%s", out)
	}
	app.screens[scrLegoPartAdd].(*formScreen).submit(app, []string{"!1"})
	if app.cur != scrPick || app.partFlow == nil || app.partFlow.Num != "3001" {
		t.Fatalf("!1 should start the flow for 3001: cur=%q flow=%+v msg=%q", app.cur, app.partFlow, app.message)
	}
	app.cur, app.stack = scrLegoHub, nil
	app.goTo(scrLegoPartAdd)
	app.screens[scrLegoPartAdd].(*formScreen).submit(app, []string{"!7"})
	if app.cur != scrLegoPartAdd || !app.messageErr {
		t.Errorf("an unknown recent must be refused: cur=%q msg=%q", app.cur, app.message)
	}
}

// seedOfflineCatalog loads a small offline catalog into the test app's store.
func seedOfflineCatalog(t *testing.T, app *App) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO cat_categories (id, name) VALUES (11, 'Bricks')`,
		`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('3001', 'Brick 2 x 4', 11)`,
		`INSERT INTO cat_colors (id, name, rgb, is_trans) VALUES (4, 'Red', 'C91A09', 0), (1, 'Blue', '0055BF', 0)`,
		`INSERT INTO cat_elements (part_num, color_id) VALUES ('3001', 4), ('3001', 1)`,
		`INSERT INTO cat_element_ids (element_id, part_num, color_id) VALUES ('300121', '3001', 4)`,
		`INSERT INTO cat_themes (id, name, parent_id) VALUES (158, 'Star Wars', 0), (171, 'Ultimate Collector Series', 158)`,
		`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES ('75192-1', 'Millennium Falcon', 2017, 171, 7541, '')`,
		`INSERT INTO fts_parts(fts_parts) VALUES('rebuild')`,
		`INSERT INTO fts_sets(fts_sets) VALUES('rebuild')`,
		`INSERT INTO cat_meta (key, value) VALUES ('refreshed_ms', '1789900000000')`,
	} {
		if _, err := app.legoDB.Exec(q); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}
}

func TestEverythingWorksOfflineWithNoKeyAndNoNetwork(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{} // no key: nothing may reach Rebrickable
	seedOfflineCatalog(t, app)

	// search sets
	if out := searchSets(app, "falcon"); !strings.Contains(out, "Millennium Falcon") || !strings.Contains(out, "Star Wars - Ultimate") || !strings.Contains(out, "offline catalog, refreshed") {
		t.Errorf("offline set search:\n%s", out)
	}
	// search parts
	app.cur, app.stack = scrLegoHub, nil
	app.legoSearchTerm = "brick 2 x 4"
	app.goTo(scrLegoPartResults)
	if out := plain(app.View()); !strings.Contains(out, "Brick 2 x 4") || !strings.Contains(out, "Bricks") || !strings.Contains(out, "offline catalog") {
		t.Errorf("offline part search:\n%s", out)
	}
	// add a set: name, year, pieces and the full theme all filled in
	startSet(t, app, "75192")
	if d := app.legoSetDraft; d.Manual || d.Name != "Millennium Falcon" || d.Year != 2017 || d.Pieces != 7541 || d.Theme != "Star Wars - Ultimate Collector Series" || d.Source != "offline catalog" {
		t.Errorf("offline set draft = %+v", d)
	}
	confirm(app, scrLegoSetConfirm, map[int]string{sfQty: "1", sfConfirm: "yes"})
	if got, _ := app.legoDB.GetSetByNum("75192"); got == nil || got.PartsQty != 7541 {
		t.Errorf("set saved from the offline catalog: %+v", got)
	}
	// add a part: name, category and the colours it exists in, from the catalog
	enter(t, app, scrLegoPartAdd, "3001")
	out := plain(app.View())
	if app.cur != scrPick || !strings.Contains(out, "Red") || !strings.Contains(out, "Blue") {
		t.Fatalf("offline colour list:\n%s", out)
	}
	pickText(t, app, scrPick, "Red")
	confirm(app, scrPartConfirm, map[int]string{cfQty: "4", cfConfirm: "yes"})
	if got, _ := app.legoDB.GetOwnedPart("3001", 4, "Red"); got == nil || got.Name != "Brick 2 x 4" || got.Category != "Bricks" {
		t.Errorf("part saved from the offline catalog: %+v (msg %q)", got, app.message)
	}
}

func TestAnElementIDInThePartPromptFindsThePartAndColour(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	enter(t, app, scrLegoPartAdd, "300121")
	if app.cur != scrPartConfirm || app.partFlow == nil || app.partFlow.Num != "3001" || app.partFlow.ColorID != 4 || app.partFlow.ColorName != "Red" {
		t.Fatalf("element 300121 should resolve to 3001 in Red: cur=%q flow=%+v msg=%q", app.cur, app.partFlow, app.message)
	}
	if !strings.Contains(app.message, "Element 300121") {
		t.Errorf("say how it was resolved: %q", app.message)
	}
	confirm(app, scrPartConfirm, map[int]string{cfQty: "2", cfConfirm: "yes"})
	if got, _ := app.legoDB.GetOwnedPart("3001", 4, "Red"); got == nil || got.Qty != 2 {
		t.Errorf("saved = %+v", got)
	}
	// a real part number wins over an element id of the same digits
	if _, err := app.legoDB.Exec(`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('300121', 'Weird numeric part', 11)`); err != nil {
		t.Fatal(err)
	}
	enter(t, app, scrLegoPartAdd, "300121")
	if app.partFlow.Num != "300121" {
		t.Errorf("an existing part number takes precedence: %+v", app.partFlow)
	}
}

func TestMissingPartsWorkOfflineWithNoKey(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	for _, q := range []string{
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10)`,
	} {
		if _, err := app.legoDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_ = app.legoDB.AddOwnedPart(ownedFor("3001", "Brick 2 x 4", "Bricks")) // 5 of 10
	app.cur, app.stack = scrLegoHub, nil
	app.goTo(scrLegoMissingAsk)
	app.screens[scrLegoMissingAsk].(*formScreen).submit(app, []string{"75192", ""})
	out := plain(app.View())
	if app.cur != scrLegoMissing || !strings.Contains(out, "offline catalog") || !strings.Contains(out, "you hold 50%") || !strings.Contains(out, "Brick 2 x 4") {
		t.Fatalf("offline missing parts (cur=%q msg=%q):\n%s", app.cur, app.message, out)
	}
}

func TestWhatCanIBuildScreenAndDetailShortcut(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	for _, q := range []string{
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,30)`,
	} {
		if _, err := app.legoDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	app.cur, app.stack = scrLegoHub, nil
	app.goTo(scrLegoBuild)
	if out := plain(app.View()); !strings.Contains(out, "No set is half covered") {
		t.Errorf("owning nothing:\n%s", out)
	}
	if err := app.legoDB.AddOwnedPart(lego.OwnedPart{PartNum: "3001", Name: "Brick", ColorID: 4, ColorName: "Red", Qty: 24}); err != nil {
		t.Fatal(err)
	}
	app.cur, app.stack = scrLegoHub, nil
	app.goTo(scrLegoBuild)
	out := plain(app.View())
	if !strings.Contains(out, "Millennium Falcon") || !strings.Contains(out, "80%") || !strings.Contains(out, "24/30") || !strings.Contains(out, "Star Wars - Ultimate") {
		t.Errorf("build results:\n%s", out)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if app.cur != scrLegoDetailAsk {
		t.Errorf("D opens the detail prompt: %q", app.cur)
	}
	// the hub lists it and B opens it
	app.cur, app.stack = scrHub, nil
	app.goTo(scrLegoHub)
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if app.cur != scrLegoBuild {
		t.Errorf("B on the hub: %q", app.cur)
	}
}

func TestHistoryScreenShowsWhoChangedWhatAndTheSessionUserIsRecorded(t *testing.T) {
	app, _ := flowApp(t) // Rebrickable is faked, so the part flow finds 3001
	app.enterHub()       // sign-on completes: the journal now names the user
	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Red")
	confirm(app, scrPartConfirm, map[int]string{cfQty: "7", cfConfirm: "yes"})
	app.cur, app.stack = scrLegoHub, nil
	app.goTo(scrLegoHistory)
	out := plain(app.View())
	if !strings.Contains(out, "admin") || !strings.Contains(out, "add") || !strings.Contains(out, "0 -> 7") || !strings.Contains(out, "part 3001") {
		t.Errorf("history:\n%s", out)
	}
	app.cur, app.stack = scrHub, nil
	app.goTo(scrLegoHub)
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if app.cur != scrLegoHistory {
		t.Errorf("H on the hub: %q", app.cur)
	}
}
