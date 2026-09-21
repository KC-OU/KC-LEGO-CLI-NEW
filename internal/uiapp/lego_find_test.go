package uiapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// fakeRebrickable serves one canned /sets/ page and records what it was asked.
func fakeRebrickable(t *testing.T, status int, body string, gotAuth, gotQuery *string) *lego.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotAuth != nil {
			*gotAuth = r.Header.Get("Authorization")
		}
		if gotQuery != nil {
			*gotQuery = r.URL.Query().Get("search")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &lego.Client{APIKey: "testkey", BaseURL: srv.URL, HTTP: srv.Client()}
}

const falconJSON = `{"results":[
  {"set_num":"75192-1","name":"Millennium Falcon","year":2017,"num_parts":7541},
  {"set_num":"7965-1","name":"Millennium Falcon","year":2011,"num_parts":1254}]}`

func searchSets(app *App, term string) string {
	app.cur = scrLegoHub
	app.legoSearchTerm = term
	app.goTo(scrLegoSetFound)
	return plain(app.View())
}

func TestLegoSetSearchUsesRebrickable(t *testing.T) {
	app := newTestApp(t)
	var auth, query string
	app.rebrick = fakeRebrickable(t, 200, falconJSON, &auth, &query)

	out := searchSets(app, "falcon")
	for _, want := range []string{"75192-1", "7965-1", "Millennium Falcon", "7541", "2017", "2 result(s) (live — Rebrickable)", "A add"} {
		if !strings.Contains(out, want) {
			t.Errorf("results missing %q:\n%s", want, out)
		}
	}
	if auth != "key testkey" || query != "falcon" {
		t.Errorf("Rebrickable was asked with Authorization=%q search=%q, want %q / %q", auth, query, "key testkey", "falcon")
	}
	if len(app.legoFound) != 2 {
		t.Errorf("results must be kept for the add step, got %d", len(app.legoFound))
	}
}

func TestLegoSetSearchFallsBackToLocalCatalogAndSaysSo(t *testing.T) {
	app := newTestApp(t)
	app.rebrick = fakeRebrickable(t, 500, "boom", nil, nil)
	if _, err := app.legoDB.Exec(`INSERT INTO ref_sets (set_num, name, year, theme, total_pieces) VALUES ('71788-1','Local Ninja Set','2021','Ninjago','230')`); err != nil {
		t.Fatalf("seeding the local catalog: %v", err)
	}

	out := searchSets(app, "ninja")
	if !strings.Contains(out, "Local Ninja Set") || !strings.Contains(out, "(local — imported lookup file)") {
		t.Errorf("expected the local catalog result:\n%s", out)
	}
	if !app.messageErr || !strings.Contains(app.message, "unreachable") {
		t.Errorf("a failed live lookup must be reported, not hidden; message=%q", app.message)
	}
}

func TestLegoSetSearchWithoutKeyOrCatalogPointsAtSettings(t *testing.T) {
	app := newTestApp(t)
	app.rebrick = &lego.Client{} // no key
	out := searchSets(app, "anything")
	if !strings.Contains(out, "0 result(s)") || !strings.Contains(app.message, "Admin > Settings & API Keys") {
		t.Errorf("expected a hint to add the API key; message=%q\n%s", app.message, out)
	}
}

func TestLegoSetSearchBlankTermRejected(t *testing.T) {
	app := newTestApp(t)
	app.cur = scrLegoHub
	app.goTo(scrLegoSetFind)
	app.screens[scrLegoSetFind].(*formScreen).submit(app, []string{"  "})
	if app.cur != scrLegoSetFind || !app.messageErr {
		t.Errorf("a blank search must stay on the form with an error (cur=%q msg=%q)", app.cur, app.message)
	}
}

// Adding a found set goes through the same lookup + confirm flow as typing a number.
func TestLegoFoundSetAddOpensConfirmScreen(t *testing.T) {
	app := newTestApp(t)
	app.rebrick = fakeRB(t, map[string]string{
		"/sets/75192-1/": `{"set_num":"75192-1","name":"Millennium Falcon","year":2017,"theme_id":171,"num_parts":7541}`,
		"/themes/171/":   `{"id":171,"parent_id":null,"name":"Ultimate Collector Series"}`,
		"/sets/":         falconJSON,
	}, nil)
	searchSets(app, "falcon")
	app.goTo(scrLegoSetFoundAdd)
	app.screens[scrLegoSetFoundAdd].(*formScreen).submit(app, []string{"75192-1"})

	if app.cur != scrLegoSetConfirm || app.legoSetDraft == nil || app.legoSetDraft.Num != "75192" || app.legoSetDraft.Pieces != 7541 {
		t.Fatalf("expected the confirm screen with the looked-up set (cur=%q draft=%+v)", app.cur, app.legoSetDraft)
	}
}

func TestLegoHubListsSetSearch(t *testing.T) {
	app := newTestApp(t)
	app.cur = scrLegoHub
	out := plain(app.View())
	for _, want := range []string{"2. Search Sets (offline catalog first)", "3. Search Parts (offline catalog first)", "6. List Owned Parts"} {
		if !strings.Contains(out, want) {
			t.Errorf("LEGO hub missing %q:\n%s", want, out)
		}
	}
}

// Lock lives on F10 because the user's keyboard only has F1-F12; F19 keeps
// working for keyboards that do have it.
func TestLockOnF10AndF19(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyF10, tea.KeyF19} {
		app := newTestApp(t)
		app.cur = scrHub
		app.Update(tea.KeyMsg{Type: key})
		if app.session != nil || app.authed || app.cur != scrLogin {
			t.Errorf("%v should lock the session (session=%v authed=%v cur=%q)", key, app.session, app.authed, app.cur)
		}
	}
	legend := plain(ui.ClassicLegend(ui.New(), ui.DefaultFKeys))
	if !strings.Contains(legend, "F10=Lock") || strings.Contains(legend, "F19") {
		t.Errorf("legend should advertise F10=Lock and no F19: %q", legend)
	}
}
