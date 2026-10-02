package uiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/labels"
)

func findByExt(t *testing.T, dir, ext string) string {
	t.Helper()
	var found string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(path) == ext {
			found = path
		}
		return nil
	})
	return found
}

func labelSizeIndex(t *testing.T, id string) int {
	t.Helper()
	for i, s := range labels.Sizes {
		if s.ID == id {
			return i
		}
	}
	t.Fatalf("no label size %q", id)
	return -1
}

// TestLabelsScreenSavesTheNewFormatsForOneNonSheetSet covers the labels.go
// addition: PNG/ZPL/.lbx are only offered for exactly one non-sheet-stock
// set — the one-label-at-a-time formats (see their own doc comments in
// internal/labels).
func TestLabelsScreenSavesTheNewFormatsForOneNonSheetSet(t *testing.T) {
	app := newTestApp(t)
	startLabels(app, []string{"75192-1"})
	typeKeys(app, "1") // "4x6", not a sheet size
	typeKeys(app, "5") // format: all of the above

	if app.exportRes == nil {
		t.Fatalf("no export result: %s", app.message)
	}
	joined := strings.Join(app.exportRes.Warnings, " | ")
	for _, want := range []string{"PNG image", "Zebra ZPL", "Brother .lbx"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings = %q, want it to mention %q", joined, want)
		}
	}

	dir := exports.Dir()
	for _, ext := range []string{".png", ".zpl", ".lbx"} {
		path := findByExt(t, dir, ext)
		if path == "" {
			t.Fatalf("no %s file found under %s", ext, dir)
		}
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 {
			t.Errorf("%s: %v (len %d)", path, err, len(b))
		}
	}
}

// TestLabelsScreenAsksWhichFormatForOneNonSheetSet covers the actual ask:
// an admin wanted to be asked which format(s) they want, not have
// PNG/ZPL/.lbx generated silently every time.
func TestLabelsScreenAsksWhichFormatForOneNonSheetSet(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	startLabels(app, []string{"75192-1"})
	typeKeys(app, "1") // "4x6", not a sheet size

	if app.exportRes != nil {
		t.Fatal("picking the size alone must not save anything yet — a format choice comes first")
	}
	body := plain(app.View())
	if !strings.Contains(body, "Format for") || !strings.Contains(body, "Brother .lbx") {
		t.Errorf("expected the format picker, got:\n%s", body)
	}

	typeKeys(app, "1") // format: PDF + web page only
	if app.exportRes == nil {
		t.Fatalf("no export result: %s", app.message)
	}
	joined := strings.Join(app.exportRes.Warnings, " | ")
	for _, dontWant := range []string{"PNG image", "Zebra ZPL", "Brother .lbx"} {
		if strings.Contains(joined, dontWant) {
			t.Errorf("choosing PDF-only must not also save extra formats, got %q", joined)
		}
	}
	if findByExt(t, exports.Dir(), ".png") != "" {
		t.Error("no PNG file should exist after a PDF-only choice")
	}
}

// TestLabelsScreenSkipsTheNewFormatsForSheetStock covers the exclusion:
// requesting a4/letter (genuinely multiple physical pages) must not try to
// produce a single PNG/ZPL/.lbx.
func TestLabelsScreenSkipsTheNewFormatsForSheetStock(t *testing.T) {
	app := newTestApp(t)
	startLabels(app, []string{"75192-1"})
	i := labelSizeIndex(t, "a4")
	typeKeys(app, string(rune('1'+i)))

	if app.exportRes == nil {
		t.Fatalf("no export result: %s", app.message)
	}
	joined := strings.Join(app.exportRes.Warnings, " | ")
	for _, dontWant := range []string{"PNG image", "Zebra ZPL", "Brother .lbx"} {
		if strings.Contains(joined, dontWant) {
			t.Errorf("sheet stock must not mention %q, got %q", dontWant, joined)
		}
	}
}
