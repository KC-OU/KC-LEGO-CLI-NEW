package uiapp

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// The documentation's screenshots are real screens, rendered from a small demo collection and
// converted to HTML, so they can never drift from the app. `make docs-generate` writes them to
// docs/assets/screens (set WMS_DOCS_OUT to choose another folder). This test also runs in the
// normal suite (into a temp folder) so a screen that panics or renders empty is caught.

// brickPNG draws a simple stylised brick, so the demo pictures look like something.
func brickPNG(t *testing.T, body color.NRGBA) []byte {
	t.Helper()
	const w, h = 160, 100
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	shade := func(c color.NRGBA, f float64) color.NRGBA {
		return color.NRGBA{uint8(float64(c.R) * f), uint8(float64(c.G) * f), uint8(float64(c.B) * f), 255}
	}
	for y := 34; y < 92; y++ { // body
		for x := 14; x < 146; x++ {
			f := 1.0
			if y > 84 || x > 138 {
				f = 0.72
			} else if y < 38 || x < 20 {
				f = 1.12
			}
			m.Set(x, y, shade(body, math.Min(f, 1.2)))
		}
	}
	for i := 0; i < 4; i++ { // studs
		cx, cy := 32+i*32, 26
		for y := cy - 12; y <= cy+12; y++ {
			for x := cx - 13; x <= cx+13; x++ {
				dx, dy := float64(x-cx)/13, float64(y-cy)/11
				if dx*dx+dy*dy <= 1 {
					f := 1.05
					if dy > 0.3 {
						f = 0.8
					}
					m.Set(x, y, shade(body, f))
				}
			}
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

var dateRE = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)

func writeScreen(t *testing.T, dirs []string, name, ansi string) {
	t.Helper()
	body := ui.ANSIToHTML(dateRE.ReplaceAllString(ansi, "2026-09-20 12:00:00"))
	page := `<pre class="term" aria-label="Terminal screen: ` + name + `">` + body + "</pre>\n"
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, name+".html"), []byte(page), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDocScreensRender(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)

	dirs := []string{t.TempDir()}
	if d := os.Getenv("WMS_DOCS_OUT"); d != "" {
		dirs = append(dirs, d)
	}
	app := detailApp(t)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	db := app.legoDB
	// a demo collection
	for _, q := range []string{
		`INSERT INTO cat_colors (id, name, rgb, is_trans) VALUES (0,'Black','05131D',0),(15,'White','FFFFFF',0),(14,'Yellow','F2CD37',0)`,
		`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('3023','Plate 1 x 2',11),('3004','Brick 1 x 2',11)`,
		`INSERT INTO cat_elements (part_num, color_id) VALUES ('3023',1),('3004',4),('3004',15)`,
		`INSERT INTO fts_parts(fts_parts) VALUES('rebuild')`,
		`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts) VALUES ('10497-1','Galileo Space Telescope',2023,158,1200),('75257-1','Millennium Falcon',2019,171,1351)`,
		`INSERT INTO fts_sets(fts_sets) VALUES('rebuild')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}
	red, blue := color.NRGBA{201, 26, 9, 255}, color.NRGBA{0, 85, 191, 255}
	if err := app.images.Seed(falconImg, brickPNG(t, blue)); err != nil {
		t.Fatal(err)
	}
	if err := app.images.Seed(brickImg, brickPNG(t, red)); err != nil {
		t.Fatal(err)
	}
	for _, p := range []lego.OwnedPart{
		{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 24, MinQty: 50},
		{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 1, ColorName: "Blue", Qty: 120},
		{PartNum: "3023", Name: "Plate 1 x 2", Category: "Plates", ColorID: 1, ColorName: "Blue", Qty: 300},
		{PartNum: "3004", Name: "Brick 1 x 2", Category: "Bricks", ColorID: 15, ColorName: "White", Qty: 80},
	} {
		if err := db.AddOwnedPart(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetMinQty("3001", 4, "Red", 50); err != nil {
		t.Fatal(err)
	}
	_ = db.UpsertSet(lego.Set{SetNum: "10497", Name: "Galileo Space Telescope", Theme: "Icons", Year: 2023, Qty: 1, PartsQty: 1200})
	_ = db.AddRecent("3001")
	_ = db.AddRecent("3023")

	shot := func(name string) {
		t.Helper()
		v := app.View()
		if len(strings.TrimSpace(plain(v))) < 40 {
			t.Fatalf("screen %s rendered empty", name)
		}
		writeScreen(t, dirs, name, v)
	}
	home := func() { app.cur, app.stack, app.message = scrHub, nil, "" }

	// sign-on (a fresh session)
	fresh, _ := newTestEnv(t)
	fresh.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	fresh.authed, fresh.session, fresh.cur = false, nil, scrLogin
	fresh.screens[scrLogin].OnEnter(fresh)
	fresh.SetTransport("telnet", "192.168.1.20")
	writeScreen(t, dirs, "signon", fresh.View())

	app.enterHub()
	shot("hub")
	home()
	app.goTo(scrLegoHub)
	shot("lego-hub")
	home()
	app.goTo(scrLegoStats)
	shot("stats")
	home()
	app.legoSearchTerm = "falcon"
	app.goTo(scrLegoSetFound)
	shot("set-search")
	home()
	openDetail(t, app, "75192")
	shot("set-detail")
	home()
	openDetail(t, app, "3001")
	shot("part-detail")
	home()
	app.legoMissing = nil
	app.goTo(scrLegoMissingAsk)
	app.screens[scrLegoMissingAsk].(*formScreen).submit(app, []string{"75192", ""})
	shot("missing")
	home()
	app.goTo(scrLegoBuild)
	shot("build")
	home()
	app.goTo(scrLegoPartAdd)
	shot("add-part")
	app.screens[scrLegoPartAdd].(*formScreen).submit(app, []string{"3001"})
	shot("add-part-colour")
	pickSubmit(app, "Red")
	shot("add-part-confirm")
	home()
	app.goTo(scrLegoHistory)
	shot("history")
	home()
	app.goTo(scrWorkshop)
	shot("workshop")
	home()
	app.goTo(scrReports)
	shot("reports")
	home()
	app.openPalette()
	app.paletteInput = "fal"
	shot("palette")
	app.paletteOpen = false
	app.helpOpen = true
	shot("help")

	files, _ := filepath.Glob(filepath.Join(dirs[0], "*.html"))
	if len(files) < 16 {
		t.Errorf("expected 16 screens, wrote %d", len(files))
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if !strings.Contains(string(b), `class="term"`) || strings.Contains(string(b), "\x1b") {
			t.Errorf("%s is not clean HTML", f)
		}
	}
}
