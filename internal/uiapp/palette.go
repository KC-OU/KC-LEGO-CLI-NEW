package uiapp

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// The command palette: Ctrl-K (or F2, for terminals that swallow Ctrl-K) opens a box
// where you type a few letters of anywhere you want to go, or a part or set number,
// and Enter takes you there. It lists screens, actions, your recent parts and, as
// you type, matching parts and sets from the offline catalog. It goes through the
// same navigation as the menus, so a screen your role may not use still bounces you
// with the usual message.

type palEntry struct {
	Label string
	Hint  string
	Run   func(app *App)
}

const paletteRows = 10

// paletteScreens are the screens worth jumping straight to (ones that need no
// prior state, unlike a results page or a confirm step).
var paletteScreens = []struct{ Label, ID, Hint string }{
	{"Overview", scrOverview, "KPIs and container status"},
	{"Part-DB Hub", scrPartDBHub, ""},
	{"Search Part-DB", scrPartDBBrowse, "find a component"},
	{"Create / update a part", scrPartDBCreate, "lookup-first add flow"},
	{"Adjust stock", scrPartDBAdjust, "receive or remove stock"},
	{"LEGO Collection", scrLegoHub, ""},
	{"Search my sets", scrLegoSetSearch, ""},
	{"Search LEGO sets", scrLegoSetFind, "offline catalog first"},
	{"Search LEGO parts", scrLegoPartSearch, "offline catalog first"},
	{"Add a set", scrLegoSetAdd, ""},
	{"Add an owned LEGO part", scrLegoPartAdd, "number, colour, quantity"},
	{"List owned LEGO parts", scrLegoPartOwned, "with LOW markers"},
	{"LEGO collection stats", scrLegoStats, ""},
	{"Missing parts for a set", scrLegoMissingAsk, ""},
	{"What can I build?", scrLegoBuild, "from your loose parts"},
	{"History of changes", scrLegoHistory, "and growth chart"},
	{"Part / set detail", scrLegoDetailAsk, "with a picture"},
	{"BrickLink lookup by number", scrLegoBLAsk, "price, live"},
	{"Operations", scrOpsHub, ""},
	{"Script Hub", scrScripts, ""},
	{"Audit log", scrAuditLog, ""},
	{"Admin", scrAdminHub, ""},
	{"Users", scrUsers, "admin"},
	{"Settings & API keys", scrSettingsHub, "admin"},
	{"Settings: BrickLink API", scrSettingsBrickLink, "admin"},
	{"Settings: Display theme", scrSettingsTheme, "admin"},
}

func (a *App) openPalette() {
	if !a.authed {
		return
	}
	a.paletteOpen, a.paletteInput, a.paletteSel = true, "", 0
}

func (a *App) goFromHub(id string) {
	a.paletteOpen = false
	a.cur, a.stack = scrHub, nil
	a.goTo(id)
}

// paletteEntries returns what matches the query, best first.
func (a *App) paletteEntries(query string) []palEntry {
	query = strings.TrimSpace(query)
	var all []palEntry
	for _, s := range paletteScreens {
		s := s
		all = append(all, palEntry{Label: s.Label, Hint: s.Hint, Run: func(app *App) { app.goFromHub(s.ID) }})
	}
	all = append(all,
		palEntry{Label: "Undo the last change", Hint: "F9", Run: func(app *App) { app.paletteOpen = false; app.doUndo() }},
		palEntry{Label: "Quick add", Hint: "F6", Run: func(app *App) { app.paletteOpen = false; app.openQuickAdd() }},
		palEntry{Label: "Key help", Hint: "F1", Run: func(app *App) { app.paletteOpen = false; app.helpOpen = true }},
		palEntry{Label: "Lock the session", Hint: "F10", Run: func(app *App) { app.paletteOpen = false; app.lock() }},
		palEntry{Label: "Sign out", Run: func(app *App) { app.paletteOpen = false; app.logout() }},
	)
	rows := make([][]string, len(all))
	for i, e := range all {
		rows[i] = []string{e.Label, e.Hint}
	}
	var out []palEntry
	byLabel := map[string]palEntry{}
	for _, e := range all {
		byLabel[e.Label] = e
	}
	for _, r := range filterRows(rows, query) {
		out = append(out, byLabel[r[0]])
	}

	var dynamic []palEntry
	if query == "" {
		if recent, err := a.legoDB.Recents(5); err == nil {
			for _, num := range recent {
				num := num
				dynamic = append(dynamic, palEntry{Label: "Recent part " + num, Hint: "detail", Run: func(app *App) { app.openDetailFor("part " + num) }})
			}
		}
	} else if len([]rune(query)) >= 2 {
		if parts, err := a.legoDB.SearchCatalogParts(query, 4); err == nil {
			for _, p := range parts {
				p := p
				dynamic = append(dynamic, palEntry{Label: "Part " + p.Num + " — " + p.Name, Hint: p.Category, Run: func(app *App) { app.openDetailFor("part " + p.Num) }})
			}
		}
		if sets, err := a.legoDB.SearchCatalogSets(query, 4); err == nil {
			for _, s := range sets {
				s := s
				dynamic = append(dynamic, palEntry{Label: fmt.Sprintf("Set %s — %s (%d)", s.Num, s.Name, s.Year), Hint: s.Theme, Run: func(app *App) { app.openDetailFor("set " + s.Num) }})
			}
		}
	}
	// A query with a digit in it is a part or set number: those go first. Words ("stats") name a
	// screen, so screens go first and the catalog hits follow.
	if query == "" || strings.ContainsAny(query, "0123456789") {
		return append(dynamic, out...)
	}
	return append(out, dynamic...)
}

// openDetailFor opens the detail page for "part 3001" / "set 75192-1".
func (a *App) openDetailFor(input string) {
	a.paletteOpen = false
	req, msg := resolveDetail(a, input)
	a.cur, a.stack = scrHub, nil
	if req == nil {
		a.setMsg(msg, true)
		return
	}
	a.detail = req
	a.goTo(scrLegoDetail)
}

// paletteKey handles a key while the palette is open; it consumes everything.
func (a *App) paletteKey(msg tea.KeyMsg) {
	entries := a.paletteEntries(a.paletteInput)
	visible := min(len(entries), paletteRows)
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlK, tea.KeyF2:
		a.paletteOpen = false
	case tea.KeyEnter:
		if visible > 0 {
			entries[min(a.paletteSel, visible-1)].Run(a)
		}
	case tea.KeyUp, tea.KeyShiftTab:
		if a.paletteSel > 0 {
			a.paletteSel--
		}
	case tea.KeyDown, tea.KeyTab:
		if a.paletteSel < visible-1 {
			a.paletteSel++
		}
	case tea.KeyBackspace:
		if r := []rune(a.paletteInput); len(r) > 0 {
			a.paletteInput, a.paletteSel = string(r[:len(r)-1]), 0
		}
	case tea.KeySpace:
		a.paletteInput, a.paletteSel = a.paletteInput+" ", 0
	case tea.KeyRunes:
		a.paletteInput, a.paletteSel = a.paletteInput+string(msg.Runes), 0
	}
}

func (a *App) paletteView() string {
	t := a.theme
	entries := a.paletteEntries(a.paletteInput)
	var b strings.Builder
	b.WriteString(t.Brand.Render("Go to:") + " " + a.paletteInput + "█\n\n")
	if len(entries) == 0 {
		b.WriteString(t.Muted.Render("  nothing matches — try part of a screen name, or a part or set number") + "\n")
	}
	room := max(a.theme.W()-4-2, 10) // inside the panel, after the two-column marker
	for i, e := range entries[:min(len(entries), paletteRows)] {
		label := ansi.Truncate(e.Label, room, "…") // one row each: a long catalog name must not wrap
		hint := ""
		if left := room - lipgloss.Width(label) - 3; e.Hint != "" && left >= 4 {
			hint = "   " + ansi.Truncate(e.Hint, left, "…")
		}
		line := "  " + label + t.Muted.Render(hint)
		if i == a.paletteSel {
			line = t.TitleReverse.Render("> "+label+" ") + t.Muted.Render(hint)
		}
		b.WriteString(line + "\n")
	}
	if len(entries) > paletteRows {
		b.WriteString(t.Muted.Render(fmt.Sprintf("  … %d more: keep typing to narrow", len(entries)-paletteRows)) + "\n")
	}
	b.WriteString("\n" + t.Muted.Render("Type to filter   Up/Down choose   Enter go   Esc close"))
	return ui.RenderPanel(t, t.Brand, "Command Palette (Ctrl-K or F2)", b.String())
}
