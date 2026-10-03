package uiapp

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
)

func TestMyExportsDeleteRemovesTheFileAndListing(t *testing.T) {
	app := newTestApp(t)
	app.session = &auth.Session{Username: "alex"}

	path, err := exports.Save(exports.Dir(), "alex", "report", "", "csv", []byte("a,b\n1,2\n"))
	if err != nil {
		t.Fatal(err)
	}

	scr := myExportsScreen().(*selectList)
	_, rows, keys := scr.rows(app)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1 export before deleting", rows)
	}

	scr.keys(app, keys[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the export file should be gone, stat err = %v", err)
	}
	_, rows, _ = scr.rows(app)
	if len(rows) != 0 {
		t.Fatalf("rows = %+v, want none left after deleting the only export", rows)
	}
}

func TestMyExportsEnterStillRegeneratesALinkNotDelete(t *testing.T) {
	app := newTestApp(t)
	app.session = &auth.Session{Username: "alex"}
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:alex"] = &access.User{Groups: []string{"admin"}}
	})
	app.loadPolicy()
	app.pUser = app.pol().Users["partdb:alex"]

	path, err := exports.Save(exports.Dir(), "alex", "report", "", "csv", []byte("a,b\n1,2\n"))
	if err != nil {
		t.Fatal(err)
	}

	scr := myExportsScreen().(*selectList)
	_, _, keys := scr.rows(app)
	scr.keys(app, keys[0], tea.KeyMsg{Type: tea.KeyEnter})

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Enter must not delete the file, stat err = %v", err)
	}
}
