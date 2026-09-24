package uiapp

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mdp/qrterminal/v3"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// The X key on Missing Parts, Part / Set Detail, Owned Parts and What Can I Build
// opens this screen: pick a format, the file is saved in WMS_EXPORT_DIR, and — when
// WMS_PUBLIC_URL is set — a single-use download link is shown as text and as a QR
// code, so a file made over telnet can be scanned straight onto a phone.

const scrExport = "export"

type exportJob struct {
	What    string // shown in the title: "missing parts for 75192"
	Kind    string // file-name part: "missing", "detail", "owned", "build"
	Num     string
	Formats []string // lego.Encode names, in the order offered
	Build   func(app *App) (*lego.ExportData, error)
	// RawBuild, when set, bypasses Build+lego.Encode entirely — for a job whose data
	// doesn't fit ExportData (the stock-take checklist's StockSheetLine, say). Formats
	// must then be exactly one key (e.g. "checklist"), just for the screen's key/label.
	RawBuild func(app *App) (body []byte, ext string, err error)
	// Archive, when true, also saves a permanent-ish copy (see internal/lego.ArchiveReport)
	// — set on the Reports hub's jobs specifically (a "the QR expired, can I still get that
	// report" answer), not on every export (owned parts, missing parts, etc.).
	Archive bool
}

type exportResult struct {
	Path, URL string
	Warnings  []string
}

// formatKeys are the letters the export screen offers each format under.
var formatKeys = map[string][2]string{
	"json":            {"J", "JSON — facts and every part, pictures embedded"},
	"xlsx":            {"E", "Excel workbook (.xlsx) — a picture link per part"},
	"csv":             {"C", "CSV — opens in Excel or LibreOffice"},
	"html":            {"H", "Web page with pictures (print it to PDF)"},
	"bricklink-xml":   {"B", "BrickLink wanted list (XML upload)"},
	"rebrickable-csv": {"R", "Rebrickable parts list (CSV import)"},
	"sorting-html":    {"G", "Sorting sheet: parts by colour with pictures and tick boxes"},
	"report":          {"P", "Printable report (clean, title-page form — for reading or printing)"},
	"sets-csv":        {"L", "Sets CSV — sets only (plain csv is parts-only, and empty for a sets-only report)"},
	"checklist":       {"K", "Printable checklist (blank, to print and count)"},
}

func startExport(app *App, job *exportJob) {
	if !app.require("lego.export", "EXPORT "+job.Kind) || !app.require("exports.create", "EXPORT "+job.Kind) {
		return
	}
	app.exportJob, app.exportRes, app.discordAsking = job, nil, false
	app.goTo(scrExport)
}

type exportScreen struct{ base }

func (s *exportScreen) PanelID() string  { return "EXPORT" }
func (s *exportScreen) Title() string    { return "Export" }
func (s *exportScreen) OnEnter(app *App) {}

func (s *exportScreen) FKeys() [][2]string {
	return [][2]string{{"F3", "Exit"}, {"F12", "Cancel"}}
}

func (s *exportScreen) Body(app *App) string {
	t := app.theme
	job := app.exportJob
	if job == nil {
		return t.Muted.Render("Nothing to export — go back and open a list first.")
	}
	var b strings.Builder
	if r := app.exportRes; r != nil {
		body := exportResultBody(app, r)
		switch {
		case app.discordAsking:
			body += "\n\n" + discordAskBody(app)
		case notify.DiscordBotFromConfig().Enabled() && exports.URL("x") != "":
			body += "\n\n" + t.Muted.Render("D  send this to Discord (with a QR code)")
		}
		return body
	}
	b.WriteString(t.Strong.Render("Export "+job.What) + "\n\n")
	for _, f := range job.Formats {
		k := formatKeys[f]
		fmt.Fprintf(&b, "  %s  %s\n", t.Accent.Render(k[0]), t.Text.Render(k[1]))
	}
	b.WriteString("\n" + t.Muted.Render(fmt.Sprintf("Saved in %s for %d days.", exports.Dir(), int(exports.MaxAge().Hours()/24))))
	if exports.URL("x") != "" {
		b.WriteString("\n" + t.Muted.Render(fmt.Sprintf("You also get a one-time download link and QR code (valid %d minutes).", int(exports.LinkTTL().Minutes()))))
	}
	return b.String()
}

func exportResultBody(app *App, r *exportResult) string {
	t := app.theme
	lines := []string{ui.Status(t, true, "Saved "+exports.Describe(r.Path))}
	for _, w := range r.Warnings {
		lines = append(lines, ui.Warn(t, w))
	}
	if r.URL == "" {
		lines = append(lines, t.Muted.Render("Set WMS_PUBLIC_URL (Admin → Settings) to get download links and QR codes."))
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "", t.Strong.Render(fmt.Sprintf("Download once, within %d minutes:", int(exports.LinkTTL().Minutes()))))
	qr := QRCode(r.URL)
	textW := t.W()
	if !t.Mono && t.W() >= lipgloss.Width(qr)+42 {
		textW = t.W() - lipgloss.Width(qr) - 2
	}
	for u := r.URL; u != ""; { // cut at a fixed width: a word wrap would break at the host's hyphen
		n := min(len(u), textW)
		lines = append(lines, t.Accent.Render(u[:n]))
		u = u[n:]
	}
	text := strings.Join(lines, "\n")
	qrH := strings.Count(qr, "\n") + 1
	room := 1 << 30
	if app.height > 0 {
		room = app.height - 9
	}
	switch {
	case t.Mono:
		return text // NO_COLOR: the link alone (a QR code is all block glyphs)
	case qrH <= room && t.W() >= lipgloss.Width(qr)+42:
		return lipgloss.JoinHorizontal(lipgloss.Top, qr, "  ", lipgloss.NewStyle().Width(textW).Render(text+"\n\n"+t.Muted.Render("Scan the code with your phone's camera.")))
	case qrH+len(lines)+1 <= room:
		return text + "\n\n" + qr
	}
	return text + "\n\n" + t.Muted.Render("(make the window taller to see a QR code)")
}

// QRCode draws s as a QR code in half blocks (two modules per character row), with
// light modules as full blocks so it scans on a dark terminal.
func QRCode(s string) string {
	var b bytes.Buffer
	qrterminal.GenerateWithConfig(s, qrterminal.Config{Level: qrterminal.L, Writer: &b, HalfBlocks: true, QuietZone: 1})
	return strings.TrimRight(b.String(), "\n")
}

func (s *exportScreen) HandleKey(app *App, msg tea.KeyMsg) {
	job := app.exportJob
	if job == nil {
		return
	}
	if app.discordAsking {
		handleDiscordAskKey(app, msg)
		return
	}
	if app.exportRes != nil {
		if isKey(msg, 'd') && notify.DiscordBotFromConfig().Enabled() && exports.URL("x") != "" {
			app.discordAsking = true
		}
		return
	}
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return
	}
	key := strings.ToUpper(string(msg.Runes))
	for _, f := range job.Formats {
		if formatKeys[f][0] == key {
			runExport(app, job, f)
			return
		}
	}
}

func runExport(app *App, job *exportJob, format string) {
	var body []byte
	var ext string
	var warns lego.ExportWarnings
	if job.RawBuild != nil {
		var err error
		body, ext, err = job.RawBuild(app)
		if err != nil {
			app.setMsg("Export failed: "+err.Error(), true)
			return
		}
	} else {
		data, err := job.Build(app)
		if err != nil {
			app.setMsg("Export failed: "+err.Error(), true)
			return
		}
		body, ext, warns, err = lego.Encode(format, data)
		if err != nil {
			app.setMsg("Export failed: "+err.Error(), true)
			return
		}
	}
	user, role, owner := app.userName(), "", app.userKey()
	if app.session != nil {
		role = app.session.Role
	}
	dir := exports.Dir()
	exports.Cleanup(dir)
	path, err := exports.Save(dir, user, job.Kind, job.Num, ext, body)
	if err != nil {
		app.setMsg("Could not save the export: "+err.Error(), true)
		return
	}
	res := &exportResult{Path: path, Warnings: warns}
	if exports.URL("x") != "" && app.can("exports.download") {
		if tok, err := exports.NewLink(dir, path, owner); err == nil {
			res.URL = exports.URL(tok)
		}
	}
	if job.Archive {
		archiveDir := config.Get(config.ArchiveDir)
		_, _ = app.legoDB.ArchiveReport(archiveDir, job.Kind, job.What, owner, ext, body)
		_, _ = app.legoDB.CleanupArchive(archiveDir, 90*24*time.Hour)
	}
	app.exportRes = res
	app.audit.Log(user, role, "EXPORT", "SUCCESS", fmt.Sprintf("%s %s as %s", job.Kind, job.Num, format))
	app.emit("export_done", map[string]any{"file": path, "format": format, "kind": job.Kind, "num": job.Num, "user": user})
}

func isKey(msg tea.KeyMsg, k rune) bool {
	return msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && (msg.Runes[0] == k || msg.Runes[0] == k-'a'+'A')
}

// withPictures adds picture links to every part row (embedding the cached ones).
func withPictures(app *App, d *lego.ExportData) *lego.ExportData {
	d.AddPartImages(app.images)
	return d
}

func missingExportJob(v *missingView) *exportJob {
	return &exportJob{What: "missing parts for " + v.SetNum, Kind: "missing", Num: v.SetNum,
		Formats: []string{"json", "xlsx", "csv", "html", "bricklink-xml", "rebrickable-csv"},
		Build: func(app *App) (*lego.ExportData, error) {
			d := app.legoDB.ExportMissing(v.SetNum, v.Report)
			r := v.Report
			d.Facts = [][2]string{{"Set", strings.TrimSpace(v.SetNum + " " + v.Name)}, {"Copies", fmt.Sprint(r.Copies)},
				{"You hold", fmt.Sprintf("%d%% (%d of %d pieces)", r.Percent(), r.PiecesHeld, r.PiecesNeeded)},
				{"Lines complete", fmt.Sprintf("%d of %d", r.Complete, r.Lines)}, {"Parts list from", v.Source}}
			return withPictures(app, d), nil
		}}
}

func ownedExportJob() *exportJob {
	return &exportJob{What: "your owned parts and sets", Kind: "owned",
		Formats: []string{"json", "xlsx", "csv", "html", "rebrickable-csv", "bricklink-xml"},
		Build: func(app *App) (*lego.ExportData, error) {
			d, err := app.legoDB.ExportOwned()
			if err != nil {
				return nil, err
			}
			return withPictures(app, d), nil
		}}
}

func buildExportJob() *exportJob {
	return &exportJob{What: "what you can build", Kind: "build",
		Formats: []string{"json", "xlsx", "csv", "html"},
		Build: func(app *App) (*lego.ExportData, error) {
			res, err := app.legoDB.CanBuild(lego.BuildOptions{MinPercent: 50})
			if err != nil {
				return nil, err
			}
			return lego.ExportBuild(res), nil
		}}
}

func detailExportJob(req *detailReq, page *detailPage) *exportJob {
	what := req.Kind + " " + req.Num
	formats := []string{"json", "xlsx", "html"}
	if req.Kind == "set" {
		formats = append(formats, "sorting-html")
	}
	return &exportJob{What: what + " (with picture)", Kind: req.Kind, Num: req.Num,
		Formats: formats,
		Build: func(app *App) (*lego.ExportData, error) {
			d, err := app.legoDB.ExportDetail(app.ctx(), app.rebrick, app.images, req.Kind, req.Num, req.ColorID)
			if err != nil {
				return nil, err
			}
			if page != nil && len(page.Rows) > 0 { // what the screen showed, which is richer
				d.Facts = d.Facts[:0]
				for _, r := range page.Rows {
					d.Facts = append(d.Facts, [2]string{r[0], r[1]})
				}
			}
			return d, nil
		}}
}

// userKey is the signed-in user as the access policy names them ("source:name").
func (a *App) userKey() string {
	if a.session == nil {
		return "local:local"
	}
	return access.Key(a.session.Source, a.session.Username)
}
