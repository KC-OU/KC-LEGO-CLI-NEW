package uiapp

import (
	"context"
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/bricklink"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui/img"
)

// A detail page shows one part or set: its picture (drawn as text, so it works over
// telnet), what it is, what you hold, where it is used, and what it costs. Everything
// comes from the offline catalog and your own data; a picture is fetched at most once
// (then cached) and the BrickLink price only when you press P.

type detailReq struct {
	Kind    string // "part" or "set"
	Num     string
	ColorID int
}

type detailPage struct {
	Title   string
	Caption string
	Picture string
	Rows    [][]string
}

func legoDetailAskScreen() screenModel {
	return &formScreen{
		panelID: "LEGDTA",
		title:   "Part / Set Detail",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Part or set number (e.g. 3001, 75192, or an element ID)"}}
		},
		preamble: func(app *App) string {
			return app.theme.Muted.Render("Shows a picture, what it is, what you hold and where it is used. A number with a dash, or 'set 75192', is a set.")
		},
		submit: func(app *App, v []string) {
			req, msg := resolveDetail(app, v[0])
			if req == nil {
				app.setMsg(msg, true)
				return
			}
			app.detail = req
			if f, ok := app.screens[scrLegoDetailAsk].(*formScreen); ok && f.fl != nil {
				f.fl.Fields[0].Value = "" // coming back from the page, the next number starts fresh
			}
			app.goTo(scrLegoDetail)
		},
	}
}

// resolveDetail decides whether what was typed is a part, a set or an element ID. The second
// result is a message for the user when it is none of them.
func resolveDetail(app *App, input string) (*detailReq, string) {
	in := strings.TrimSpace(input)
	if in == "" {
		return nil, "Enter a part or set number."
	}
	explicit := ""
	if low := strings.ToLower(in); strings.HasPrefix(low, "set ") {
		explicit, in = "set", strings.TrimSpace(in[4:])
	} else if strings.HasPrefix(low, "part ") {
		explicit, in = "part", strings.TrimSpace(in[5:])
	}
	set := func() *detailReq {
		if s, _ := app.legoDB.CatalogSet(in); s != nil || app.rebrick.Enabled() {
			return &detailReq{Kind: "set", Num: in, ColorID: -1}
		}
		return nil
	}
	// A dash usually means a set ("75192-1"), but some part numbers have one too, so a set that is
	// not found does not end the search: the part checks below still run.
	if explicit == "set" || (explicit == "" && strings.Contains(in, "-")) {
		if r := set(); r != nil {
			return r, ""
		}
		if explicit == "set" {
			return nil, fmt.Sprintf("No set %q in the offline catalog.", in)
		}
	}
	if p, _ := app.legoDB.CatalogPart(in); p != nil {
		return &detailReq{Kind: "part", Num: p.Num, ColorID: -1}, ""
	}
	if explicit != "part" {
		if part, colorID, ok := app.legoDB.ElementPart(in); ok {
			return &detailReq{Kind: "part", Num: part, ColorID: colorID}, ""
		}
		if s, _ := app.legoDB.CatalogSet(in); s != nil {
			return &detailReq{Kind: "set", Num: in, ColorID: -1}, ""
		}
	}
	if app.rebrick.Enabled() || len(app.legoDB.OwnedPartsOfSafe(in)) > 0 {
		return &detailReq{Kind: "part", Num: in, ColorID: -1}, ""
	}
	return nil, fmt.Sprintf("No part or set numbered %q in the catalog. Try Search Parts.", in)
}

type detailScreen struct {
	base
	page *detailPage
}

func (s *detailScreen) PanelID() string { return "LEGDET2" }
func (s *detailScreen) Title() string {
	if s.page != nil {
		return s.page.Title
	}
	return "Detail"
}

func (s *detailScreen) OnEnter(app *App) {
	s.page = nil
	if app.detail == nil {
		app.setMsg("Nothing to show — go back and enter a number.", true)
		return
	}
	s.page = buildDetail(app, app.detail)
}

// pictureBox is how big the picture may be: sized so the page fits the client's
// window (a plain telnet window is 80x25) with the facts beside the picture.
func pictureBox(app *App) (cols, rows int) {
	h := app.height
	if h <= 0 {
		h = 25
	}
	switch {
	case h >= 40 && app.theme.W() >= 100:
		return 44, 18
	case h >= 30 && app.theme.W() >= 90:
		return 34, 14
	}
	return 24, 10
}

func (s *detailScreen) Body(app *App) string {
	if s.page == nil {
		return ""
	}
	t := app.theme
	side := s.page.Picture != "" && t.W() >= 70
	// The page must fit the window (a header band above, the key hints and any message below), so long
	// values (minifigure lists, examples of use) are shortened until it does.
	budget := 1 << 30
	if app.height > 0 {
		budget = app.height - 8 - 1
		if s.page.Caption != "" {
			budget--
		}
		if app.message != "" {
			budget -= 2
		}
	}
	var out string
	if side {
		picCols, _ := pictureBox(app)
		pic := lipgloss.NewStyle().Width(picCols).Render(s.page.Picture)
		var facts string
		for _, maxLines := range []int{99, 4, 3, 2, 1} {
			facts = renderFacts(t, s.page.Rows, t.W()-picCols-4, maxLines)
			if strings.Count(facts, "\n")+1 <= budget {
				break
			}
		}
		out = lipgloss.JoinHorizontal(lipgloss.Top, pic, "  ", facts)
	} else {
		if s.page.Picture != "" {
			out = s.page.Picture + "\n"
		}
		out += renderFacts(t, s.page.Rows, t.W()-2, 99)
	}
	if s.page.Caption != "" {
		out += "\n" + t.Muted.Render(s.page.Caption)
	}
	return out + "\n" + t.Muted.Render(detailKeys(app.detail))
}

// renderFacts prints "Label  value" lines with values wrapped to width, no grid: a
// grid would not fit beside a picture in an 80-column window.
func renderFacts(t ui.Theme, rows [][]string, width, maxLines int) string {
	labelW := 0
	for _, r := range rows {
		labelW = max(labelW, len([]rune(r[0])))
	}
	valueW := max(width-labelW-2, 12)
	var b strings.Builder
	for _, r := range rows {
		lines := strings.Split(lipgloss.NewStyle().Width(valueW).Render(r[1]), "\n")
		if len(lines) > maxLines {
			lines = lines[:maxLines]
			lines[maxLines-1] = ansi.Truncate(strings.TrimRight(lines[maxLines-1], " "), valueW-1, "") + "…"
		}
		for i, l := range lines {
			label := strings.Repeat(" ", labelW)
			if i == 0 {
				label = r[0] + strings.Repeat(" ", labelW-len([]rune(r[0])))
			}
			b.WriteString(t.Muted.Render(label) + "  " + t.Strong.Render(strings.TrimRight(l, " ")) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func detailKeys(r *detailReq) string {
	if r == nil {
		return ""
	}
	if r.Kind == "set" {
		return "A add  K check  M missing  P price  L label  X export  Q back"
	}
	return "A add / update this part   P BrickLink price   X export   Q back"
}

func (s *detailScreen) HandleKey(app *App, msg tea.KeyMsg) {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 || app.detail == nil {
		return
	}
	switch strings.ToLower(string(msg.Runes[0])) {
	case "a":
		if app.detail.Kind == "set" {
			startSetDraft(app, app.detail.Num)
		} else {
			startPartFlow(app, app.detail.Num)
		}
	case "m":
		if app.detail.Kind == "set" {
			showMissing(app, app.detail.Num)
		}
	case "p":
		if !app.require("bricklink.price", "BRICKLINK_PRICE") {
			return
		}
		fetchDetailPrice(app)
	case "x":
		startExport(app, detailExportJob(app.detail, s.page))
	case "k":
		if app.detail.Kind == "set" {
			kind := lego.CheckIntake
			if app.legoDB.GetSetState(catalogSetNum(app.detail.Num)).Checked() {
				kind = lego.CheckRecount
			}
			startCheck(app, catalogSetNum(app.detail.Num), kind)
		}
	case "l":
		if app.detail.Kind == "set" {
			startLabels(app, []string{catalogSetNum(app.detail.Num)})
		}
	}
}

// fetchDetailPrice asks BrickLink for this item's price (one call, cached a day) and
// stores it — off the key-handling path via startBusy, so a slow BrickLink call can't
// freeze the screen; detailPriceFetched refreshes the page once the price is stored.
func fetchDetailPrice(app *App) {
	c := app.blClient()
	if !c.Enabled() {
		app.setMsg("BrickLink is not set up (Admin > Settings & API Keys > BrickLink API).", true)
		return
	}
	req := app.detail
	typ, no, blColor := bricklink.Part, req.Num, 0
	if req.Kind == "set" {
		typ = bricklink.Set
		if !strings.Contains(no, "-") {
			no += "-1"
		}
	} else if req.ColorID >= 0 {
		blColor, _ = app.legoDB.BLColorFor(req.ColorID)
	}
	cond := strings.ToUpper(config.Get(config.BricklinkCondition))
	app.startBusy("Fetching BrickLink price…", func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		avg, cur, found, err := (priceAdapter{c}).AvgPrice(ctx, string(typ), no, blColor, cond)
		if err != nil {
			return detailPriceFetchedMsg{err: blMessage(err, req.Kind, req.Num)}
		}
		_ = app.legoDB.StorePrice(string(typ), no, blColor, cond, avg, cur, !found)
		if !found {
			return detailPriceFetchedMsg{err: "BrickLink has no sales for this item in its price window."}
		}
		return detailPriceFetchedMsg{msg: fmt.Sprintf("BrickLink average sold price: %.4f %s.", avg, cur)}
	})
}

// detailPriceFetchedMsg reports the background BrickLink price fetch fetchDetailPrice
// kicked off; handled in App.Update.
type detailPriceFetchedMsg struct{ msg, err string }

func (a *App) detailPriceFetched(m detailPriceFetchedMsg) {
	a.stopBusy()
	if scr, ok := a.screens[scrLegoDetail]; ok { // refresh so the newly stored price shows
		scr.OnEnter(a)
	}
	if m.err != "" {
		a.setMsg(m.err, true)
		return
	}
	a.setMsg(m.msg, false)
}

// priceAdapter prices through BrickLink for the lego package's PriceFetcher shape.
type priceAdapter struct{ c *bricklink.Client }

func (p priceAdapter) AvgPrice(ctx context.Context, itemType, no string, blColor int, cond string) (float64, string, bool, error) {
	pr, err := p.c.PriceGuide(ctx, bricklink.ItemType(itemType), no, blColor, bricklink.Sold, cond)
	switch {
	case err != nil && isNotFound(err):
		return 0, "", false, nil
	case err != nil:
		return 0, "", false, err
	case pr.TotalQuantity == 0 || pr.Avg == 0:
		return 0, pr.CurrencyCode, false, nil
	}
	return float64(pr.Avg), pr.CurrencyCode, true, nil
}

// picture loads and draws the first picture that works; it never fails the page.
func picture(app *App, urls []string) (art, caption string) {
	mode := img.ParseMode(config.Env(config.TUIImages, ""), app.theme.Mono)
	if mode == img.ModeOff {
		return "", ""
	}
	cols, rows := pictureBox(app)
	if app.theme.W() < 70 {
		cols = min(app.theme.W()-4, 30)
	}
	for _, u := range urls {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		b, err := app.images.Get(ctx, u)
		cancel()
		if err != nil {
			continue
		}
		m, err := img.Decode(b)
		if err != nil {
			continue
		}
		return img.Render(m, cols, rows, mode), ""
	}
	if app.images.Offline {
		return "", "(no picture: none cached and images are not downloaded in offline mode)"
	}
	return "", "(no picture available)"
}

func buildDetail(app *App, req *detailReq) *detailPage {
	if req.Kind == "set" {
		return buildSetDetail(app, req)
	}
	return buildPartDetail(app, req)
}

func buildPartDetail(app *App, req *detailReq) *detailPage {
	db := app.legoDB
	pg := &detailPage{Title: "Part " + req.Num}
	name, category, source := "", "", ""
	if info := db.LookupPart(app.ctx(), app.rebrick, req.Num); info != nil {
		name, category, source = info.Name, info.Category, info.Source
		pg.Rows = append(pg.Rows, []string{"Part #", info.Num}, []string{"Name", name}, []string{"Category", orDash(category)}, []string{"Details from", source})
		var names []string
		for _, c := range info.Colors {
			names = append(names, c.Name)
		}
		pg.Rows = append(pg.Rows, []string{"Comes in", colourSummary(names)})
		colorIDs := make([]int, 0, len(info.Colors))
		for _, c := range info.Colors {
			colorIDs = append(colorIDs, c.ID)
		}
		pg.Picture, pg.Caption = picture(app, img.PartURLs(info.Num, req.ColorID, colorIDs))
	} else {
		pg.Rows = append(pg.Rows, []string{"Part #", req.Num}, []string{"Details from", "not found in any source"})
		pg.Picture, pg.Caption = picture(app, img.PartURLs(req.Num, req.ColorID, nil))
	}
	if req.ColorID >= 0 {
		if c, ok := db.ColorByID(req.ColorID); ok {
			pg.Rows = append(pg.Rows, []string{"Colour", c.Name})
		}
	}
	owned, _ := db.OwnedPartsOf(req.Num)
	if len(owned) == 0 {
		pg.Rows = append(pg.Rows, []string{"You hold", "none"})
	} else {
		var parts []string
		for _, p := range owned {
			s := fmt.Sprintf("%s %d", orDash(p.ColorName), p.Qty)
			if p.IsLow() {
				s += " LOW"
			}
			parts = append(parts, s)
		}
		pg.Rows = append(pg.Rows, []string{"You hold", strings.Join(parts, ", ")})
	}
	if total, examples, err := db.SetsUsingPart(req.Num, 3); err == nil && total > 0 {
		var ex []string
		for _, e := range examples {
			ex = append(ex, fmt.Sprintf("%s (%d)", e.Name, e.Year))
		}
		pg.Rows = append(pg.Rows, []string{"Used in", fmt.Sprintf("%d set(s), e.g. %s", total, strings.Join(ex, "; "))})
	}
	pg.Rows = append(pg.Rows, priceRow(app, "PART", req.Num, req.ColorID))
	return pg
}

func colourSummary(names []string) string {
	switch {
	case len(names) == 0:
		return "no colour list"
	case len(names) <= 6:
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%d colours: %s, …", len(names), strings.Join(names[:6], ", "))
}

// priceRow shows the last stored price (never a fresh call; P fetches one).
func isNotFound(err error) bool { return errors.Is(err, bricklink.ErrNotFound) }

func priceRow(app *App, itemType, no string, colorID int) []string {
	blColor := 0
	if itemType == "PART" && colorID >= 0 {
		blColor, _ = app.legoDB.BLColorFor(colorID)
	}
	if itemType == "SET" && !strings.Contains(no, "-") {
		no += "-1"
	}
	cond := strings.ToUpper(config.Get(config.BricklinkCondition))
	p, err := app.legoDB.LatestPrice(itemType, no, blColor, cond)
	switch {
	case err != nil || p == nil:
		return []string{"BrickLink price", "not fetched — press P (uses one BrickLink call)"}
	case p.Missing:
		return []string{"BrickLink price", "no sales in BrickLink's window (checked " + p.FetchedAt.Format("2006-01-02") + ")"}
	}
	return []string{"BrickLink price", fmt.Sprintf("%.4f %s avg sold, %s (as of %s)", p.Avg, p.Currency, map[string]string{"N": "new", "U": "used"}[p.Cond], p.FetchedAt.Format("2006-01-02"))}
}

func buildSetDetail(app *App, req *detailReq) *detailPage {
	db := app.legoDB
	pg := &detailPage{Title: "Set " + req.Num}
	lk := db.LookupSet(app.ctx(), app.rebrick, req.Num)
	var imgURLs []string
	if lk.Found() {
		s := lk.Set
		pg.Rows = append(pg.Rows, []string{"Set #", s.Num}, []string{"Name", s.Name}, []string{"Theme", orDash(s.Theme)}, []string{"Year", strconv.Itoa(s.Year)}, []string{"Pieces", strconv.Itoa(s.Pieces)}, []string{"Details from", lk.Source})
		if s.ImgURL != "" {
			imgURLs = append(imgURLs, s.ImgURL)
		}
	} else {
		pg.Rows = append(pg.Rows, []string{"Set #", req.Num}, []string{"Details from", "not found — " + strings.Join(lk.Notes, " ")})
	}
	pg.Picture, pg.Caption = picture(app, imgURLs)
	if mine, err := db.GetSetByNum(collectionSetNum(req.Num)); err == nil && mine != nil {
		owned := fmt.Sprintf("x%d", mine.Qty)
		if mine.PartedOut {
			owned += ", parted out"
		}
		pg.Rows = append(pg.Rows, []string{"You own", owned})
	} else {
		pg.Rows = append(pg.Rows, []string{"You own", "not in your collection"})
	}
	pg.Rows = append(pg.Rows, setStatusRows(app, catalogSetNum(req.Num))...)
	if figs, err := db.MinifigsInSet(req.Num, 4); err == nil && len(figs) > 0 {
		pg.Rows = append(pg.Rows, []string{"Minifigures", strings.Join(figs, "; ")})
	}
	if inv := db.LookupSetInventory(app.ctx(), app.rebrick, req.Num); len(inv.Items) > 0 {
		if rep, err := db.MissingFor(inv.Items, 1); err == nil {
			pg.Rows = append(pg.Rows, []string{"Your loose parts", fmt.Sprintf("%d%% of this set's pieces (%d of %d), %d of %d lines complete", rep.Percent(), rep.PiecesHeld, rep.PiecesNeeded, rep.Complete, rep.Lines)})
		}
	}
	pg.Rows = append(pg.Rows, priceRow(app, "SET", req.Num, -1))
	return pg
}

// showMissing opens the Missing Parts screen for one copy of a set.
func showMissing(app *App, setNum string) {
	inv := app.legoDB.LookupSetInventory(app.ctx(), app.rebrick, setNum)
	if len(inv.Items) == 0 {
		app.setMsg(strings.Join(inv.Notes, " "), true)
		return
	}
	rep, err := app.legoDB.MissingFor(inv.Items, 1)
	if err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	name := ""
	if set, _ := app.legoDB.CatalogSet(setNum); set != nil {
		name = set.Name
	}
	app.legoMissing = &missingView{SetNum: setNum, Name: name, Report: rep, Source: inv.Source}
	app.goTo(scrLegoMissing)
}

// setStatusRows are the check, location and order facts for a set.
func setStatusRows(app *App, setNum string) [][]string {
	st := app.legoDB.GetSetState(setNum)
	var rows [][]string
	switch {
	case !st.Checked():
		rows = append(rows, []string{"Parts check", "not checked yet — press K"})
	case st.Incomplete():
		v := fmt.Sprintf("INCOMPLETE — %d missing", st.MissingQty)
		if st.OnOrderQty > 0 {
			v += fmt.Sprintf(", %d on order", st.OnOrderQty)
		}
		rows = append(rows, []string{"Parts check", v})
	default:
		rows = append(rows, []string{"Parts check", "COMPLETE"})
	}
	if c := st.LastCheck; c != nil {
		rows = append(rows, []string{"Last checked", fmt.Sprintf("%s by %s (%s)", c.FinishedAt.Local().Format("2 Jan 2006 15:04"), c.CheckedBy, c.Kind)})
	}
	if st.Location != "" || st.Condition != "" {
		rows = append(rows, []string{"Kept", strings.Trim(st.Location+" · "+st.Condition, " ·")})
	}
	return rows
}
