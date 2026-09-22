package uiapp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/docker"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// The control room: the sign-on screen and the Overview dashboard share a big
// KC-PARTS logo, live system status (checked in the background and cached, so a
// slow container never delays the prompt), collection figures (sets, pieces,
// what is missing and on order) and alerts. Before sign-in the collection and
// alerts are public (WMS_SIGNON_PUBLIC_STATS=0 hides them).

var kcLogo = []string{
	`██╗  ██╗ ██████╗      ██████╗  █████╗ ██████╗ ████████╗███████╗`,
	`██║ ██╔╝██╔════╝      ██╔══██╗██╔══██╗██╔══██╗╚══██╔══╝██╔════╝`,
	`█████╔╝ ██║     █████╗██████╔╝███████║██████╔╝   ██║   ███████╗`,
	`██╔═██╗ ██║     ╚════╝██╔═══╝ ██╔══██║██╔══██╗   ██║   ╚════██║`,
	`██║  ██╗╚██████╗      ██║     ██║  ██║██║  ██║   ██║   ███████║`,
	`╚═╝  ╚═╝ ╚═════╝      ╚═╝     ╚═╝  ╚═╝╚═╝  ╚═╝   ╚═╝   ╚══════╝`,
}

// sysItem is one system's state.
type sysItem struct {
	Name string
	OK   bool
	Warn bool
	Text string
}

type sysStatus struct {
	Items []sysItem
	At    time.Time
}

type sysStatusMsg sysStatus
type clockTickMsg struct{}
type logoTickMsg struct{}

const logoFrames = 10

// statusCmd checks the systems (each with a short timeout, in parallel).
func (a *App) statusCmd() tea.Cmd {
	legoDB := a.legoDB
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		items := make([]sysItem, 4)
		var wg sync.WaitGroup
		container := func(i int, name, key string) {
			defer wg.Done()
			st, err := docker.InspectStatus(ctx, config.Get(key))
			switch {
			case err != nil || st == "":
				items[i] = sysItem{Name: name, Text: "unreachable"}
			case st == "running":
				items[i] = sysItem{Name: name, OK: true, Text: "online"}
			default:
				items[i] = sysItem{Name: name, Text: st}
			}
		}
		wg.Add(3)
		go container(0, "Part-DB", config.PartDBContainer)
		go container(1, "ModernWMS", config.ModernWMSContainer)
		go func() {
			defer wg.Done()
			c := &http.Client{Timeout: 1500 * time.Millisecond}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+config.Get(config.SyncPort)+"/", nil)
			resp, err := c.Do(req)
			if err != nil {
				items[2] = sysItem{Name: "Sync", Text: "not answering"}
				return
			}
			resp.Body.Close()
			items[2] = sysItem{Name: "Sync", OK: resp.StatusCode < 500, Text: "running"}
		}()
		wg.Wait()
		items[3] = catalogItem(legoDB)
		return sysStatusMsg{Items: items, At: time.Now()}
	}
}

func catalogItem(db *lego.DB) sysItem {
	it := sysItem{Name: "Catalog", Text: "not loaded"}
	if db == nil {
		return it
	}
	parts, _, refreshed, err := db.CatalogStatus()
	if err != nil || parts == 0 {
		return it
	}
	days := int(time.Since(refreshed).Hours() / 24)
	it.Text, it.OK = fmt.Sprintf("%dd old", days), days <= 30
	it.Warn = !it.OK
	if days == 0 {
		it.Text = "fresh today"
	}
	return it
}

func statusEvery() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return statusRefreshMsg{} })
}

type statusRefreshMsg struct{}

func clockTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return clockTickMsg{} })
}

func logoTick() tea.Cmd {
	return tea.Tick(55*time.Millisecond, func(time.Time) tea.Msg { return logoTickMsg{} })
}

// controlRoomUpdate handles the control room's own messages.
func (a *App) controlRoomUpdate(msg tea.Msg) (bool, tea.Cmd) {
	switch m := msg.(type) {
	case sysStatusMsg:
		s := sysStatus(m)
		a.sys = &s
		return true, statusEvery()
	case statusRefreshMsg:
		return true, a.statusCmd()
	case clockTickMsg:
		if a.cur == scrLogin || a.cur == scrOverview {
			return true, clockTick()
		}
		a.clockOn = false
		return true, nil
	case logoTickMsg:
		if a.logoFrame < logoFrames {
			a.logoFrame++
			return true, logoTick()
		}
		return true, nil
	}
	return false, nil
}

// collection is the figures the tiles show, cached for 10 seconds.
type collection struct {
	Sets, Pieces, Incomplete, Missing, OnOrder, Low, Shipped int
	Spent                                                    float64
	Currency                                                 string
	at                                                       time.Time
}

func (a *App) collection() collection {
	if a.coll != nil && time.Since(a.coll.at) < 10*time.Second {
		return *a.coll
	}
	c := collection{at: time.Now()}
	if st, err := a.legoDB.Stats(); err == nil {
		c.Sets, c.Pieces = st.SetCopies, st.SetPieces+st.LoosePieces
		c.Low = st.LowStock
	}
	if inc, err := a.legoDB.IncompleteSets(); err == nil {
		c.Incomplete = len(inc)
		for _, s := range inc {
			c.Missing += s.MissingQty
			c.OnOrder += s.OnOrderQty
		}
	}
	if orders, err := a.legoDB.ListOrders("shipped"); err == nil {
		c.Shipped = len(orders)
	}
	month := time.Now().Format("2006-01")
	if rows, err := a.legoDB.Spend("month"); err == nil {
		for _, r := range rows {
			if r.Key == month {
				c.Spent, c.Currency = r.Parts+r.Shipping, r.Currency
			}
		}
	}
	a.coll = &c
	return c
}

// alerts are the things worth a look, most important first.
func (a *App) alerts(c collection) []string {
	var out []string
	if c.Incomplete > 0 {
		out = append(out, fmt.Sprintf("%d set(s) short %d", c.Incomplete, c.Missing))
	}
	if c.Low > 0 {
		out = append(out, fmt.Sprintf("%d part(s) LOW", c.Low))
	}
	if c.Shipped > 0 {
		out = append(out, fmt.Sprintf("%d order(s) on the way", c.Shipped))
	}
	if age, ok := newestBackup(config.Get(config.ModernWMSBackupDir)); ok && age > 7*24*time.Hour {
		out = append(out, fmt.Sprintf("last backup %d days ago", int(age.Hours()/24)))
	}
	if a.sys != nil {
		for _, it := range a.sys.Items {
			if !it.OK && !it.Warn {
				out = append(out, it.Name+" is "+it.Text)
			}
		}
	}
	return out
}

func newestBackup(dir string) (time.Duration, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return 0, false
	}
	var newest time.Time
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return time.Since(newest), !newest.IsZero()
}

// logo is the KC-PARTS logo, revealed left to right while the sweep runs, in the
// theme's brand and accent colours; a small screen gets a one-line version.
func (a *App) logo(width, maxRows int) string {
	t := a.theme
	if t.Mono || width < 66 || maxRows < len(kcLogo) {
		return t.Brand.Render("■ KC-PARTS") + t.Muted.Render("  LEGO · Part-DB · ModernWMS control suite")
	}
	cols := len([]rune(kcLogo[0]))
	show := cols
	if a.logoFrame < logoFrames {
		show = cols * a.logoFrame / logoFrames
	}
	styles := []lipgloss.Style{t.Brand, t.Brand, t.Accent.UnsetUnderline(), t.Accent.UnsetUnderline(), t.Strong, t.Muted}
	var lines []string
	for i, l := range kcLogo {
		r := []rune(l)
		lines = append(lines, styles[i%len(styles)].Render(string(r[:show]))+strings.Repeat(" ", cols-show))
	}
	lines = append(lines, t.Muted.Render("   LEGO · Part-DB · ModernWMS control suite"))
	return strings.Join(lines, "\n")
}

// tile draws a titled box of lines, exactly w columns wide.
func (a *App) tile(title string, lines []string, w, h int) string {
	t := a.theme
	inner := w - 4
	for len(lines) < h {
		lines = append(lines, "")
	}
	var b strings.Builder
	top := "╭─ " + title + " " + strings.Repeat("─", max(0, w-5-lipgloss.Width(title))) + "╮"
	b.WriteString(t.Muted.Render(top) + "\n")
	for _, l := range lines[:h] {
		l = ansi.Truncate(l, inner, "…")
		b.WriteString(t.Muted.Render("│ ") + l + strings.Repeat(" ", max(0, inner-lipgloss.Width(l))) + t.Muted.Render(" │") + "\n")
	}
	b.WriteString(t.Muted.Render("╰" + strings.Repeat("─", w-2) + "╯"))
	return b.String()
}

func (a *App) dot(ok, warn bool) string {
	t := a.theme
	switch {
	case ok:
		return t.Success.Render("●")
	case warn:
		return t.Warning.Render("●")
	}
	return t.Danger.Render("○")
}

func (a *App) systemLines() []string {
	if a.sys == nil {
		return []string{a.theme.Muted.Render("checking…")}
	}
	var out []string
	for _, it := range a.sys.Items {
		out = append(out, fmt.Sprintf("%s %-10s %s", a.dot(it.OK, it.Warn), it.Name, a.theme.Muted.Render(it.Text)))
	}
	return out
}

func (a *App) collectionLines(c collection) []string {
	t := a.theme
	status := t.Success.Render("all sets complete")
	if c.Incomplete > 0 {
		status = t.Danger.Render(fmt.Sprintf("%d incomplete", c.Incomplete))
	}
	return []string{
		fmt.Sprintf("%s sets · %s pieces", t.Strong.Render(fmt.Sprint(c.Sets)), t.Strong.Render(fmt.Sprint(c.Pieces))),
		status,
		fmt.Sprintf("missing   %s", t.Strong.Render(fmt.Sprint(c.Missing))),
		fmt.Sprintf("on order  %s", t.Strong.Render(fmt.Sprint(c.OnOrder))),
	}
}

func (a *App) alertLines(c collection) []string {
	al := a.alerts(c)
	if len(al) == 0 {
		return []string{a.theme.Success.Render("✓ nothing needs you")}
	}
	var out []string
	for _, s := range al {
		out = append(out, a.theme.Warning.Render("!")+" "+s)
	}
	return out
}

// tiles lays out up to three tiles side by side across the width.
func (a *App) tiles(width int, parts ...[2]any) string {
	n := len(parts)
	w := (width - (n - 1)) / n
	var cols []string
	for i, p := range parts {
		tw := w
		if i == n-1 {
			tw = width - (n-1)*(w+1)
		}
		cols = append(cols, a.tile(p[0].(string), p[1].([]string), tw, 4))
	}
	out := cols[0]
	for _, c := range cols[1:] {
		out = lipgloss.JoinHorizontal(lipgloss.Top, out, " ", c)
	}
	return out
}

func publicStats() bool { return config.Get(config.SignOnPublicStats) != "0" }

// motd is the admin's message of the day ("" when none).
func motd() string { return strings.TrimSpace(config.Get(config.MOTD)) }

// signOnPreamble is the control room above the sign-on fields.
func signOnPreamble(app *App) string {
	t := app.theme
	w := min(t.W(), 100)
	h := app.height
	if h <= 0 {
		h = 25
	}
	fixed := 4 // the fields and message line under this
	if !app.theme.Classic {
		fixed += 8 // header and legend
	}
	room := h - fixed
	var parts []string
	logoRows := len(kcLogo) + 1
	tilesRows := 6
	if room >= logoRows+tilesRows+2 {
		parts = append(parts, app.logo(w, logoRows))
	} else {
		parts = append(parts, app.logo(0, 0))
	}
	if room >= tilesRows+3 || room >= logoRows+tilesRows+2 {
		if publicStats() {
			c := app.collection()
			parts = append(parts, app.tiles(w, [2]any{"SYSTEM", app.systemLines()}, [2]any{"COLLECTION", app.collectionLines(c)}, [2]any{"ALERTS", app.alertLines(c)}))
		} else {
			parts = append(parts, app.tiles(w, [2]any{"SYSTEM", app.systemLines()}))
		}
	}
	foot := []string{}
	if m := motd(); m != "" {
		foot = append(foot, t.Accent.UnsetUnderline().Render("✉ "+m))
	}
	where := "local console"
	switch {
	case app.transport == "telnet":
		where = "telnet · " + orDash(app.remoteAddr)
	case app.touchMode:
		where = "web terminal (HTTPS)"
	}
	foot = append(foot, t.Muted.Render(time.Now().Format("Mon 2 Jan 2006  15:04:05")+"  ·  "+where))
	if note := cleartextNote(app); note != "" {
		foot = append(foot, ansi.Truncate(note, t.W(), "…"))
	}
	parts = append(parts, strings.Join(foot, "\n"))
	return strings.Join(parts, "\n")
}

// dashboardBody is the Overview in control-room style.
func dashboardBody(app *App) string {
	t := app.theme
	w := t.W()
	c := app.collection()
	var parts []string
	parts = append(parts, app.tiles(w, [2]any{"SYSTEM", app.systemLines()}, [2]any{"COLLECTION", app.collectionLines(c)}, [2]any{"ALERTS", app.alertLines(c)}))
	spend := "nothing this month"
	if c.Spent > 0 {
		spend = fmt.Sprintf("%.2f %s this month", c.Spent, c.Currency)
	}
	day := "—"
	if r := app.setOfTheDay(); r != nil {
		day = fmt.Sprintf("%s %s (%d%%)", r.SetNum, r.Name, r.Percent)
	}
	var recent []string
	if rows, err := app.legoDB.History("", 4); err == nil {
		for _, r := range rows {
			what := fmt.Sprintf("%s %s %s", r.Action, r.ItemType, r.Item)
			if r.Note != "" {
				what += " — " + r.Note
			}
			recent = append(recent, t.Muted.Render(r.At.Local().Format("02 Jan 15:04"))+" "+what)
		}
	}
	if len(recent) == 0 {
		recent = []string{t.Muted.Render("no changes yet")}
	}
	growth := ""
	if snaps, err := app.legoDB.Snapshots(30); err == nil && len(snaps) > 1 {
		var vals []int
		for i := len(snaps) - 1; i >= 0; i-- {
			vals = append(vals, snaps[i].Pieces)
		}
		growth = "  " + t.Accent.UnsetUnderline().Render(miniSpark(vals)) + t.Muted.Render(" pieces, last 30 snapshots")
	}
	info := t.Muted.Render("Spend: ") + spend + t.Muted.Render("   Set of the Day: ") + day
	parts = append(parts, ansi.Truncate(info, w, "…"))
	if growth != "" {
		parts = append(parts, ansi.Truncate(growth, w, "…"))
	}
	parts = append(parts, app.tile("RECENT ACTIVITY", recent, w, min(4, len(recent))))
	parts = append(parts, t.Muted.Render("C completion · O orders · W workshop · K system KPIs · R refresh"))
	return strings.Join(parts, "\n")
}

// miniSpark is a one-line chart of vals.
func miniSpark(vals []int) string {
	bars := []rune("▁▂▃▄▅▆▇█")
	lo, hi := vals[0], vals[0]
	for _, v := range vals {
		lo, hi = min(lo, v), max(hi, v)
	}
	var b strings.Builder
	for _, v := range vals {
		i := 0
		if hi > lo {
			i = (v - lo) * (len(bars) - 1) / (hi - lo)
		}
		b.WriteRune(bars[i])
	}
	return b.String()
}
