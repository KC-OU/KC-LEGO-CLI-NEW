package uiapp

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

const (
	falconImg = "https://cdn.rebrickable.com/media/sets/75192-1.jpg"
	brickImg  = "https://cdn.rebrickable.com/media/parts/ldraw/4/3001.png"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			if y < 10 {
				m.Set(x, y, color.NRGBA{200, 30, 30, 255})
			} else {
				m.Set(x, y, color.NRGBA{30, 30, 200, 255})
			}
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// detailApp is an offline app with a small catalog, a set with contents, and two cached pictures.
func detailApp(t *testing.T) *App {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	for _, q := range []string{
		`UPDATE cat_sets SET img_url = '` + falconImg + `' WHERE set_num = '75192-1'`,
		`INSERT INTO cat_minifigs (fig_num, name, num_parts, img_url) VALUES ('fig-1', 'Han Solo', 4, '')`,
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10)`,
		`INSERT INTO cat_inventory_minifigs (inventory_id, fig_num, quantity) VALUES (1,'fig-1',2)`,
	} {
		if _, err := app.legoDB.Exec(q); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}
	png := testPNG(t)
	if err := app.images.Seed(falconImg, png); err != nil {
		t.Fatal(err)
	}
	if err := app.images.Seed(brickImg, png); err != nil {
		t.Fatal(err)
	}
	return app
}

func openDetail(t *testing.T, app *App, input string) string {
	t.Helper()
	if app.height == 0 { // a roomy window so values do not wrap in assertions; the 80x25 case has its own test
		app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	}
	app.cur, app.stack = scrLegoHub, nil
	app.goTo(scrLegoDetailAsk)
	app.screens[scrLegoDetailAsk].(*formScreen).submit(app, []string{input})
	if app.cur != scrLegoDetail {
		t.Fatalf("%q: expected the detail page, on %q (msg %q)", input, app.cur, app.message)
	}
	return plain(app.View())
}

func TestPartDetailShowsPictureFactsHoldingsAndUse(t *testing.T) {
	app := detailApp(t)
	_ = app.legoDB.AddOwnedPart(ownedFor("3001", "Brick 2 x 4", "Bricks"))
	_ = app.legoDB.SetMinQty("3001", 4, "Red", 9)
	out := openDetail(t, app, "3001")
	for _, want := range []string{"▀", "Brick 2 x 4", "Bricks", "offline catalog", "Blue, Red", "Red 5 LOW", "1 set(s), e.g. Millennium Falcon (2017)", "not fetched — press P"} {
		if !strings.Contains(out, want) {
			t.Errorf("part detail lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "A add / update this part") {
		t.Errorf("the keys are shown:\n%s", out)
	}
}

func TestSetDetailShowsThemeMinifigsAndHowMuchYouCanBuild(t *testing.T) {
	app := detailApp(t)
	_ = app.legoDB.AddOwnedPart(ownedFor("3001", "Brick 2 x 4", "Bricks")) // 5 of 10
	out := openDetail(t, app, "75192")
	for _, want := range []string{"▀", "Millennium Falcon", "Star Wars - Ultimate Collector Series", "7541", "not in your collection", "Han Solo x2", "50% of this set's pieces (5 of 10)"} {
		if !strings.Contains(out, want) {
			t.Errorf("set detail lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "M missing") || !strings.Contains(out, "K check") {
		t.Errorf("set keys:\n%s", out)
	}
}

func TestDetailNumberResolution(t *testing.T) {
	app := detailApp(t)
	for input, want := range map[string]detailReq{
		"3001":      {Kind: "part", Num: "3001", ColorID: -1},
		"part 3001": {Kind: "part", Num: "3001", ColorID: -1},
		"75192-1":   {Kind: "set", Num: "75192-1", ColorID: -1},
		"75192":     {Kind: "set", Num: "75192", ColorID: -1},
		"set 75192": {Kind: "set", Num: "75192", ColorID: -1},
		"300121":    {Kind: "part", Num: "3001", ColorID: 4}, // an element ID
	} {
		got, msg := resolveDetail(app, input)
		if got == nil || *got != want {
			t.Errorf("%q -> %+v %q, want %+v", input, got, msg, want)
		}
	}
	for _, bad := range []string{"", "   ", "zzz-9", "set 00000", "0000"} {
		if got, msg := resolveDetail(app, bad); got != nil || msg == "" {
			t.Errorf("%q must be refused with a message, got %+v", bad, got)
		}
	}
	// an element ID opens that colour's page
	out := openDetail(t, app, "300121")
	if !strings.Contains(out, "Colour") || !strings.Contains(out, "Red") {
		t.Errorf("element page names the colour:\n%s", out)
	}
}

func TestDetailWithoutAPictureSaysSoAndStillWorks(t *testing.T) {
	app := detailApp(t)
	app.images.Dir = t.TempDir() // an empty cache, offline
	out := openDetail(t, app, "75192")
	if strings.Contains(out, "▀") || !strings.Contains(out, "(no picture: none cached") || !strings.Contains(out, "Millennium Falcon") {
		t.Errorf("no picture, but the facts stay:\n%s", out)
	}
}

func TestPictureModesAndMono(t *testing.T) {
	app := detailApp(t)
	t.Setenv(config.TUIImages, "off")
	if out := openDetail(t, app, "75192"); strings.Contains(out, "▀") || strings.Contains(out, "no picture") {
		t.Errorf("off shows neither picture nor caption:\n%s", out)
	}
	t.Setenv(config.TUIImages, "ascii")
	out := openDetail(t, app, "75192")
	if strings.Contains(out, "▀") {
		t.Errorf("ascii mode draws no half-blocks:\n%s", out)
	}
	t.Setenv(config.TUIImages, "")
	t.Setenv("NO_COLOR", "1")
	app.theme.Mono = true
	if out := openDetail(t, app, "75192"); strings.Contains(out, "▀") {
		t.Errorf("mono (NO_COLOR) uses ASCII, since half-blocks would be solid bars:\n%s", out)
	}
}

func TestDetailKeysAddMissingAndPrice(t *testing.T) {
	app := detailApp(t)
	fakeBrickLink(t, map[string]string{
		"/items/PART/3001/price":   ok(`{"new_or_used":"U","currency_code":"GBP","avg_price":"0.0587","unit_quantity":10,"total_quantity":900}`),
		"/items/SET/75192-1/price": ok(`{"new_or_used":"U","currency_code":"GBP","avg_price":"0","unit_quantity":0,"total_quantity":0}`),
	})
	// P without credentials explains itself
	openDetail(t, app, "3001")
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if !app.messageErr || !strings.Contains(app.message, "not set up") {
		t.Errorf("P without BrickLink: %q", app.message)
	}
	for k, v := range map[string]string{config.BricklinkConsumerKey: "k", config.BricklinkConsumerSecret: "s", config.BricklinkToken: "t", config.BricklinkTokenSecret: "ts"} {
		config.SetOverride(k, v)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if app.messageErr || !strings.Contains(app.message, "0.0587 GBP") {
		t.Fatalf("P with BrickLink: %q err=%v", app.message, app.messageErr)
	}
	if out := plain(app.View()); !strings.Contains(out, "0.0587 GBP avg sold, used") {
		t.Errorf("the page shows the stored price after P:\n%s", out)
	}
	// a set with no sales says so and is remembered as unpriced, not free
	openDetail(t, app, "75192")
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if !app.messageErr || !strings.Contains(app.message, "no sales") {
		t.Errorf("no sales: %q", app.message)
	}
	// A starts the add flow for this part, M shows missing parts for a set
	openDetail(t, app, "3001")
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if app.cur != scrPick || app.partFlow == nil || app.partFlow.Num != "3001" {
		t.Errorf("A should start the part flow: cur=%q", app.cur)
	}
	openDetail(t, app, "75192")
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if app.cur != scrLegoMissing || !strings.Contains(plain(app.View()), "offline catalog") {
		t.Errorf("M should show the missing parts: cur=%q", app.cur)
	}
	openDetail(t, app, "75192")
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if app.cur != scrLegoSetConfirm {
		t.Errorf("A on a set should start adding it: cur=%q", app.cur)
	}
}

func TestDetailIsReachableFromTheHubAndResultsAndSurvivesNoState(t *testing.T) {
	app := detailApp(t)
	app.cur, app.stack = scrHub, nil
	app.goTo(scrLegoHub)
	if out := plain(app.View()); !strings.Contains(out, "D.") && !strings.Contains(out, "Part / Set Detail") {
		t.Errorf("the hub lists the detail page:\n%s", out)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if app.cur != scrLegoDetailAsk {
		t.Errorf("D on the hub: %q", app.cur)
	}
	app.cur, app.stack = scrLegoHub, nil
	app.legoSearchTerm = "brick"
	app.goTo(scrLegoPartResults)
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if app.cur != scrLegoDetailAsk {
		t.Errorf("D on part results: %q", app.cur)
	}
	app.detail = nil
	app.cur = scrLegoHub
	app.goTo(scrLegoDetail)
	_ = app.View() // no request: must not panic
}

func TestDetailPagesFitAnEightyByTwentyFiveTelnetWindow(t *testing.T) {
	for _, input := range []string{"75192", "3001"} {
		app := detailApp(t)
		app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
		out := openDetail(t, app, input)
		lines := strings.Split(out, "\n")
		if len(lines) > 23 { // leaves room for the message and prompt lines
			t.Errorf("%s: %d lines will not fit a 25-row window:\n%s", input, len(lines), out)
		}
		for _, l := range lines {
			if n := len([]rune(l)); n > 80 {
				t.Errorf("%s: a line is %d columns wide: %q", input, n, l)
			}
		}
		if !strings.Contains(out, "Q back") {
			t.Errorf("%s: the key hints must be on screen:\n%s", input, out)
		}
	}
}

func TestAPartNumberWithADashIsStillFoundAsAPart(t *testing.T) {
	app := detailApp(t)
	if _, err := app.legoDB.Exec(`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('x-01', 'Dashed part', 11)`); err != nil {
		t.Fatal(err)
	}
	got, msg := resolveDetail(app, "x-01")
	if got == nil || got.Kind != "part" || got.Num != "x-01" {
		t.Fatalf("a dash must not force 'set': %+v %q", got, msg)
	}
	if got, _ := resolveDetail(app, "75192-1"); got == nil || got.Kind != "set" {
		t.Errorf("but a real set still is one: %+v", got)
	}
	if got, msg := resolveDetail(app, "set x-01"); got != nil || !strings.Contains(msg, `No set "x-01"`) {
		t.Errorf("an explicit 'set' is never a part: %+v %q", got, msg)
	}
	if got, _ := resolveDetail(app, "part 75192"); got != nil && got.Kind == "set" {
		t.Errorf("an explicit 'part' is never a set: %+v", got)
	}
}

func TestDetailPageWithVeryLongValuesStillFitsTheWindowHeight(t *testing.T) {
	long := strings.Repeat("Some very long minifigure name, ", 20)
	for _, h := range []int{24, 25} {
		app := detailApp(t)
		app.Update(tea.WindowSizeMsg{Width: 80, Height: h})
		openDetail(t, app, "75192")
		s := app.screens[scrLegoDetail].(*detailScreen)
		s.page.Rows = [][]string{{"Set #", "75192-1"}, {"Name", "Millennium Falcon"}, {"Theme", long}, {"Minifigures", long},
			{"Your loose parts", long}, {"BrickLink price", long}, {"Used in", long}}
		lines := strings.Split(strings.TrimRight(plain(app.View()), "\n"), "\n")
		if len(lines) > h || !strings.Contains(lines[0], "KCPARTS") || !strings.Contains(lines[len(lines)-1], "Q back") {
			t.Errorf("height %d: %d lines, header %q, last %q", h, len(lines), lines[0], lines[len(lines)-1])
		}
	}
}
