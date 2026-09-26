package uiapp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// The parts check: a set's whole parts list, every line starting as "have all".
// Mark what is missing (M) or extra (E), type a count, and finish (F): the check is
// recorded with who did it, the set's parts go into its own Part-DB location, extras
// become loose parts that remember this set, and the set is flagged INCOMPLETE if
// anything is short. The same screen does stock checks (a recount) and has a
// barcode-scanner mode (Z) where each scan adds one.

const scrSetCheck = "set_check"

type checkState struct {
	check     *lego.SetCheck
	setName   string
	undo      []lego.CheckLine // line snapshots, newest last
	undoIdx   []int
	spares    map[int]string // line index → "2 spare (10696)"
	finishing bool           // Finish Check pressed, Part-DB sync running in the background — see finishCheck
}

type setCheckScreen struct {
	base
	sel, top      int
	filter        string
	filtering     bool
	prompt, input string // an open number prompt: "missing", "extra", "have"
	scanner       bool
	scanBuf       string
	flash         string
}

// startCheck opens the check for a set (resuming a saved draft).
func startCheck(app *App, setNum, kind string) {
	perm := "sets.check"
	if kind == lego.CheckRecount {
		perm = "sets.stocktake"
	}
	if !app.require(perm, "SET_CHECK "+setNum) {
		return
	}
	by := "local"
	if app.session != nil {
		by = app.session.Username
	}
	c, err := app.legoDB.NewCheck(app.ctx(), app.rebrick, setNum, kind, by)
	if err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	c.CheckedBy = by
	name := ""
	if s, _ := app.legoDB.CatalogSet(setNum); s != nil {
		name = s.Name
	} else if s, _ := app.legoDB.GetSetByNum(collectionSetNum(setNum)); s != nil {
		name = s.Name
	}
	app.checking = &checkState{check: c, setName: name}
	app.checking.findSpares(app)
	app.goTo(scrSetCheck)
}

func (st *checkState) findSpares(app *App) {
	st.spares = map[int]string{}
	for i, l := range st.check.Lines {
		if l.Missing() == 0 {
			continue
		}
		sp, _ := app.legoDB.SparesFor(l.PartNum, l.ColorID)
		var parts []string
		for _, s := range sp {
			if s.OriginSet == st.check.SetNum {
				continue
			}
			where := "loose"
			if s.OriginSet != "" {
				where = s.OriginSet
			}
			parts = append(parts, fmt.Sprintf("%d in %s", s.Qty, where))
		}
		if len(parts) > 0 {
			st.spares[i] = "spare: " + strings.Join(parts, ", ")
		}
	}
}

func (s *setCheckScreen) PanelID() string { return "SETCHK" }
func (s *setCheckScreen) Title() string   { return "Parts Check" }
func (s *setCheckScreen) capturing() bool { return true } // every letter is a command here
func (s *setCheckScreen) OnEnter(app *App) {
	s.sel, s.top, s.filter, s.filtering, s.prompt, s.input, s.scanner, s.scanBuf, s.flash = 0, 0, "", false, "", "", false, "", ""
}
func (s *setCheckScreen) FKeys() [][2]string {
	return [][2]string{{"F3", "Exit"}, {"F12", "Cancel"}}
}

// visible is the indexes of the lines the filter lets through.
func (s *setCheckScreen) visible(app *App) []int {
	var out []int
	f := strings.ToLower(strings.TrimSpace(s.filter))
	for i, l := range app.checking.check.Lines {
		if f == "" || f == "missing" && l.Missing() > 0 || f == "extra" && l.Extra > 0 ||
			strings.Contains(strings.ToLower(l.PartNum+" "+l.PartName+" "+l.ColorName+" "+l.Category), f) {
			out = append(out, i)
		}
	}
	return out
}

func (s *setCheckScreen) rows(app *App) int {
	h := app.height
	if h <= 0 {
		h = 25
	}
	return max(3, h-15)
}

func (s *setCheckScreen) Body(app *App) string {
	t := app.theme
	st := app.checking
	if st == nil {
		return t.Muted.Render("Nothing to check — open a set first.")
	}
	c := st.check
	pieces, have, missing, extra, missLines := c.Totals()
	kind := "Parts check"
	if c.Kind == lego.CheckRecount {
		kind = "Stock check"
	}
	head := t.Strong.Render(fmt.Sprintf("%s: %s %s", kind, c.SetNum, st.setName))
	status := t.Success.Render("COMPLETE")
	if missing > 0 {
		status = t.Danger.Render(fmt.Sprintf("INCOMPLETE — %d missing (%d lines)", missing, missLines))
	}
	sum := fmt.Sprintf("%d lines · %d pieces · have %d · ", len(c.Lines), pieces, have) + status
	if extra > 0 {
		sum += t.Accent.Render(fmt.Sprintf(" · %d extra", extra))
	}
	if n := len(st.spares); n > 0 {
		sum += t.Warning.Render(fmt.Sprintf(" · %d line(s) have spares elsewhere", n))
	}
	vis := s.visible(app)
	s.sel = min(s.sel, max(0, len(vis)-1))
	n := s.rows(app)
	if s.sel < s.top {
		s.top = s.sel
	}
	if s.sel >= s.top+n {
		s.top = s.sel - n + 1
	}
	end := min(len(vis), s.top+n)
	rows := make([][]string, 0, end-s.top)
	for vi := s.top; vi < end; vi++ {
		i := vis[vi]
		l := c.Lines[i]
		mark := "  "
		if vi == s.sel {
			mark = "▶ "
		}
		miss, ex := "", ""
		if m := l.Missing(); m > 0 {
			miss = "M" + strconv.Itoa(m)
		}
		if l.Extra > 0 {
			ex = "E" + strconv.Itoa(l.Extra)
		}
		note := st.spares[i]
		if l.Optional {
			if note != "" {
				note = "optional · " + note
			} else {
				note = "optional"
			}
		}
		rows = append(rows, []string{mark, l.PartNum, orDash(l.ColorName), l.PartName, strconv.Itoa(l.Need), strconv.Itoa(l.Have), miss, ex, note})
	}
	title := fmt.Sprintf("%d of %d line(s)", len(vis), len(c.Lines))
	if s.filter != "" {
		title += " matching " + strconv.Quote(s.filter)
	}
	table := ui.RenderColumns(t.WithWidth(t.W()), []string{"", "Part", "Colour", "Name", "Need", "Have", "Miss", "Extra", ""}, rows, title)
	var foot string
	switch {
	case s.prompt != "":
		foot = t.Accent.Render(fmt.Sprintf("%s: %s_", map[string]string{"missing": "How many are missing", "extra": "How many extra", "have": "How many do you have", "take": "How many to take from spares"}[s.prompt], s.input)) +
			t.Muted.Render("   Enter ok · Esc cancel")
	case s.filtering:
		foot = t.Accent.Render("Filter: "+s.filter+"_") + t.Muted.Render("   part, colour, name, category, 'missing' or 'extra' · Enter done")
	case s.scanner:
		foot = t.Accent.Render("SCANNER MODE — scan or type a part number + Enter adds one: "+s.scanBuf+"_") + t.Muted.Render("   Z/Esc leaves")
	default:
		foot = t.Muted.Render("H have all · M missing · E extra · 0-9 count · A all have · T take spare") + "\n" +
			t.Muted.Render("U undo · / filter · Z scanner · S save · F finish · Esc leave · O optional")
	}
	if s.flash != "" {
		foot = s.flash + "\n" + foot
	}
	var fl []string
	for _, l := range strings.Split(foot, "\n") {
		fl = append(fl, ansi.Truncate(l, t.W(), "…"))
	}
	return head + "\n" + ansi.Truncate(sum, t.W(), "…") + "\n" + table + "\n" + strings.Join(fl, "\n")
}

func (s *setCheckScreen) HandleKey(app *App, msg tea.KeyMsg) {
	st := app.checking
	if st == nil {
		app.onBack()
		return
	}
	s.flash = ""
	vis := s.visible(app)
	cur := -1
	if s.sel < len(vis) {
		cur = vis[s.sel]
	}
	switch {
	case s.prompt != "":
		s.promptKey(app, msg, cur)
		return
	case s.filtering:
		switch msg.Type {
		case tea.KeyEnter, tea.KeyEsc:
			s.filtering = false
		case tea.KeyBackspace:
			if s.filter != "" {
				s.filter = s.filter[:len(s.filter)-1]
			}
		case tea.KeyRunes, tea.KeySpace:
			s.filter += string(msg.Runes)
			s.sel = 0
		}
		return
	case s.scanner:
		s.scanKey(app, msg)
		return
	}
	switch msg.Type {
	case tea.KeyEsc:
		s.leave(app)
		return
	case tea.KeyUp:
		s.sel = max(0, s.sel-1)
		return
	case tea.KeyDown:
		s.sel = min(len(vis)-1, s.sel+1)
		return
	case tea.KeyPgUp:
		s.sel = max(0, s.sel-s.rows(app))
		return
	case tea.KeyPgDown:
		s.sel = min(len(vis)-1, s.sel+s.rows(app))
		return
	case tea.KeyEnter:
		if cur >= 0 {
			s.prompt, s.input = "have", ""
		}
		return
	}
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return
	}
	r := msg.Runes[0]
	switch {
	case r >= '0' && r <= '9' && cur >= 0:
		s.prompt, s.input = "have", string(r)
	case (r == 'h' || r == 'H') && cur >= 0:
		st.set(cur, func(l *lego.CheckLine) { l.Have = l.Need })
		s.sel = min(len(vis)-1, s.sel+1)
	case (r == 'm' || r == 'M') && cur >= 0:
		s.prompt, s.input = "missing", ""
	case (r == 'e' || r == 'E') && cur >= 0:
		s.prompt, s.input = "extra", ""
	case (r == 't' || r == 'T') && cur >= 0:
		if st.spares[cur] == "" {
			app.setMsg("No spares of this part anywhere.", true)
			return
		}
		s.prompt, s.input = "take", strconv.Itoa(st.check.Lines[cur].Missing())
	case r == 'a' || r == 'A':
		for i := range st.check.Lines {
			st.set(i, func(l *lego.CheckLine) { l.Have = l.Need })
		}
		app.setMsg("Every line marked as have-all.", false)
	case (r == 'o' || r == 'O') && cur >= 0:
		l := st.check.Lines[cur]
		next := !l.Optional
		if err := app.legoDB.SetOptional(l.PartNum, next); err != nil {
			app.setMsg(err.Error(), true)
			return
		}
		st.set(cur, func(l *lego.CheckLine) { l.Optional = next })
		word := "required again"
		if next {
			word = "optional \u2014 won't count toward missing or completion"
		}
		app.setMsg(fmt.Sprintf("%s marked %s.", l.PartNum, word), false)
	case r == 'u' || r == 'U':
		st.undoLast()
	case r == '/':
		s.filtering = true
	case r == 'z' || r == 'Z':
		s.scanner, s.scanBuf = true, ""
	case r == 's' || r == 'S':
		if err := app.legoDB.SaveCheck(st.check); err != nil {
			app.setMsg(err.Error(), true)
			return
		}
		app.setMsg("Saved — open the check again to carry on.", false)
		app.checking = nil
		app.onBack()
	case r == 'f' || r == 'F':
		finishCheck(app)
	case r == 'q' || r == 'Q':
		s.leave(app)
	}
}

func (s *setCheckScreen) leave(app *App) {
	app.setMsg("Check not saved (S saves it for later, F finishes).", false)
	app.checking = nil
	app.onBack()
}

func (s *setCheckScreen) promptKey(app *App, msg tea.KeyMsg, cur int) {
	st := app.checking
	switch msg.Type {
	case tea.KeyEsc:
		s.prompt = ""
		return
	case tea.KeyBackspace:
		if s.input != "" {
			s.input = s.input[:len(s.input)-1]
		}
		return
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			if r >= '0' && r <= '9' && len(s.input) < 6 {
				s.input += string(r)
			}
		}
		return
	case tea.KeyEnter:
	default:
		return
	}
	n := 1
	if s.input != "" {
		n, _ = strconv.Atoi(s.input)
	}
	p := s.prompt
	s.prompt = ""
	if cur < 0 {
		return
	}
	switch p {
	case "missing":
		st.set(cur, func(l *lego.CheckLine) { l.Have = max(0, l.Need-n) })
	case "extra":
		st.set(cur, func(l *lego.CheckLine) { l.Extra = n })
	case "have":
		st.set(cur, func(l *lego.CheckLine) {
			if n > l.Need { // counted more than the set needs: the rest are extras
				l.Have, l.Extra = l.Need, n-l.Need
			} else {
				l.Have = n
			}
		})
	case "take":
		l := st.check.Lines[cur]
		if st.check.ID == 0 || st.check.Status != lego.StatusDone {
			// Not finished yet: just count them in; FinishCheck records it, TakeSpare needs a finished check.
			if _, err := app.legoDB.TakeSpareLoose(l.PartNum, l.ColorID, min(n, l.Missing()), st.check.SetNum); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			st.set(cur, func(l *lego.CheckLine) { l.Have = min(l.Need, l.Have+n) })
			st.findSpares(app)
			app.setMsg(fmt.Sprintf("Took %d %s from spares.", n, l.PartNum), false)
		}
	}
}

// scanKey handles barcode-scanner mode: characters build a code, Enter matches it
// to a line by part number (or element id) and adds one.
func (s *setCheckScreen) scanKey(app *App, msg tea.KeyMsg) {
	st := app.checking
	switch msg.Type {
	case tea.KeyEsc:
		s.scanner = false
		return
	case tea.KeyBackspace:
		if s.scanBuf != "" {
			s.scanBuf = s.scanBuf[:len(s.scanBuf)-1]
		}
		return
	case tea.KeyRunes:
		if len(msg.Runes) == 1 && (msg.Runes[0] == 'z' || msg.Runes[0] == 'Z') && s.scanBuf == "" {
			s.scanner = false
			return
		}
		s.scanBuf += string(msg.Runes)
		return
	case tea.KeyEnter:
	default:
		return
	}
	code := strings.TrimSpace(s.scanBuf)
	s.scanBuf = ""
	if code == "" {
		return
	}
	part, color := code, -2
	if p, c, ok := app.legoDB.ElementPart(code); ok { // a LEGO element id names part and colour
		part, color = p, c
	}
	for i, l := range st.check.Lines {
		if strings.EqualFold(l.PartNum, part) && (color == -2 || l.ColorID == color) && l.Have < l.Need {
			st.set(i, func(l *lego.CheckLine) { l.Have++ })
			for vi, idx := range s.visible(app) {
				if idx == i {
					s.sel = vi
				}
			}
			s.flash = app.theme.Success.Render(fmt.Sprintf("✓ %s %s — %d of %d", l.PartNum, l.ColorName, l.Have+1, l.Need))
			return
		}
	}
	s.flash = "\a" + app.theme.Danger.Render("✗ "+code+" is not in this set (or that line is already complete)")
}

func (st *checkState) set(i int, f func(l *lego.CheckLine)) {
	st.undo = append(st.undo, st.check.Lines[i])
	st.undoIdx = append(st.undoIdx, i)
	if len(st.undo) > 500 {
		st.undo, st.undoIdx = st.undo[1:], st.undoIdx[1:]
	}
	f(&st.check.Lines[i])
}

func (st *checkState) undoLast() {
	if n := len(st.undo); n > 0 {
		st.check.Lines[st.undoIdx[n-1]] = st.undo[n-1]
		st.undo, st.undoIdx = st.undo[:n-1], st.undoIdx[:n-1]
	}
}

// finishCheck records the check, pushes the set to Part-DB (when a token is set),
// adds extras as loose parts, and flags or clears the set. The Part-DB push (up to
// 5 minutes) runs off the key-handling path via startBusy, so the screen stays
// responsive; app.checking is only cleared once it's done, so you're never
// navigated away mid-sync and left unsure whether it worked — the check screen (with
// a spinner on its message line) stays up until the result is known.
func finishCheck(app *App) {
	st := app.checking
	if st.finishing { // a repeat F while the Part-DB push is still running: ignore it
		return
	}
	c := st.check
	extras, err := app.legoDB.FinishCheck(c)
	if err != nil {
		app.setMsg("Could not finish: "+err.Error(), true)
		return
	}
	pieces, _, missing, extra, _ := c.Totals()
	user, role := c.CheckedBy, ""
	if app.session != nil {
		role = app.session.Role
	}
	action := "SET_CHECKED"
	if c.Kind == lego.CheckRecount {
		action = "STOCK_CHECK"
	}
	app.audit.Log(user, role, action, "SUCCESS", fmt.Sprintf("set=%s pieces=%d missing=%d extra=%d", c.SetNum, pieces, missing, extra))
	msg := fmt.Sprintf("%s: checked by %s — ", c.SetNum, user)
	if missing > 0 {
		msg += fmt.Sprintf("INCOMPLETE, %d missing (Missing Parts → P prices, O order).", missing)
		app.emit("set_incomplete", map[string]any{"set": c.SetNum, "name": st.setName, "missing": missing, "by": user})
		app.notifyEvent("set_incomplete", fmt.Sprintf("Set %s %s is missing %d part(s)", c.SetNum, st.setName, missing), "")
		app.queueAlert("Missing parts", fmt.Sprintf("Set %s is missing %d part(s).", c.SetNum, missing))
	} else {
		msg += "COMPLETE."
		app.emit("set_complete", map[string]any{"set": c.SetNum, "name": st.setName, "by": user})
	}
	if extra > 0 {
		msg += fmt.Sprintf(" %d extra part(s) added to your loose parts.", extra)
	}
	if app.pdbw == nil || !app.pdbw.API().Enabled() {
		msg += " (Part-DB not updated: no API token — `wms lego sync-parts` later.)"
		app.checking = nil
		app.onBack()
		app.setMsg(msg, missing > 0)
		return
	}
	st.finishing = true
	app.startBusy("Updating Part-DB…", func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		syncer := &lego.PartSyncer{Lego: app.legoDB, Writer: app.pdbw}
		res, err := syncer.PushSet(ctx, c, st.setName)
		switch {
		case err != nil:
			msg += " Part-DB not updated: " + err.Error()
		case len(res.Errors) > 0:
			msg += fmt.Sprintf(" Part-DB: %d line(s) failed (%v).", len(res.Errors), res.Errors[0])
		default:
			msg += fmt.Sprintf(" Part-DB: %d line(s) in location %s.", res.Lines, c.SetNum)
		}
		for _, p := range extras {
			_, _ = syncer.Push(ctx, p)
		}
		return checkFinishedMsg{msg: msg, isErr: missing > 0}
	})
}

// checkFinishedMsg reports the background Part-DB push finishCheck kicked off;
// handled in App.Update.
type checkFinishedMsg struct {
	msg   string
	isErr bool
}

func (a *App) checkFinished(m checkFinishedMsg) {
	a.stopBusy()
	a.checking = nil
	a.onBack()
	a.setMsg(m.msg, m.isErr)
}
