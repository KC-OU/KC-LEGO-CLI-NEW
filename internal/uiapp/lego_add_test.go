package uiapp

import (
	tea "github.com/charmbracelet/bubbletea"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// fakeRB serves fixed Rebrickable paths; anything not listed is a 404.
func fakeRB(t *testing.T, routes map[string]string, status map[string]int) *lego.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if st, ok := status[r.URL.Path]; ok {
			w.WriteHeader(st)
			return
		}
		if body, ok := routes[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return &lego.Client{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}
}

var falconRoutes = map[string]string{
	"/sets/75192-1/": `{"set_num":"75192-1","name":"Millennium Falcon","year":2017,"theme_id":171,"num_parts":7541}`,
	"/themes/171/":   `{"id":171,"parent_id":158,"name":"Ultimate Collector Series"}`,
	"/themes/158/":   `{"id":158,"parent_id":null,"name":"Star Wars"}`,
}

// confirm fills the confirm screen the way a user would and submits it.
func confirm(app *App, id string, set map[int]string) {
	scr := app.screens[id].(*formScreen)
	for i, v := range set {
		scr.fl.Fields[i].Value = v
	}
	scr.submit(app, scr.fl.Values())
}

func startSet(t *testing.T, app *App, input string) {
	t.Helper()
	app.cur = scrLegoHub
	app.goTo(scrLegoSetAdd)
	app.screens[scrLegoSetAdd].(*formScreen).submit(app, []string{input})
	if app.cur != scrLegoSetConfirm {
		t.Fatalf("expected the confirm screen, on %q (msg %q)", app.cur, app.message)
	}
}

func TestAddSetFillsEverythingFromRebrickable(t *testing.T) {
	app := newTestApp(t)
	app.rebrick = fakeRB(t, falconRoutes, nil)
	startSet(t, app, "75192") // typed without Rebrickable's -1

	d := app.legoSetDraft
	if d.Name != "Millennium Falcon" || d.Year != 2017 || d.Pieces != 7541 || d.Theme != "Star Wars - Ultimate Collector Series" || d.Manual {
		t.Fatalf("draft = %+v", d)
	}
	out := plain(app.View())
	for _, want := range []string{"Name: Millennium Falcon", "Year: 2017", "Pieces: 7541", "Theme: Star Wars - Ultimate Collector Series",
		"Details from: Rebrickable (live)", "Already owned: not in your collection yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("confirm screen missing %q:\n%s", want, out)
		}
	}
	if app.messageErr {
		t.Errorf("a clean lookup should not show an error: %q", app.message)
	}
	if got := app.screens[scrLegoSetConfirm].ActiveForm().Fields[sfQty].Value; got != "1" {
		t.Errorf("a new set should default to owning 1, got %q", got)
	}

	confirm(app, scrLegoSetConfirm, map[int]string{sfQty: "2", sfConfirm: "yes"})
	got, err := app.legoDB.GetSetByNum("75192")
	if err != nil || got.Name != "Millennium Falcon" || got.Qty != 2 || got.PartsQty != 7541 || got.Year != 2017 || got.Theme != "Star Wars - Ultimate Collector Series" {
		t.Fatalf("saved set = %+v (err %v)", got, err)
	}
	if app.cur != scrLegoHub || !strings.Contains(app.message, "added") || app.messageErr {
		t.Errorf("after saving expect the LEGO hub and a success message (cur=%q msg=%q)", app.cur, app.message)
	}
	if app.legoSetDraft != nil {
		t.Error("the draft must be cleared after saving")
	}
}

func TestUpdatingAnExistingSetOnlyChangesTheQuantity(t *testing.T) {
	app := newTestApp(t)
	app.rebrick = fakeRB(t, falconRoutes, nil)
	_ = app.legoDB.UpsertSet(lego.Set{SetNum: "75192", Name: "Falcon (mine)", Theme: "Star Wars - UCS", Year: 2017, PartsQty: 7541,
		InstructionBookNumber: "6208889", InstructionBookCount: 3, Qty: 2, PartedOut: true})

	startSet(t, app, "75192-1")
	if app.legoSetDraft.Existing == nil || app.legoSetDraft.Theme != "Star Wars - UCS" {
		t.Fatalf("what is already stored should win over the lookup: %+v", app.legoSetDraft)
	}
	out := plain(app.View())
	if got := app.screens[scrLegoSetConfirm].ActiveForm().Fields[sfQty].Value; got != "2" || !strings.Contains(out, "Already owned: 2") {
		t.Errorf("the current quantity should be the default (%q):\n%s", got, out)
	}

	confirm(app, scrLegoSetConfirm, map[int]string{sfQty: "5", sfConfirm: "y"})
	got, _ := app.legoDB.GetSetByNum("75192")
	if got.Qty != 5 || got.InstructionBookNumber != "6208889" || got.InstructionBookCount != 3 || !got.PartedOut || got.Theme != "Star Wars - UCS" || got.Name != "Falcon (mine)" {
		t.Fatalf("only the quantity should change: %+v", got)
	}
	if !strings.Contains(app.message, "updated") || !strings.Contains(app.message, "was 2") {
		t.Errorf("message should say it was an update: %q", app.message)
	}
}

func TestSetConfirmNeedsAnExplicitYes(t *testing.T) {
	for _, answer := range []string{"", "maybe", "ye"} {
		app := newTestApp(t)
		app.rebrick = fakeRB(t, falconRoutes, nil)
		startSet(t, app, "75192")
		confirm(app, scrLegoSetConfirm, map[int]string{sfQty: "1", sfConfirm: answer})
		if _, err := app.legoDB.GetSetByNum("75192"); err == nil || !app.messageErr || app.cur != scrLegoSetConfirm {
			t.Errorf("answer %q must not save and must stay on the confirm screen (cur=%q msg=%q)", answer, app.cur, app.message)
		}
	}
	app := newTestApp(t)
	app.rebrick = fakeRB(t, falconRoutes, nil)
	startSet(t, app, "75192")
	confirm(app, scrLegoSetConfirm, map[int]string{sfQty: "1", sfConfirm: "no"})
	if _, err := app.legoDB.GetSetByNum("75192"); err == nil {
		t.Error("\"no\" must not save")
	}
	if app.cur != scrLegoHub || !strings.Contains(app.message, "Cancelled") {
		t.Errorf("\"no\" should cancel back to the LEGO hub (cur=%q msg=%q)", app.cur, app.message)
	}
}

func TestSetQuantityIsValidated(t *testing.T) {
	for _, bad := range []string{"", "abc", "-1", "2.5", "100000"} {
		app := newTestApp(t)
		app.rebrick = fakeRB(t, falconRoutes, nil)
		startSet(t, app, "75192")
		confirm(app, scrLegoSetConfirm, map[int]string{sfQty: bad, sfConfirm: "yes"})
		if _, err := app.legoDB.GetSetByNum("75192"); err == nil || !app.messageErr {
			t.Errorf("quantity %q must be rejected (msg=%q)", bad, app.message)
		}
	}
	app := newTestApp(t)
	app.rebrick = fakeRB(t, falconRoutes, nil)
	startSet(t, app, "75192")
	confirm(app, scrLegoSetConfirm, map[int]string{sfQty: "0", sfConfirm: "yes"})
	if got, err := app.legoDB.GetSetByNum("75192"); err != nil || got.Qty != 0 {
		t.Errorf("0 is allowed (a set you no longer own): %+v %v", got, err)
	}
}

func TestSetNotOnRebrickableFallsBackToManualEntry(t *testing.T) {
	app := newTestApp(t)
	app.rebrick = fakeRB(t, nil, nil) // everything 404s
	startSet(t, app, "99999")
	if !app.legoSetDraft.Manual || !strings.Contains(app.message, `Rebrickable has no set "99999"`) || !app.messageErr {
		t.Fatalf("expected manual entry with the reason (msg=%q)", app.message)
	}

	confirm(app, scrLegoSetConfirm, map[int]string{sfQty: "1", sfConfirm: "yes"}) // no name yet
	if !app.messageErr || !strings.Contains(app.message, "Name is required") {
		t.Errorf("a manual set needs a name: %q", app.message)
	}
	for _, year := range []string{"abc", "1800", "2101"} {
		confirm(app, scrLegoSetConfirm, map[int]string{sfName: "Mystery", sfYear: year, sfQty: "1", sfConfirm: "yes"})
		if _, err := app.legoDB.GetSetByNum("99999"); err == nil || !strings.Contains(app.message, "Year") {
			t.Errorf("year %q must be rejected (msg=%q)", year, app.message)
		}
	}
	confirm(app, scrLegoSetConfirm, map[int]string{sfName: "Mystery", sfYear: "1999", sfPieces: "10", sfTheme: "Town", sfQty: "1", sfConfirm: "yes"})
	if got, err := app.legoDB.GetSetByNum("99999"); err != nil || got.Name != "Mystery" || got.Year != 1999 || got.PartsQty != 10 || got.Theme != "Town" {
		t.Fatalf("manual set = %+v %v", got, err)
	}
}

func TestRebrickableFailuresAreReportedAndFallBack(t *testing.T) {
	cases := map[string]struct {
		status int
		want   string
	}{"bad key": {http.StatusUnauthorized, "rejected the API key"}, "rate limit": {http.StatusTooManyRequests, "rate-limiting"}, "server error": {http.StatusInternalServerError, "lookup failed"}}
	for name, c := range cases {
		app := newTestApp(t)
		app.rebrick = fakeRB(t, nil, map[string]int{"/sets/71788-1/": c.status})
		if _, err := app.legoDB.Exec(`INSERT INTO ref_sets (set_num, name, year, theme, total_pieces) VALUES ('71788-1','Street Bike','2023','Ninjago Core','56')`); err != nil {
			t.Fatal(err)
		}
		startSet(t, app, "71788")
		if !strings.Contains(app.message, c.want) || !app.messageErr {
			t.Errorf("%s: message %q should mention %q", name, app.message, c.want)
		}
		// The old imported lookup table still fills the details in.
		if d := app.legoSetDraft; d.Manual || d.Name != "Street Bike" || d.Source != "legacy import" || d.Pieces != 56 {
			t.Errorf("%s: expected the local catalog fallback, got %+v", name, d)
		}
	}
}

func TestThemeLookupFailureStillFillsTheRest(t *testing.T) {
	app := newTestApp(t)
	app.rebrick = fakeRB(t, map[string]string{"/sets/75192-1/": falconRoutes["/sets/75192-1/"]}, map[string]int{"/themes/171/": http.StatusTooManyRequests})
	startSet(t, app, "75192")
	d := app.legoSetDraft
	if d.Name != "Millennium Falcon" || d.Pieces != 7541 || d.Theme != "" || d.Manual {
		t.Fatalf("draft = %+v", d)
	}
	if !strings.Contains(app.message, "Theme lookup failed") {
		t.Errorf("the theme problem should be reported: %q", app.message)
	}
	// The theme field is always editable, so it can be typed in.
	confirm(app, scrLegoSetConfirm, map[int]string{sfTheme: "Star Wars", sfQty: "1", sfConfirm: "yes"})
	if got, _ := app.legoDB.GetSetByNum("75192"); got == nil || got.Theme != "Star Wars" {
		t.Errorf("typed theme should be saved: %+v", got)
	}
}

func TestNoKeyAndNoCatalogPointsAtSettings(t *testing.T) {
	app := newTestApp(t)
	app.rebrick = &lego.Client{}
	startSet(t, app, "12345")
	if !app.legoSetDraft.Manual || !strings.Contains(app.message, "catalog refresh") {
		t.Errorf("expected manual entry with a hint about the offline catalog: %q", app.message)
	}
}

func TestAddSetRejectsBlankAndWorksWithoutDraft(t *testing.T) {
	app := newTestApp(t)
	app.cur = scrLegoHub
	app.goTo(scrLegoSetAdd)
	app.screens[scrLegoSetAdd].(*formScreen).submit(app, []string{"   "})
	if app.cur != scrLegoSetAdd || !app.messageErr {
		t.Errorf("a blank set number must stay put with an error (cur=%q)", app.cur)
	}
	// The confirm screens must survive being reached with no draft.
	app.legoSetDraft, app.partFlow = nil, nil
	for _, id := range []string{scrLegoSetConfirm, scrPartConfirm} {
		app.cur = scrLegoHub
		app.goTo(id)
		_ = app.View()
		app.screens[id].(*formScreen).submit(app, make([]string, 11))
	}
}

func TestReadOnlyRolesCannotAddOrUpdate(t *testing.T) {
	app := newTestApp(t)
	app.session = &auth.Session{Source: "modernwms", Username: "viewer", Role: "ViewOnly",
		Permissions: &wmsdb.Permissions{CanWrite: false}}
	for _, id := range []string{scrLegoSetAdd, scrLegoPartAdd} {
		app.cur, app.stack = scrLegoHub, nil
		app.goTo(id)
		if app.cur == id || !app.messageErr {
			t.Errorf("a read-only role must be bounced from %s (cur=%q msg=%q)", id, app.cur, app.message)
		}
	}
}

// The confirm screen asks one thing at a time, like the rest of the app:
// theme (prefilled), then how many, then the yes/no.
func TestConfirmPromptsAppearOneAtATime(t *testing.T) {
	app := newTestApp(t)
	app.rebrick = fakeRB(t, falconRoutes, nil)
	startSet(t, app, "75192")

	if out := plain(app.View()); strings.Contains(out, "How many do you own") || strings.Contains(out, "Save this?") {
		t.Fatalf("later prompts should not show yet:\n%s", out)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter}) // accept the theme
	if out := plain(app.View()); !strings.Contains(out, "How many do you own: 1") || strings.Contains(out, "Save this?") {
		t.Fatalf("the quantity prompt should appear next:\n%s", out)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "yes" {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter}) // submit
	if got, err := app.legoDB.GetSetByNum("75192"); err != nil || got.Qty != 3 {
		t.Fatalf("typing through the prompts should save 3 copies: %+v %v", got, err)
	}
}

// Regression: with an empty field focused, typing a capital L (the first
// letter of e.g. "Lloyd's Ninja Bike") used to fire the Lock shortcut and
// sign the user out mid-entry. Letter shortcuts other than Q are off on forms.
func TestTypingAShortcutLetterInAFormIsJustTyping(t *testing.T) {
	app := newTestApp(t)
	app.rebrick = fakeRB(t, nil, nil)
	startSet(t, app, "99999") // manual entry: Name is the first, empty, editable field

	for _, r := range "Lloyd's Ninja Bike" {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if !app.authed || app.cur != scrLegoSetConfirm {
		t.Fatalf("typing must not trigger shortcuts (authed=%v cur=%q)", app.authed, app.cur)
	}
	if got := app.screens[scrLegoSetConfirm].ActiveForm().Fields[sfName].Value; got != "Lloyd's Ninja Bike" {
		t.Errorf("the name should have been typed in full, got %q", got)
	}
	for _, r := range []rune{'u', 'g', '+'} {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if app.cur != scrLegoSetConfirm {
		t.Errorf("u/g/+ typed into a form must not navigate away, on %q", app.cur)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyF10}) // the F-key still locks
	if app.authed || app.cur != scrLogin {
		t.Error("F10 must still lock the session from a form")
	}
}

func ownedFor(num, name, cat string) lego.OwnedPart {
	return lego.OwnedPart{PartNum: num, Name: name, Category: cat, ColorID: 4, ColorName: "Red", Qty: 5}
}
