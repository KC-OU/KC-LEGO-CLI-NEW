package uiapp

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/labels"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Printable set labels: pick the label stock, and — for exactly one set at a
// non-sheet size, where PNG/ZPL/.lbx are even possible (see their own doc
// comments in internal/labels) — which format(s) to save, then a PDF (and an
// HTML page) is saved with the one-time download link / QR code, like any
// export.

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

type labelsScreen struct {
	base
	sizeIdx int // -1 = still choosing the stock; >= 0 = that size chosen, waiting on a format choice
}

func (s *labelsScreen) PanelID() string    { return "LABELS" }
func (s *labelsScreen) Title() string      { return "Print Labels" }
func (s *labelsScreen) OnEnter(app *App)   { s.sizeIdx = -1 }
func (s *labelsScreen) FKeys() [][2]string { return [][2]string{{"F3", "Exit"}, {"F12", "Cancel"}} }

func (s *labelsScreen) Body(app *App) string {
	if app.exportRes != nil {
		return exportResultBody(app, app.exportRes)
	}
	t := app.theme
	if s.sizeIdx >= 0 {
		return s.formatBody(app)
	}
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

func (s *labelsScreen) formatBody(app *App) string {
	t := app.theme
	size := labels.Sizes[s.sizeIdx]
	var b strings.Builder
	b.WriteString(t.Strong.Render("Format for "+size.Name) + "\n\n")
	rows := []string{
		"PDF + web page (default — print at 100%, no margins)",
		"Also a PNG image — no page-size matching to get wrong, print it via \"Print Picture\"",
		"Also Zebra ZPL — Zebra-style printers, not a Brother QL",
		"Also a Brother .lbx — a real P-touch Editor file; test-print before trusting it",
		"All of the above",
	}
	for i, row := range rows {
		fmt.Fprintf(&b, "  %s  %s\n", t.Accent.Render(fmt.Sprint(i+1)), t.Text.Render(row))
	}
	b.WriteString("\n" + t.Muted.Render("F12 cancels."))
	return b.String()
}

func (s *labelsScreen) HandleKey(app *App, msg tea.KeyMsg) {
	if app.exportRes != nil || msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return
	}
	if s.sizeIdx < 0 {
		i := int(msg.Runes[0] - '1')
		if i < 0 || i >= len(labels.Sizes) {
			return
		}
		// Only one set at a non-sheet size can even produce PNG/ZPL/.lbx —
		// everything else has nothing to choose, straight to PDF + web page.
		if len(app.labelSets) == 1 && !labels.Sizes[i].Sheet() {
			s.sizeIdx = i
			return
		}
		s.save(app, i, 0)
		return
	}
	f := int(msg.Runes[0] - '0')
	if f < 1 || f > 5 {
		return
	}
	s.save(app, s.sizeIdx, f)
}

// save writes PDF + HTML (always) plus whichever extra format the format
// step chose: 2 = PNG, 3 = ZPL, 4 = .lbx, 5 = all three, 0 or 1 = none.
func (s *labelsScreen) save(app *App, sizeIdx, format int) {
	size := labels.Sizes[sizeIdx]
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
	warnings := []string{"The web page version is " + exports.Describe(htmlPath)}

	if format == 2 || format == 5 {
		if png, err := labels.PNG(items, size); err == nil {
			if p, err := exports.Save(dir, user, "labels-"+size.ID, name, "png", png); err == nil {
				warnings = append(warnings, "Also saved as a PNG image: "+exports.Describe(p))
			}
		}
	}
	if format == 3 || format == 5 {
		if zpl, err := labels.ZPL(items, size); err == nil {
			if p, err := exports.Save(dir, user, "labels-"+size.ID, name, "zpl", zpl); err == nil {
				warnings = append(warnings, "Also saved as Zebra ZPL: "+exports.Describe(p))
			}
		}
	}
	if format == 4 || format == 5 {
		if lbx, err := labels.LBX(items, size); err == nil {
			if p, err := exports.Save(dir, user, "labels-"+size.ID, name, "lbx", lbx); err == nil {
				warnings = append(warnings, "Also saved as a Brother .lbx (test-print before trusting it): "+exports.Describe(p))
			}
		}
	}
	res := &exportResult{Path: pdfPath, Warnings: warnings}
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
