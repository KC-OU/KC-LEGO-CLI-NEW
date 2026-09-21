package uiapp

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

const (
	scrLegoHub         = "lego_hub"
	scrLegoSetSearch   = "lego_set_search"
	scrLegoSetResults  = "lego_set_results"
	scrLegoSetLookup   = "lego_set_lookup"
	scrLegoSetDetail   = "lego_set_detail"
	scrLegoSetFind     = "lego_set_find"
	scrLegoSetFound    = "lego_set_found"
	scrLegoSetFoundAdd = "lego_set_found_add"
	scrLegoPartSearch  = "lego_part_search"
	scrLegoPartResults = "lego_part_results"
	scrLegoPartAdd     = "lego_part_add"
	scrLegoPartOwned   = "lego_part_owned"
)

func legoHubScreen() screenModel {
	return &menuScreen{
		panelID: "LEGO",
		title:   "LEGO Collection",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Search My Sets", Go: func(app *App) { app.goTo(scrLegoSetSearch) }},
				{Key: "2", Label: "Search Sets (offline catalog first)", Go: func(app *App) { app.goTo(scrLegoSetFind) }},
				{Key: "3", Label: "Search Parts (offline catalog first)", Go: func(app *App) { app.goTo(scrLegoPartSearch) }},
				{Key: "4", Label: "Add / Update a Set", Go: func(app *App) { app.goTo(scrLegoSetAdd) }},
				{Key: "5", Label: "Add / Update an Owned Part (for sync to Part-DB)", Go: func(app *App) { app.goTo(scrLegoPartAdd) }},
				{Key: "6", Label: "List Owned Parts", Go: func(app *App) { app.goTo(scrLegoPartOwned) }},
				{Key: "7", Label: "Collection Stats", Go: func(app *App) { app.goTo(scrLegoStats) }},
				{Key: "8", Label: "Missing Parts for a Set", Go: func(app *App) { app.goTo(scrLegoMissingAsk) }},
				{Key: "9", Label: "BrickLink Lookup by Number (price, live)", Go: func(app *App) { app.goTo(scrLegoBLAsk) }},
				{Key: "H", Label: "History of Changes and Growth Chart", Go: func(app *App) { app.goTo(scrLegoHistory) }},
				{Key: "B", Label: "What Can I Build? (from your loose parts)", Go: func(app *App) { app.goTo(scrLegoBuild) }},
				{Key: "D", Label: "Part / Set Detail with picture", Go: func(app *App) { app.goTo(scrLegoDetailAsk) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
		intro: lowStockNote,
	}
}

// lowStockNote is the LEGO hub's badge: how many parts you asked to keep a
// minimum of have fallen below it. "LOW" is spelled out, so it never rests on colour.
func lowStockNote(app *App) string {
	low, err := app.legoDB.LowStock()
	if err != nil || len(low) == 0 {
		return ""
	}
	return app.theme.Warning.Render(fmt.Sprintf(" LOW STOCK: %d part(s) are below the minimum you set ", len(low))) +
		app.theme.Muted.Render("  -> 6 List Owned Parts, then / then low")
}

func legoSetSearchScreen() screenModel {
	return &formScreen{
		panelID: "LEGSCH",
		title:   "Search My LEGO Sets",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Search term (blank = all)"}}
		},
		submit: func(app *App, values []string) {
			app.legoSearchTerm = values[0]
			app.goTo(scrLegoSetResults)
		},
	}
}

func legoSetResultsScreen() screenModel {
	return &tableScreen{
		panelID: "LEGRES",
		title:   "My Sets",
		columns: []string{"Set #", "Name", "Theme", "Year", "Qty", "Parts", "Parted Out"},
		fetch: func(app *App) ([][]string, string, error) {
			sets, err := app.legoDB.SearchSets(app.legoSearchTerm)
			if err != nil {
				return nil, "", err
			}
			rows := make([][]string, len(sets))
			for i, s := range sets {
				rows[i] = []string{s.SetNum, s.Name, s.Theme, strconv.Itoa(s.Year), strconv.Itoa(s.Qty), strconv.Itoa(s.PartsQty), yesNo(s.PartedOut)}
			}
			title := fmt.Sprintf("%d set(s) — press I to inspect a set number", len(sets))
			return rows, title, nil
		},
		extra: func(app *App, msg tea.KeyMsg) {
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && (msg.Runes[0] == 'i' || msg.Runes[0] == 'I') {
				app.goTo(scrLegoSetLookup)
			}
		},
	}
}

func yesNo(b bool) string {
	if b {
		return "YES"
	}
	return "NO"
}

func legoSetLookupScreen() screenModel {
	return &formScreen{
		panelID: "LEGINS",
		title:   "Inspect Set",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Set Number"}}
		},
		submit: func(app *App, values []string) {
			s, err := app.legoDB.GetSetByNum(values[0])
			if err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.legoSetDetailRows = [][]string{
				{"Set Number", s.SetNum},
				{"Name", s.Name},
				{"Theme", s.Theme},
				{"Year", strconv.Itoa(s.Year)},
				{"Instruction Book #", s.InstructionBookNumber},
				{"Instruction Book Count", strconv.Itoa(s.InstructionBookCount)},
				{"Qty Owned", strconv.Itoa(s.Qty)},
				{"Parts Qty", strconv.Itoa(s.PartsQty)},
				{"Parted Out", yesNo(s.PartedOut)},
				{"Updated", s.UpdatedAt.Format("2006-01-02 15:04")},
			}
			app.goTo(scrLegoSetDetail)
		},
	}
}

func legoSetDetailScreen() screenModel {
	return &tableScreen{
		panelID: "LEGDET",
		title:   "Set Detail",
		columns: []string{"Field", "Value"},
		boxed:   true,
		fetch:   func(app *App) ([][]string, string, error) { return app.legoSetDetailRows, "Set Detail", nil },
	}
}

func legoPartOwnedScreen() screenModel {
	return &tableScreen{
		panelID: "LEGPOW",
		title:   "Owned Parts",
		columns: []string{"Part #", "Colour", "Name", "Category", "Qty", "Part-DB"},
		fetch: func(app *App) ([][]string, string, error) {
			owned, err := app.legoDB.ListOwnedParts()
			if err != nil {
				return nil, "", err
			}
			rows := make([][]string, len(owned))
			for i, p := range owned {
				synced, colour := "not yet", p.ColorName
				if p.SyncedPartID > 0 {
					synced = "#" + strconv.Itoa(p.SyncedPartID)
				}
				if colour == "" {
					colour = "—"
				}
				qty := strconv.Itoa(p.Qty)
				if p.IsLow() {
					qty += fmt.Sprintf(" LOW (min %d)", p.MinQty)
				}
				rows[i] = []string{p.PartNum, colour, p.Name, p.Category, qty, synced}
			}
			title := fmt.Sprintf("%d owned part(s) — / filter", len(owned))
			if pending := countUnsynced(owned); pending > 0 {
				title += fmt.Sprintf(" — %d not in Part-DB yet, run `wms lego sync-parts`", pending)
			}
			return rows, title, nil
		},
	}
}

func legoPartSearchScreen() screenModel {
	return &formScreen{
		panelID: "LEGPCH",
		title:   "Search LEGO Parts",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Search term (part name or number)"}}
		},
		submit: func(app *App, values []string) {
			app.legoSearchTerm = values[0]
			app.goTo(scrLegoPartResults)
		},
	}
}

func legoPartResultsScreen() screenModel {
	return &tableScreen{
		panelID: "LEGPRS",
		title:   "Part Search Results",
		columns: []string{"Part #", "Name", "Category"},
		fetch: func(app *App) ([][]string, string, error) {
			res := app.legoDB.FindParts(app.ctx(), app.rebrick, app.legoSearchTerm)
			if len(res.Notes) > 0 {
				app.setMsg(strings.Join(res.Notes, " "), true)
			}
			rows := make([][]string, len(res.Hits))
			for i, p := range res.Hits {
				rows[i] = []string{p.Num, p.Name, p.Category}
			}
			return rows, fmt.Sprintf("%d result(s) (%s) — D detail, / filter", len(res.Hits), res.Source), nil
		},
		extra: func(app *App, msg tea.KeyMsg) {
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && (msg.Runes[0] == 'd' || msg.Runes[0] == 'D') {
				app.goTo(scrLegoDetailAsk)
			}
		},
	}
}

// foundSet is one row of a set search — from the live Rebrickable API or,
// failing that, the imported local catalog — kept on the App so the "add to
// collection" step can pick one by its Set # without a second lookup.
type foundSet struct {
	SetNum, Name, Theme string
	Year, Pieces        int
}

// collectionSetNum converts a Rebrickable set number ("75192-1") to the form
// the collection stores ("75192", the legacy KC-LEGO-CLI format); other
// versions of a set ("75192-2") keep their suffix so they stay distinct.
func collectionSetNum(n string) string { return strings.TrimSuffix(strings.TrimSpace(n), "-1") }

func legoSetFindScreen() screenModel {
	return &formScreen{
		panelID: "LEGSFD",
		title:   "Search LEGO Sets",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Search term (set number, name or theme)"}}
		},
		submit: func(app *App, values []string) {
			if strings.TrimSpace(values[0]) == "" {
				app.setMsg("Enter a set number, name or theme to search for.", true)
				return
			}
			app.legoSearchTerm = values[0]
			app.goTo(scrLegoSetFound)
		},
	}
}

func legoSetFoundScreen() screenModel {
	return &tableScreen{
		panelID: "LEGSFR",
		title:   "Set Search Results",
		columns: []string{"Set #", "Name", "Theme", "Year", "Pieces", "Owned"},
		fetch: func(app *App) ([][]string, string, error) {
			res := app.legoDB.FindSets(app.ctx(), app.rebrick, app.legoSearchTerm)
			if len(res.Notes) > 0 {
				app.setMsg(strings.Join(res.Notes, " "), true)
			}
			found := make([]foundSet, len(res.Hits))
			for i, h := range res.Hits {
				found[i] = foundSet{SetNum: h.Num, Name: h.Name, Theme: h.Theme, Year: h.Year, Pieces: h.Pieces}
			}
			app.legoFound = found
			source := res.Source

			rows := make([][]string, len(found))
			for i, f := range found {
				owned := "—"
				if mine, err := app.legoDB.GetSetByNum(collectionSetNum(f.SetNum)); err == nil && mine != nil {
					owned = "x" + strconv.Itoa(mine.Qty)
				}
				year, pieces := "", ""
				if f.Year > 0 {
					year = strconv.Itoa(f.Year)
				}
				if f.Pieces > 0 {
					pieces = strconv.Itoa(f.Pieces)
				}
				theme := f.Theme
				if theme == "" {
					theme = "—"
				}
				rows[i] = []string{f.SetNum, f.Name, theme, year, pieces, owned}
			}
			return rows, fmt.Sprintf("%d result(s) (%s) — A add, D detail, / filter", len(found), source), nil
		},
		extra: func(app *App, msg tea.KeyMsg) {
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && (msg.Runes[0] == 'd' || msg.Runes[0] == 'D') {
				app.goTo(scrLegoDetailAsk)
				return
			}
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && (msg.Runes[0] == 'a' || msg.Runes[0] == 'A') && len(app.legoFound) > 0 {
				app.goTo(scrLegoSetFoundAdd)
			}
		},
	}
}

func legoSetFoundAddScreen() screenModel {
	return &formScreen{
		panelID:      "LEGSFA",
		title:        "Add Found Set to Collection",
		writeGated:   true,
		deniedAction: "ADD_LEGO_SET",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Set # (from the results)"}}
		},
		submit: func(app *App, values []string) { startSetDraft(app, values[0]) },
	}
}

func countUnsynced(owned []lego.OwnedPart) int {
	n := 0
	for _, p := range owned {
		if p.SyncedPartID == 0 {
			n++
		}
	}
	return n
}

func legoStatsScreen() screenModel {
	return &tableScreen{
		panelID: "LEGSTA",
		title:   "Collection Stats",
		columns: []string{"", ""},
		boxed:   true,
		fetch: func(app *App) ([][]string, string, error) {
			st, err := app.legoDB.Stats()
			if err != nil {
				return nil, "", err
			}
			top := func(cs []lego.Count) string {
				if len(cs) == 0 {
					return "—"
				}
				parts := make([]string, 0, 5)
				for _, c := range cs[:min(len(cs), 5)] {
					parts = append(parts, fmt.Sprintf("%s %d", c.Name, c.N))
				}
				return strings.Join(parts, ", ")
			}
			low := "none"
			if st.LowStock > 0 {
				low = fmt.Sprintf("%d LOW", st.LowStock)
			}
			return [][]string{
				{"Sets", fmt.Sprintf("%d title(s), %d cop(ies), %d in parted-out sets", st.SetTitles, st.SetCopies, st.PartedOut)},
				{"Pieces in sets", strconv.Itoa(st.SetPieces)},
				{"Loose parts", fmt.Sprintf("%d piece(s), %d line(s), %d distinct part(s)", st.LoosePieces, st.PartLines, st.DistinctParts)},
				{"Low stock", low},
				{"Sets by theme", top(st.Themes)},
				{"Loose pieces by colour", top(st.Colours)},
				{"Loose pieces by category", top(st.Categories)},
			}, "Collection at a glance", nil
		},
	}
}

// missingView is one "what does this set still need" result.
type missingView struct {
	SetNum string
	Name   string
	Report *lego.MissingReport
	Source string // where the parts list came from
}

func legoMissingAskScreen() screenModel {
	return &formScreen{
		panelID: "LEGMIS",
		title:   "Missing Parts for a Set",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Set number (e.g. 75192)"}, {Label: "Copies to build (blank = 1)"}}
		},
		preamble: func(app *App) string {
			return app.theme.Muted.Render("Compares the set's parts list with the loose parts you hold, matching part and colour.")
		},
		submit: func(app *App, values []string) {
			num := strings.TrimSpace(values[0])
			if num == "" {
				app.setMsg("Enter a set number.", true)
				return
			}
			copies := 1
			if c := strings.TrimSpace(values[1]); c != "" {
				n, err := strconv.Atoi(c)
				if err != nil || n < 1 || n > 99 {
					app.setMsg("Copies must be a whole number from 1 to 99.", true)
					return
				}
				copies = n
			}
			inv := app.legoDB.LookupSetInventory(app.ctx(), app.rebrick, num)
			if len(inv.Items) == 0 {
				app.setMsg(strings.Join(inv.Notes, " "), true)
				return
			}
			rep, err := app.legoDB.MissingFor(inv.Items, copies)
			if err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			name := ""
			if set, err := app.legoDB.GetSetByNum(num); err == nil && set != nil {
				name = set.Name
			}
			app.legoMissing = &missingView{SetNum: num, Name: name, Report: rep, Source: inv.Source}
			app.goTo(scrLegoMissing)
		},
	}
}

const missingRowCap = 60

func legoMissingScreen() screenModel {
	return &tableScreen{
		panelID: "LEGMSG",
		title:   "Missing Parts",
		columns: []string{"Part #", "Colour", "Name", "Need", "Have", "Short", "Covered by"},
		fetch: func(app *App) ([][]string, string, error) {
			v := app.legoMissing
			if v == nil {
				return nil, "Nothing to show — go back and enter a set number", nil
			}
			r := v.Report
			rows := make([][]string, 0, min(len(r.Missing), missingRowCap))
			for _, m := range r.Missing[:min(len(r.Missing), missingRowCap)] {
				rows = append(rows, []string{m.PartNum, m.ColorName, m.PartName, strconv.Itoa(m.Need), strconv.Itoa(m.Have), strconv.Itoa(m.Short), orDash(strings.Join(m.Substituted, ", "))})
			}
			label := v.SetNum
			if v.Name != "" {
				label += " " + v.Name
			}
			title := fmt.Sprintf("%s x%d: you hold %d%% (%d of %d pieces), %d of %d lines complete", label, r.Copies, r.Percent(), r.PiecesHeld, r.PiecesNeeded, r.Complete, r.Lines)
			var notes []string
			if r.SubstitutedPieces > 0 {
				notes = append(notes, fmt.Sprintf("%d piece(s) are equivalent parts", r.SubstitutedPieces))
			}
			if len(r.Missing) > missingRowCap {
				notes = append(notes, fmt.Sprintf("biggest %d of %d shortfalls; all: wms lego wanted --set %s", missingRowCap, len(r.Missing), v.SetNum))
			} else if len(r.Missing) > 0 {
				notes = append(notes, "BrickLink list: wms lego wanted --set "+v.SetNum)
			}
			src, _, _ := strings.Cut(v.Source, ",") // "offline catalog", not the refresh date
			title += "\n" + strings.Join(append([]string{src}, notes...), " — ")
			return rows, title, nil
		},
	}
}

func legoBuildScreen() screenModel {
	return &tableScreen{
		panelID: "LEGBLD",
		title:   "What Can I Build?",
		columns: []string{"Set #", "Name", "Theme", "Year", "Have", "Pieces", "Missing"},
		fetch: func(app *App) ([][]string, string, error) {
			res, err := app.legoDB.CanBuild(lego.BuildOptions{MinPercent: 50})
			if err != nil {
				return nil, "", err
			}
			rows := make([][]string, len(res))
			for i, r := range res {
				theme := r.Theme
				if theme == "" {
					theme = "—"
				}
				rows[i] = []string{r.SetNum, r.Name, theme, strconv.Itoa(r.Year), fmt.Sprintf("%d%%", r.Percent), strconv.Itoa(r.Have) + "/" + strconv.Itoa(r.Total), strconv.Itoa(r.Missing)}
			}
			title := fmt.Sprintf("%d set(s) at least 50%% covered by loose parts — D detail, / filter", len(res))
			if len(res) == 0 {
				title = "No set is half covered by your loose parts yet"
			}
			return rows, title, nil
		},
		extra: func(app *App, msg tea.KeyMsg) {
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && (msg.Runes[0] == 'd' || msg.Runes[0] == 'D') {
				app.goTo(scrLegoDetailAsk)
			}
		},
	}
}

func legoHistoryScreen() screenModel {
	return &tableScreen{
		panelID: "LEGHST",
		title:   "History of Changes",
		columns: []string{"When", "Who", "What", "Item", "Colour", "Qty", "Note"},
		fetch: func(app *App) ([][]string, string, error) {
			h, err := app.legoDB.History("", 40)
			if err != nil {
				return nil, "", err
			}
			rows := make([][]string, len(h))
			for i, r := range h {
				rows[i] = []string{r.At.Local().Format("01-02 15:04"), r.Actor, r.Action, r.ItemType + " " + r.Item, orDash(r.ColorName), fmt.Sprintf("%d -> %d", r.Before, r.After), r.Note}
			}
			title := fmt.Sprintf("%d recent change(s), newest first", len(h))
			if snaps, err := app.legoDB.Snapshots(30); err == nil && len(snaps) > 1 {
				series := make([]float64, 0, len(snaps))
				for i := len(snaps) - 1; i >= 0; i-- {
					series = append(series, float64(snaps[i].Pieces))
				}
				title += fmt.Sprintf(" — pieces over %d snapshot(s): %s (restore: wms lego restore)", len(snaps), lego.Sparkline(series))
			}
			return rows, title, nil
		},
	}
}
