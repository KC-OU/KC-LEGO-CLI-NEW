package uiapp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// scrPick is one reusable "choose from a list" screen. What it is choosing
// (a colour, a Part-DB category) lives in App.pick; the flow that started it
// supplies the items and the callbacks. It shows a numbered list, narrows it as
// you type, and — where the caller allows — accepts text that matches nothing.

const (
	scrPick        = "pick"
	pickVisibleMax = 12
)

type pickItem struct {
	Key   string // what the caller gets back (a colour id, a category id)
	Label string
	RGB   string // hex colour for a swatch, "" for none
}

type pickState struct {
	Header     string // first line above the list, e.g. "Colours 3001 comes in:"
	Prompt     string
	Items      []pickItem
	AllowFree  bool   // accept text that matches nothing (a colour name, a new category)
	FreeHint   string // what free text means here
	BlankLabel string // when set, an empty answer is valid and means this ("no colour")
	OnPick     func(app *App, it pickItem)
	OnFree     func(app *App, text string) // also called with "" for a blank answer
}

func pickScreen() screenModel {
	var self *formScreen
	self = &formScreen{
		panelID:      "PICK",
		title:        "Choose",
		writeGated:   true,
		deniedAction: "PICK_ITEM",
		build: func(app *App) []ui.Field {
			if app.pick == nil {
				return []ui.Field{{Label: "Nothing to choose — go back", Protected: true}}
			}
			return []ui.Field{{Label: app.pick.Prompt}}
		},
		preamble: func(app *App) string {
			if app.pick == nil || self.fl == nil {
				return ""
			}
			return pickList(app.theme, app.pick, self.fl.Value(0), pickRows(app.height))
		},
		submit: func(app *App, values []string) { pickSubmit(app, strings.TrimSpace(values[0])) },
	}
	return self
}

// pickMatches returns the items whose label contains typed (all of them when
// typed is empty or a number), keeping each item's fixed list position.
func pickMatches(st *pickState, typed string) []int {
	typed = strings.ToLower(strings.TrimSpace(typed))
	var out []int
	if _, err := strconv.Atoi(typed); typed == "" || err == nil {
		for i := range st.Items {
			out = append(out, i)
		}
		return out
	}
	for i, it := range st.Items {
		if strings.Contains(strings.ToLower(it.Label), typed) {
			out = append(out, i)
		}
	}
	return out
}

// pickRows is how many choices fit on a screen of this height beside the header, hint and prompt
// (about fifteen rows), so the top of the screen is never pushed off by a long list.
func pickRows(height int) int {
	if height <= 0 {
		return pickVisibleMax
	}
	return max(3, min(pickVisibleMax, height-15))
}

func pickList(t ui.Theme, st *pickState, typed string, rows int) string {
	var b strings.Builder
	if st.Header != "" {
		b.WriteString(t.Muted.Render(st.Header) + "\n")
	}
	idx := pickMatches(st, typed)
	for n, i := range idx {
		if n == rows {
			b.WriteString(t.Muted.Render(fmt.Sprintf("     … and %d more — keep typing to narrow the list", len(idx)-rows)) + "\n")
			break
		}
		it := st.Items[i]
		swatch := "  "
		if it.RGB != "" && !t.Mono {
			swatch = ui.Swatch(it.RGB)
		}
		fmt.Fprintf(&b, " %s %s %s\n", t.Strong.Render(fmt.Sprintf("%3d.", i+1)), swatch, t.Text.Render(it.Label))
	}
	if len(idx) == 0 {
		b.WriteString(t.Muted.Render("     nothing matches") + "\n")
	}
	hint := "Type a number, or part of a name to narrow the list."
	if st.AllowFree && st.FreeHint != "" {
		hint += " " + st.FreeHint
	}
	if st.BlankLabel != "" {
		hint += " Blank = " + st.BlankLabel + "."
	}
	b.WriteString(t.Muted.Render(hint))
	return b.String()
}

func pickSubmit(app *App, text string) {
	st := app.pick
	if st == nil {
		app.onBack()
		return
	}
	if text == "" {
		if st.BlankLabel != "" && st.OnFree != nil {
			st.OnFree(app, "")
			return
		}
		app.setMsg("Type a number from the list, or part of a name.", true)
		return
	}
	if n, err := strconv.Atoi(text); err == nil {
		if n < 1 || n > len(st.Items) {
			app.setMsg(fmt.Sprintf("There is no item %d — choose 1 to %d.", n, len(st.Items)), true)
			return
		}
		st.OnPick(app, st.Items[n-1])
		return
	}
	matches := pickMatches(st, text)
	for _, i := range matches { // an exact name wins over longer names containing it ("Red" vs "Dark Red")
		if strings.EqualFold(st.Items[i].Label, text) {
			st.OnPick(app, st.Items[i])
			return
		}
	}
	switch {
	case len(matches) == 1:
		st.OnPick(app, st.Items[matches[0]])
	case len(matches) > 1:
		app.setMsg(fmt.Sprintf("%d items match %q — type more of the name, or choose a number.", len(matches), text), true)
	case st.AllowFree && st.OnFree != nil:
		st.OnFree(app, text)
	default:
		app.setMsg(fmt.Sprintf("Nothing matches %q — choose a number from the list.", text), true)
	}
}

// startPick shows the picker for st; the calling flow continues from its callbacks.
func startPick(app *App, st *pickState) {
	app.pick = st
	app.goTo(scrPick)
}
