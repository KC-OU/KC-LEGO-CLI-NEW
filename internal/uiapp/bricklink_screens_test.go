package uiapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func fakeBrickLink(t *testing.T, routes map[string]string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "OAuth ") {
			w.WriteHeader(401)
			w.Write([]byte(`{"meta":{"message":"BAD_OAUTH_REQUEST","code":401}}`))
			return
		}
		if body, ok := routes[r.URL.Path]; ok {
			w.Write([]byte(body))
			return
		}
		w.WriteHeader(404)
		w.Write([]byte(`{"meta":{"message":"RESOURCE_NOT_FOUND","code":404}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("BRICKLINK_BASE_URL", srv.URL)
}

var uiBLRoutes = map[string]string{
	"/colors":                  ok(`[{"color_id":5,"color_name":"Red"}]`),
	"/items/PART/3001":         ok(`{"no":"3001","name":"Brick 2 x 4","type":"PART","year_released":1958}`),
	"/items/PART/3001/price":   ok(`{"new_or_used":"U","currency_code":"GBP","min_price":"0.01","max_price":"0.3","avg_price":"0.0587","unit_quantity":10,"total_quantity":900}`),
	"/items/SET/75192-1":       ok(`{"no":"75192-1","name":"Millennium Falcon","type":"SET","year_released":2017,"is_obsolete":false}`),
	"/items/SET/75192-1/price": ok(`{"new_or_used":"U","currency_code":"GBP","avg_price":"0","unit_quantity":0,"total_quantity":0}`),
}

func ok(data string) string { return `{"meta":{"code":200,"message":"OK"},"data":` + data + `}` }

func TestBrickLinkSettingsSaveMaskTestAndAudit(t *testing.T) {
	app := newTestApp(t)
	fakeBrickLink(t, uiBLRoutes)
	app.cur, app.stack = scrHub, nil
	app.goTo(scrSettingsHub)
	if !strings.Contains(plain(app.View()), "BrickLink API") {
		t.Fatal("the settings menu lists BrickLink")
	}
	app.goTo(scrSettingsBrickLink)
	if out := plain(app.View()); !strings.Contains(out, "(not set)") || !strings.Contains(out, "BY NUMBER") {
		t.Errorf("the screen explains itself:\n%s", out)
	}
	f := app.screens[scrSettingsBrickLink].(*formScreen)
	for _, fld := range f.fl.Fields[:4] {
		if !fld.Password {
			t.Errorf("%q must be masked as it is typed", fld.Label)
		}
	}
	f.submit(app, []string{"CONSUMERKEY123", "consumersecret456", "TOKENVALUE789", "tokensecret000", "gbp"})
	if app.messageErr || !strings.Contains(app.message, "accepted") {
		t.Fatalf("saving valid values: msg=%q err=%v", app.message, app.messageErr)
	}
	if config.Get(config.BricklinkToken) != "TOKENVALUE789" || config.Get(config.BricklinkCurrency) != "GBP" {
		t.Errorf("values were not stored")
	}
	if a := auditText(t, app); !strings.Contains(a, "bricklink_api") || strings.Contains(a, "CONSUMERKEY123") || strings.Contains(a, "tokensecret000") {
		t.Errorf("the change is audited without the values:\n%s", a)
	}
	// the screen shows only the last four characters afterwards
	app.cur, app.stack = scrHub, nil
	app.goTo(scrSettingsBrickLink)
	out := plain(app.View())
	if !strings.Contains(out, "Y123") || strings.Contains(out, "CONSUMERKEY123") || strings.Contains(out, "tokensecret000") {
		t.Errorf("secrets must be masked:\n%s", out)
	}
	f = app.screens[scrSettingsBrickLink].(*formScreen)
	f.submit(app, []string{"", "", "", "", "pounds"})
	if !app.messageErr || !strings.Contains(app.message, "three-letter") {
		t.Errorf("a bad currency is refused: %q", app.message)
	}
}

func TestBrickLinkSettingsReportABadTokenButKeepWhatWasSaved(t *testing.T) {
	app := newTestApp(t)
	fakeBrickLink(t, map[string]string{"/colors": `{"meta":{"message":"TOKEN_IP_MISMATCHED","code":401}}`})
	app.cur = scrHub
	app.goTo(scrSettingsBrickLink)
	app.screens[scrSettingsBrickLink].(*formScreen).submit(app, []string{"k", "s", "t", "ts", ""})
	if !app.messageErr || !strings.Contains(app.message, "different IP") {
		t.Errorf("msg=%q", app.message)
	}
	if config.Get(config.BricklinkToken) != "t" {
		t.Error("values are kept even when the test fails, so a typo can be corrected")
	}
	app.screens[scrSettingsBrickLink].(*formScreen).submit(app, []string{"", "", "", "", ""})
	if !app.messageErr {
		t.Error("a blank submit must re-test and still report the failure")
	}
}

func TestBrickLinkSettingsAreAdminOnly(t *testing.T) {
	app := newTestApp(t)
	app.session.Permissions.IsAdmin = false
	app.cur, app.stack = scrHub, nil
	app.goTo(scrSettingsHub)
	if app.cur == scrSettingsHub || !app.messageErr {
		t.Errorf("a non-admin must be bounced from settings (cur=%q)", app.cur)
	}
}

func TestBrickLinkLookupByNumberShowsItemAndPrice(t *testing.T) {
	app := newTestApp(t)
	fakeBrickLink(t, uiBLRoutes)
	for k, v := range map[string]string{config.BricklinkConsumerKey: "k", config.BricklinkConsumerSecret: "s", config.BricklinkToken: "t", config.BricklinkTokenSecret: "ts"} {
		config.SetOverride(k, v)
	}
	ask := func(kind, no, colour string) {
		app.cur, app.stack = scrLegoHub, nil
		app.goTo(scrLegoBLAsk)
		app.screens[scrLegoBLAsk].(*formScreen).submit(app, []string{kind, no, colour})
	}
	ask("part", "3001", "")
	out := plain(app.View())
	if app.cur != scrLegoBLResult || !strings.Contains(out, "Brick 2 x 4") || !strings.Contains(out, "0.0587 GBP (used)") || !strings.Contains(out, "900 piece(s)") {
		t.Fatalf("part lookup (cur=%q msg=%q):\n%s", app.cur, app.message, out)
	}
	ask("set", "75192", "")
	if out := plain(app.View()); !strings.Contains(out, "Millennium Falcon") || !strings.Contains(out, "not free: no data") {
		t.Errorf("a set with no sales must say so rather than show 0:\n%s", out)
	}
	ask("part", "0000", "")
	if app.cur != scrLegoBLAsk || !strings.Contains(app.message, `no part "0000"`) {
		t.Errorf("unknown item: cur=%q msg=%q", app.cur, app.message)
	}
	ask("gadget", "1", "")
	if !app.messageErr {
		t.Error("a bad type is refused")
	}
	ask("part", "3001", "red") // no colour catalog / mapping yet
	if !app.messageErr || !strings.Contains(app.message, "No colour") {
		t.Errorf("colour without a catalog: %q", app.message)
	}
}

func TestBrickLinkLookupWithoutCredentialsExplains(t *testing.T) {
	app := newTestApp(t)
	app.cur = scrLegoHub
	app.goTo(scrLegoBLAsk)
	app.screens[scrLegoBLAsk].(*formScreen).submit(app, []string{"part", "3001", ""})
	if !app.messageErr || !strings.Contains(app.message, "not set up") {
		t.Errorf("msg=%q", app.message)
	}
}
