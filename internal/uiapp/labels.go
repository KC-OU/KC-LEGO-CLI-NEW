package uiapp

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/labels"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Printable set labels: pick the label stock, and a PDF (and an HTML page) is saved
// with the one-time download link / QR code, like any export.

const scrLabels = "labels"

func startLabels(app *App, sets []string) {
	if !app.require("labels.print", "LABELS") {
		return
	}
	if len(sets) == 0 {
		app.setMsg("No sets to label.", true)
		return
	}
	app.labelSets, app.exportRes = sets, nil
	app.goTo(scrLabels)
}

type labelsScreen struct{ base }

func (s *labelsScreen) PanelID() string    { return "LABELS" }
func (s *labelsScreen) Title() string      { return "Print Labels" }
func (s *labelsScreen) OnEnter(app *App)   {}
func (s *labelsScreen) FKeys() [][2]string { return [][2]string{{"F3", "Exit"}, {"F12", "Cancel"}} }

func (s *labelsScreen) Body(app *App) string {
	if app.exportRes != nil {
		return exportResultBody(app, app.exportRes)
	}
	t := app.theme
	var b strings.Builder
	sets := app.labelSets
	what := strings.Join(sets[:min(len(sets), 6)], ", ")
	if len(sets) > 6 {
		what += fmt.Sprintf(" and %d more", len(sets)-6)
	}
	b.WriteString(t.Strong.Render(fmt.Sprintf("Labels for %d set(s): %s", len(sets), what)) + "\n\n")
	for i, sz := range labels.Sizes {
		fmt.Fprintf(&b, "  %s  %s\n", t.Accent.Render(fmt.Sprint(i+1)), t.Text.Render(sz.Name))
	}
	b.WriteString("\n" + t.Muted.Render("Each label: set, name, year, pieces, missing, who checked it and when,\nlocation, QR code and barcode. PDF + web page: print at 100%, no margins."))
	return b.String()
}

func (s *labelsScreen) HandleKey(app *App, msg tea.KeyMsg) {
	if app.exportRes != nil || msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return
	}
	i := int(msg.Runes[0] - '1')
	if i < 0 || i >= len(labels.Sizes) {
		return
	}
	size := labels.Sizes[i]
	var items []labels.Data
	for _, n := range app.labelSets {
		items = append(items, app.legoDB.LabelData(n))
	}
	dir, user := exports.Dir(), app.userName()
	name := app.labelSets[0]
	if len(app.labelSets) > 1 {
		name = fmt.Sprintf("%d-sets", len(app.labelSets))
	}
	htmlPath, err := exports.Save(dir, user, "labels-"+size.ID, name, "html", labels.HTML(items, size))
	if err == nil {
		_, err = exports.Save(dir, user, "labels-"+size.ID, name, "pdf", labels.PDF(items, size))
	}
	if err != nil {
		app.setMsg("Could not save the labels: "+err.Error(), true)
		return
	}
	pdfPath := strings.TrimSuffix(htmlPath, ".html") + ".pdf"
	res := &exportResult{Path: pdfPath, Warnings: []string{"The web page version is " + exports.Describe(htmlPath)}}
	if exports.URL("x") != "" && app.can("exports.download") {
		if tok, err := exports.NewLink(dir, pdfPath, app.userKey()); err == nil {
			res.URL = exports.URL(tok)
		}
	}
	app.exportRes = res
	app.audit.Log(user, "", "LABELS", "SUCCESS", fmt.Sprintf("%d label(s) %s", len(items), size.ID))
	app.emit("export_done", map[string]any{"file": pdfPath, "format": "pdf", "kind": "labels", "num": name, "user": user})
}

func labelsAskScreen() screenModel {
	return &formScreen{
		panelID: "LBLASK",
		title:   "Print Labels",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Sets: numbers separated by spaces, or all, or incomplete", Value: "incomplete", Fresh: true}}
		},
		submit: func(app *App, v []string) {
			sets, err := app.legoDB.LabelSets(strings.Fields(v[0]))
			if err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.back()
			startLabels(app, sets)
		},
	}
}
