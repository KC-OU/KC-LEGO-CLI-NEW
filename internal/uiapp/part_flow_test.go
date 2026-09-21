package uiapp

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb/partdbtest"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// brickRoutes is what Rebrickable says about part 3001 (and its colour table).
var brickRoutes = map[string]string{
	"/parts/3001/":         `{"part_num":"3001","name":"Brick 2 x 4","part_cat_id":11}`,
	"/part_categories/11/": `{"id":11,"name":"Bricks"}`,
	"/parts/3001/colors/":  `{"results":[{"color_id":4,"color_name":"Red","num_sets":900},{"color_id":1,"color_name":"Blue","num_sets":800},{"color_id":72,"color_name":"Dark Bluish Gray","num_sets":700}]}`,
	"/colors/":             `{"results":[{"id":4,"name":"Red","rgb":"C91A09"},{"id":1,"name":"Blue","rgb":"0055BF"},{"id":72,"name":"Dark Bluish Gray","rgb":"6C6E68"}]}`,
}

func flowApp(t *testing.T) (*App, *partdbtest.Fake) {
	t.Helper()
	app, fake := newTestEnv(t)
	app.rebrick = fakeRB(t, brickRoutes, nil)
	return app, fake
}

// enter starts the flow from a hub the way the menus do.
func enter(t *testing.T, app *App, entry, part string) {
	t.Helper()
	app.cur, app.stack = scrLegoHub, nil
	app.goTo(entry)
	app.screens[entry].(*formScreen).submit(app, []string{part})
}

func pickText(t *testing.T, app *App, want, text string) {
	t.Helper()
	if app.cur != want {
		t.Fatalf("expected screen %q, on %q (msg %q)", want, app.cur, app.message)
	}
	pickSubmit(app, text)
}

func partIDByIPN(t *testing.T, app *App, ipn string) int {
	t.Helper()
	id, err := app.pdb.FindPartByIPN(ipn)
	if err != nil || id == 0 {
		t.Fatalf("Part-DB has no part with IPN %q (id=%d err=%v; msg %q)", ipn, id, err, app.message)
	}
	return id
}

func TestAddLegoPartFillsInFromRebrickableAndWritesBothPlaces(t *testing.T) {
	app, fake := flowApp(t)
	enter(t, app, scrLegoPartAdd, " 3001 ")

	if app.cur != scrPick {
		t.Fatalf("expected the colour picker, on %q (msg %q)", app.cur, app.message)
	}
	out := plain(app.View())
	for _, want := range []string{"Colours 3001 comes in", "Red", "Blue", "Dark Bluish Gray"} {
		if !strings.Contains(out, want) {
			t.Errorf("colour picker missing %q:\n%s", want, out)
		}
	}
	pickText(t, app, scrPick, "red")

	if app.cur != scrPartConfirm {
		t.Fatalf("expected the confirm screen, on %q (msg %q)", app.cur, app.message)
	}
	out = plain(app.View())
	for _, want := range []string{"Brick 2 x 4", "Lego > Bricks", "Red", "Rebrickable"} {
		if !strings.Contains(out, want) {
			t.Errorf("confirm screen missing %q:\n%s", want, out)
		}
	}
	confirm(app, scrPartConfirm, map[int]string{cfQty: "25", cfConfirm: "yes"})

	got, _ := app.legoDB.GetOwnedPart("3001", 4, "Red")
	if got == nil || got.Qty != 25 || got.Name != "Brick 2 x 4" || got.Category != "Bricks" {
		t.Fatalf("LEGO row = %+v (msg %q)", got, app.message)
	}
	id := partIDByIPN(t, app, "3001-4")
	pd, err := app.pdb.GetPart(id)
	if err != nil || pd.Name != "Brick 2 x 4 - Red" || pd.Category != "Bricks" || pd.TotalStock != 25 {
		t.Fatalf("Part-DB part = %+v err=%v", pd, err)
	}
	if pd.MfgPN != "" {
		t.Errorf("the manufacturer part number must stay empty so ModernWMS follows the IPN, got %q", pd.MfgPN)
	}
	if app.cur != scrLegoHub || app.messageErr || !strings.Contains(app.message, "part #") {
		t.Errorf("want a success message back on the hub, got cur=%q err=%v msg=%q", app.cur, app.messageErr, app.message)
	}
	for _, c := range fake.Comments() {
		if c != "wms-go" {
			t.Errorf("every write must carry _comment=wms-go, saw %q", c)
		}
	}
	if app.partFlow != nil {
		t.Error("the flow state should be cleared after saving")
	}
}

func TestUpdatingALegoPartChangesOnlyTheQuantityAndUndoRestoresIt(t *testing.T) {
	app, _ := flowApp(t)
	add := func(qty string) {
		enter(t, app, scrLegoPartAdd, "3001")
		pickText(t, app, scrPick, "Red")
		confirm(app, scrPartConfirm, map[int]string{cfQty: qty, cfConfirm: "yes"})
	}
	add("25")
	first := partIDByIPN(t, app, "3001-4")

	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Red")
	if got := app.screens[scrPartConfirm].ActiveForm().Fields[cfQty].Value; got != "25" {
		t.Errorf("the quantity you already hold should be the default, got %q", got)
	}
	if out := plain(app.View()); !strings.Contains(out, "25 in your LEGO collection") {
		t.Errorf("the confirm screen should say what you already have:\n%s", out)
	}
	confirm(app, scrPartConfirm, map[int]string{cfQty: "40", cfConfirm: "yes"})

	if second := partIDByIPN(t, app, "3001-4"); second != first {
		t.Errorf("updating must reuse Part-DB part #%d, got #%d", first, second)
	}
	if q, _ := app.pdbw.Stock(first); q != 40 {
		t.Errorf("Part-DB stock = %g, want 40", q)
	}
	if got, _ := app.legoDB.GetOwnedPart("3001", 4, "Red"); got.Qty != 40 {
		t.Errorf("LEGO qty = %d, want 40", got.Qty)
	}

	app.Update(tea.KeyMsg{Type: tea.KeyF9})
	if got, _ := app.legoDB.GetOwnedPart("3001", 4, "Red"); got == nil || got.Qty != 25 {
		t.Errorf("undo should bring the LEGO row back to 25: %+v (msg %q)", got, app.message)
	}
	if q, _ := app.pdbw.Stock(first); q != 25 {
		t.Errorf("undo should bring Part-DB stock back to 25, got %g (msg %q)", q, app.message)
	}
}

func TestDifferentColoursAreDifferentParts(t *testing.T) {
	app, _ := flowApp(t)
	for _, c := range []string{"Red", "Blue"} {
		enter(t, app, scrLegoPartAdd, "3001")
		pickText(t, app, scrPick, c)
		confirm(app, scrPartConfirm, map[int]string{cfQty: "5", cfConfirm: "yes"})
	}
	if partIDByIPN(t, app, "3001-4") == partIDByIPN(t, app, "3001-1") {
		t.Error("red and blue must be separate Part-DB parts")
	}
}

func TestATypedColourIsKeptAsFreeText(t *testing.T) {
	app, _ := flowApp(t)
	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Sparkly Purple")
	confirm(app, scrPartConfirm, map[int]string{cfQty: "2", cfConfirm: "yes"})
	ipn := lego.IPNFor("3001", lego.NoColor, "Sparkly Purple")
	partIDByIPN(t, app, ipn)
	if got, _ := app.legoDB.GetOwnedPart("3001", lego.NoColor, "Sparkly Purple"); got == nil || got.Qty != 2 {
		t.Errorf("free-text colour row = %+v", got)
	}
}

func TestAPartWithNoColourAnswerCanBeBlank(t *testing.T) {
	app, _ := flowApp(t)
	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "")
	confirm(app, scrPartConfirm, map[int]string{cfQty: "7", cfConfirm: "yes"})
	partIDByIPN(t, app, "3001")
}

func TestConfirmValidation(t *testing.T) {
	app, _ := flowApp(t)
	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Red")

	for _, bad := range []string{"", "x", "-3", "1.5", "99999999"} {
		confirm(app, scrPartConfirm, map[int]string{cfQty: bad, cfConfirm: "yes"})
		if app.cur != scrPartConfirm || !app.messageErr {
			t.Errorf("quantity %q must be rejected on the confirm screen (cur=%q msg=%q)", bad, app.cur, app.message)
		}
	}
	confirm(app, scrPartConfirm, map[int]string{cfQty: "3", cfConfirm: ""})
	if app.cur != scrPartConfirm || !app.messageErr || !strings.Contains(app.message, "yes") {
		t.Errorf("a blank confirmation must ask for yes/no: cur=%q msg=%q", app.cur, app.message)
	}
	if got, _ := app.legoDB.GetOwnedPart("3001", 4, "Red"); got != nil {
		t.Errorf("nothing may be saved before an explicit yes: %+v", got)
	}
	confirm(app, scrPartConfirm, map[int]string{cfQty: "3", cfConfirm: "no"})
	if app.cur != scrLegoHub || strings.Contains(app.message, "part #") {
		t.Errorf("no should cancel back to the hub: cur=%q msg=%q", app.cur, app.message)
	}
	if id, _ := app.pdb.FindPartByIPN("3001-4"); id != 0 {
		t.Error("cancelling must not create a Part-DB part")
	}
}

func TestUnknownPartIsEnteredByHandIntoPartDBOnly(t *testing.T) {
	app, _ := flowApp(t)
	app.cur, app.stack = scrHub, nil
	app.goTo(scrPartDBHub)
	app.goTo(scrPartDBCreate)
	app.screens[scrPartDBCreate].(*formScreen).submit(app, []string{"LM358"})
	if !strings.Contains(app.message, `No source knows part "LM358"`) {
		t.Fatalf("expected the manual-entry explanation, got %q", app.message)
	}
	pickText(t, app, scrPick, "Op-amps") // not an existing category: a new one
	if app.cur != scrPartConfirm {
		t.Fatalf("expected confirm, on %q (msg %q)", app.cur, app.message)
	}
	confirm(app, scrPartConfirm, map[int]string{cfQty: "10", cfConfirm: "yes"})
	if !strings.Contains(app.message, "Name is required") {
		t.Errorf("a manual part needs a name: %q", app.message)
	}
	confirm(app, scrPartConfirm, map[int]string{cfName: "LM358 dual op-amp", cfDesc: "SOIC-8", cfMfg: "LM358DR", cfQty: "10", cfConfirm: "yes"})

	id := partIDByIPN(t, app, "LM358")
	pd, _ := app.pdb.GetPart(id)
	if pd.Name != "LM358 dual op-amp" || pd.MfgPN != "LM358DR" || pd.Category != "Op-amps" || pd.TotalStock != 10 {
		t.Errorf("Part-DB part = %+v", pd)
	}
	if owned, _ := app.legoDB.OwnedPartsOf("LM358"); len(owned) != 0 {
		t.Errorf("a non-LEGO part must not land in the LEGO collection: %+v", owned)
	}
	if app.cur != scrPartDBHub {
		t.Errorf("should return to where it started, on %q", app.cur)
	}
}

func TestPickingAnExistingCategoryReusesIt(t *testing.T) {
	app, _ := flowApp(t)
	enter(t, app, scrPartDBCreate, "NE555")
	pickText(t, app, scrPick, "(Other)")
	confirm(app, scrPartConfirm, map[int]string{cfName: "NE555 timer", cfQty: "1", cfConfirm: "yes"})
	pd, _ := app.pdb.GetPart(partIDByIPN(t, app, "NE555"))
	if pd.Category != "(Other)" {
		t.Errorf("category = %q, want the existing (Other)", pd.Category)
	}
	cats, _ := app.pdb.AllCategories()
	if len(cats) != 1 {
		t.Errorf("picking an existing category must not create another: %+v", cats)
	}
}

func TestNoTokenKeepsTheLegoRowAndSaysPartDBWasSkipped(t *testing.T) {
	app, _ := flowApp(t)
	t.Setenv(config.PartDBAPIToken, "")
	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Red")
	confirm(app, scrPartConfirm, map[int]string{cfQty: "9", cfConfirm: "yes"})

	if got, _ := app.legoDB.GetOwnedPart("3001", 4, "Red"); got == nil || got.Qty != 9 {
		t.Fatalf("what you typed must not be lost when Part-DB can't be reached: %+v", got)
	}
	if !app.messageErr || !strings.Contains(app.message, "NOT in Part-DB") || !strings.Contains(app.message, "token") {
		t.Errorf("the message should say Part-DB was skipped and why: %q", app.message)
	}
	if id, _ := app.pdb.FindPartByIPN("3001-4"); id != 0 {
		t.Error("nothing may reach Part-DB without a token")
	}
}

func TestPartDBRejectingTheTokenKeepsTheLegoRow(t *testing.T) {
	app, _ := flowApp(t)
	t.Setenv(config.PartDBAPIToken, "wrong")
	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Red")
	confirm(app, scrPartConfirm, map[int]string{cfQty: "9", cfConfirm: "yes"})
	if got, _ := app.legoDB.GetOwnedPart("3001", 4, "Red"); got == nil {
		t.Fatal("the LEGO row must survive a Part-DB failure")
	}
	if !app.messageErr || !strings.Contains(app.message, "NOT in Part-DB") {
		t.Errorf("msg = %q", app.message)
	}
}

func TestPartFlowValidationAndReadOnlyRoles(t *testing.T) {
	app, _ := flowApp(t)
	app.cur = scrLegoHub
	app.goTo(scrLegoPartAdd)
	app.screens[scrLegoPartAdd].(*formScreen).submit(app, []string{"   "})
	if app.cur != scrLegoPartAdd || !app.messageErr {
		t.Errorf("a blank part number must stay put with an error (cur=%q)", app.cur)
	}

	app.session = &auth.Session{Source: "modernwms", Username: "viewer", Role: "ViewOnly",
		Permissions: &wmsdb.Permissions{CanWrite: false}}
	for _, id := range []string{scrLegoPartAdd, scrPartDBCreate, scrPartConfirm, scrPick} {
		app.cur, app.stack = scrHub, nil
		app.goTo(id)
		if app.cur == id || !app.messageErr {
			t.Errorf("a read-only role must be bounced from %s (cur=%q msg=%q)", id, app.cur, app.message)
		}
	}
}

func TestPartFlowScreensSurviveBeingReachedWithoutState(t *testing.T) {
	app, _ := flowApp(t)
	app.partFlow, app.pick = nil, nil
	for _, id := range []string{scrPartConfirm, scrPick} {
		app.cur = scrHub
		app.goTo(id)
		_ = app.View()
		if scr, ok := app.screens[id].(*formScreen); ok {
			scr.submit(app, make([]string, 11))
		}
	}
}

func TestTypingThroughTheColourPickerWithKeys(t *testing.T) {
	app, _ := flowApp(t)
	enter(t, app, scrLegoPartAdd, "3001")
	for _, r := range "dark" {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if out := plain(app.View()); !strings.Contains(out, "Dark Bluish Gray") || strings.Contains(out, "Blue\n") {
		t.Errorf("typing should narrow the list to matching colours:\n%s", out)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.cur != scrPartConfirm || app.partFlow.ColorID != 72 {
		t.Errorf("a unique match should be picked: cur=%q flow=%+v msg=%q", app.cur, app.partFlow, app.message)
	}
}

func TestTestsNeverTouchLiveData(t *testing.T) {
	app, _ := newTestEnv(t)
	if app.pdb == nil {
		t.Fatal("no Part-DB")
	}
	for _, k := range []string{config.PartDBDBPath, config.SettingsFile, config.TwoFAFile, config.AuditLogFile, config.LegoDBPath} {
		v := os.Getenv(k)
		if v == "" || strings.HasPrefix(v, "/root/docker-server") || v == "/root/tui_audit.log" {
			t.Errorf("%s = %q must point at a temp file in tests", k, v)
		}
	}
	if got := config.Get(config.PartDBAPIURL); !strings.HasPrefix(got, "http://127.0.0.1:") || strings.Contains(got, ":8081") {
		t.Errorf("Part-DB API URL %q must be the fake server", got)
	}
}
