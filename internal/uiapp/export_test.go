package uiapp

import (
	"archive/zip"
	"encoding/json"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
)

func press(app *App, k string) { app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}) }

func TestSetDetailExportsJSONWithPictureAndParts(t *testing.T) {
	app := detailApp(t)
	t.Setenv(config.PublicURL, "https://lego-tui.example.com/")
	openDetail(t, app, "75192-1")
	press(app, "x")
	if app.cur != scrExport {
		t.Fatalf("X should open the export screen, on %q", app.cur)
	}
	press(app, "j")
	if app.exportRes == nil {
		t.Fatalf("nothing exported: %q", app.message)
	}
	var out struct {
		Picture struct{ URL, MIME, Base64 string }
		Facts   []struct{ Field, Value string }
		Parts   []struct {
			Part  string
			Image struct{ URL, Base64 string }
		}
	}
	b, err := os.ReadFile(app.exportRes.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Picture.URL != falconImg || out.Picture.MIME != "image/png" || out.Picture.Base64 == "" {
		t.Errorf("set picture = %+v", out.Picture)
	}
	if len(out.Parts) != 1 || out.Parts[0].Part != "3001" || out.Parts[0].Image.URL != brickImg || out.Parts[0].Image.Base64 == "" {
		t.Errorf("parts = %+v", out.Parts)
	}
	if len(out.Facts) == 0 {
		t.Error("facts missing")
	}
	if !strings.HasPrefix(app.exportRes.URL, "HTTPS://LEGO-TUI.EXAMPLE.COM/DL/") {
		t.Errorf("link = %q", app.exportRes.URL)
	}
	view := app.View()
	if !strings.Contains(strings.ReplaceAll(plain(view), "\n", ""), app.exportRes.URL[:30]) || !strings.Contains(view, "█") {
		t.Errorf("result screen should show the link and a QR code:\n%s", plain(view))
	}
	tok := app.exportRes.URL[strings.LastIndex(app.exportRes.URL, "/")+1:]
	if f, _, err := exports.Claim(exports.Dir(), tok); err != nil || f != app.exportRes.Path {
		t.Errorf("link does not claim the file: %v", err)
	}
}

func TestMissingPartsExportXLSX(t *testing.T) {
	app := detailApp(t)
	openDetail(t, app, "75192-1")
	press(app, "m")
	if app.cur != scrLegoMissing {
		t.Fatalf("on %q", app.cur)
	}
	press(app, "x")
	press(app, "e")
	if app.exportRes == nil {
		t.Fatalf("nothing exported: %q", app.message)
	}
	if !strings.HasSuffix(app.exportRes.Path, ".xlsx") || app.exportRes.URL != "" {
		t.Fatalf("result = %+v", app.exportRes)
	}
	zr, err := zip.OpenReader(app.exportRes.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) < 6 {
		t.Errorf("workbook has %d parts", len(zr.File))
	}
	if !strings.Contains(plain(app.View()), "WMS_PUBLIC_URL") {
		t.Error("without a public URL the screen should say how to get links")
	}
}
