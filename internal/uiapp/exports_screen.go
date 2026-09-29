package uiapp

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
)

// My Exports: your own recent export files, with a one-key "get me a fresh
// download link" — the most common support ask ("the link died, can you resend
// it?") without anyone touching a terminal. The files themselves already expire
// on their own (Admin → Access Control → Security settings); this only ever
// makes a new link to one that's still there.
const scrMyExports = "my_exports"

func myExportsScreen() screenModel {
	return &selectList{
		panelID:   "MYEXP",
		title:     "My Exports",
		hint:      "Enter regenerates a fresh download link",
		emptyHint: "Nothing exported yet — X on a list screen makes one.",
		rows: func(app *App) ([]string, [][]string, []string) {
			if app.session == nil {
				return nil, nil, nil
			}
			files, err := exports.ListExports(exports.Dir(), app.session.Username)
			if err != nil {
				app.setMsg(err.Error(), true)
			}
			var rows [][]string
			var keys []string
			for i, f := range files {
				rows = append(rows, []string{exports.Describe(f.Path), f.ModTime.Format("Jan 2 15:04")})
				keys = append(keys, fmt.Sprint(i))
			}
			return []string{"File", "Created"}, rows, keys
		},
		keys: func(app *App, key string, msg tea.KeyMsg) {
			if msg.Type != tea.KeyEnter || key == "" || app.session == nil {
				return
			}
			files, err := exports.ListExports(exports.Dir(), app.session.Username)
			if err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			var idx int
			if _, err := fmt.Sscanf(key, "%d", &idx); err != nil || idx < 0 || idx >= len(files) {
				return
			}
			f := files[idx]
			if !app.can("exports.download") {
				app.setMsg("ACCESS DENIED — needs the exports.download permission.", true)
				return
			}
			tok, err := exports.NewLink(exports.Dir(), f.Path, app.session.Username)
			if err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.userName(), "", "EXPORT_RELINKED", "SUCCESS", f.Path)
			app.setMsg("New link (expires in "+exports.LinkTTL().String()+"): "+exports.URL(tok), false)
		},
	}
}
