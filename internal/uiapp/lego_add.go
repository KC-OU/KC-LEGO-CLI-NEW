package uiapp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

const (
	scrLegoSetAdd     = "lego_set_add"
	scrLegoSetConfirm = "lego_set_confirm"

	maxOwnedQty = 99999
)

// Adding a set or part is two steps: type the number, then review what was
// looked up (Rebrickable first, then the local catalog, then what you already
// have), say how many you own, and confirm. Only the quantity — and the theme
// or category, so you can match your own naming — is yours to enter; the
// rest is filled in. When nothing can be found the same confirm screen turns
// the remaining fields into inputs so the item can still be recorded by hand.

type setDraft struct {
	Num, Name, Theme string
	Year, Pieces     int
	Source           string
	Existing         *lego.Set // the record already in the collection, if any
	Manual           bool      // nothing found: Name/Year/Pieces are typed in
}

// lookupSet gathers what is known about a set. It never fails: problems come
// back as a note to show the user, and the draft falls back to manual entry.
func lookupSet(app *App, input string) (*setDraft, string) {
	d := &setDraft{Num: collectionSetNum(input)}
	if mine, err := app.legoDB.GetSetByNum(d.Num); err == nil {
		d.Existing = mine
	}

	lk := app.legoDB.LookupSet(app.ctx(), app.rebrick, input)
	note := strings.Join(lk.Notes, " ")
	if lk.Found() {
		d.Name, d.Theme, d.Year, d.Pieces, d.Source = lk.Set.Name, lk.Set.Theme, lk.Set.Year, lk.Set.Pieces, lk.Source
	}

	if mine := d.Existing; mine != nil {
		// What you already stored wins over a fresh lookup, so an update of the
		// quantity never overwrites details you had corrected.
		if mine.Name != "" {
			d.Name = mine.Name
		}
		if mine.Theme != "" {
			d.Theme = mine.Theme
		}
		if mine.Year != 0 {
			d.Year = mine.Year
		}
		if mine.PartsQty != 0 {
			d.Pieces = mine.PartsQty
		}
		if d.Source == "" {
			d.Source = "your collection"
		} else {
			d.Source += " + your collection"
		}
	}
	if d.Source == "" {
		d.Manual, d.Source = true, "not found — enter the details by hand"
		if note == "" {
			note = "No source knows this set — enter the details by hand."
		}
	}
	return d, note
}

func startSetDraft(app *App, input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		app.setMsg("Enter a set number, e.g. 71788 or 75192-1.", true)
		return
	}
	d, note := lookupSet(app, input)
	app.legoSetDraft = d
	app.goTo(scrLegoSetConfirm)
	switch {
	case note != "":
		app.setMsg(note, true)
	default:
		app.setMsg("Check the details, enter how many you own, then type yes to save.", false)
	}
}

func legoSetAddScreen() screenModel {
	return &formScreen{
		panelID:      "LEGADD",
		title:        "Add / Update a Set",
		writeGated:   true,
		deniedAction: "ADD_LEGO_SET",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Set number (e.g. 71788 or 75192-1)"}}
		},
		submit: func(app *App, values []string) { startSetDraft(app, values[0]) },
	}
}

// Field positions on the set confirm screen; the layout is the same whether
// or not the details are typed in, only which fields are Protected changes.
// Everything read-only comes first because the classic prompt style only
// shows fields up to the first one you can edit, and the summary (where the
// details came from, what you already own) must be visible up front.
const (
	sfName, sfYear, sfPieces, sfTheme, sfQty, sfLocation, sfCondition, sfCheck, sfConfirm = 3, 4, 5, 6, 7, 8, 9, 10, 11
)

func legoSetConfirmScreen() screenModel {
	return &formScreen{
		panelID:      "LEGCNF",
		title:        "Confirm Set",
		writeGated:   true,
		deniedAction: "ADD_LEGO_SET",
		build: func(app *App) []ui.Field {
			d := app.legoSetDraft
			if d == nil {
				return []ui.Field{{Label: "Nothing to confirm — go back and enter a set number", Protected: true}}
			}
			owned, qty := "not in your collection yet", "1"
			if d.Existing != nil {
				owned, qty = fmt.Sprintf("%d — change it below if that's wrong", d.Existing.Qty), strconv.Itoa(d.Existing.Qty)
			}
			st := app.legoDB.GetSetState(catalogSetNum(d.Num))
			// Offer the parts check when the parts list is known and the set was never checked.
			checkNow := "no"
			if items, _ := app.legoDB.CatalogSetInventory(catalogSetNum(d.Num)); len(items) > 0 && !st.Checked() && app.can("sets.check") {
				checkNow = "yes"
			}
			num := func(n int) string {
				if n == 0 {
					return ""
				}
				return strconv.Itoa(n)
			}
			return []ui.Field{
				{Label: "Set", Value: d.Num, Protected: true},
				{Label: "Details from", Value: d.Source, Protected: true},
				{Label: "Already owned", Value: owned, Protected: true},
				{Label: "Name", Value: d.Name, Protected: !d.Manual},
				{Label: "Year", Value: num(d.Year), Protected: !d.Manual},
				{Label: "Pieces", Value: num(d.Pieces), Protected: !d.Manual},
				{Label: "Theme", Value: d.Theme},
				{Label: "How many do you own", Value: qty, Fresh: true},
				{Label: "Where it is kept (shelf, box, bin)", Value: st.Location},
				{Label: "Condition: sealed, built, in pieces, displayed", Value: st.Condition},
				{Label: "Check the parts now? (yes / no)", Value: checkNow, Fresh: true},
				{Label: "Save this? (yes / no)"},
			}
		},
		submit: submitSetConfirm,
	}
}

func submitSetConfirm(app *App, v []string) {
	d := app.legoSetDraft
	if d == nil {
		app.onBack()
		return
	}
	qty, err := strconv.Atoi(strings.TrimSpace(v[sfQty]))
	if err != nil || qty < 0 || qty > maxOwnedQty {
		app.setMsg(fmt.Sprintf("How many you own must be a whole number from 0 to %d.", maxOwnedQty), true)
		return
	}
	name := strings.TrimSpace(v[sfName])
	year, pieces := d.Year, d.Pieces
	if d.Manual {
		if name == "" {
			app.setMsg("Name is required.", true)
			return
		}
		if year, err = parseOptionalInt(v[sfYear], 0, 2100); err != nil || (year != 0 && year < 1932) {
			app.setMsg("Year must be a four-digit year (or blank).", true)
			return
		}
		if pieces, err = parseOptionalInt(v[sfPieces], 0, 1000000); err != nil {
			app.setMsg("Pieces must be a whole number (or blank).", true)
			return
		}
	} else {
		name = d.Name
	}
	switch strings.ToLower(strings.TrimSpace(v[sfConfirm])) {
	case "yes", "y":
	case "no", "n":
		app.setMsg("Cancelled — nothing was saved.", false)
		app.popTo(scrLegoHub)
		return
	default:
		app.setMsg(`Type "yes" to save or "no" to cancel.`, true)
		return
	}

	var set lego.Set
	was := 0
	if d.Existing != nil {
		set, was = *d.Existing, d.Existing.Qty
	}
	set.SetNum, set.Name, set.Theme, set.Year, set.PartsQty, set.Qty = d.Num, name, strings.TrimSpace(v[sfTheme]), year, pieces, qty
	if err := app.legoDB.UpsertSet(set); err != nil {
		app.setMsg("Could not save the set: "+err.Error(), true)
		return
	}

	action, msg := "ADD_LEGO_SET", fmt.Sprintf("Set %s (%s) added — you own %d.", set.SetNum, set.Name, qty)
	if d.Existing != nil {
		action, msg = "UPDATE_LEGO_SET", fmt.Sprintf("Set %s (%s) updated — you own %d (was %d).", set.SetNum, set.Name, qty, was)
	}
	app.audit.Log(app.session.Username, app.session.Role, action, "SUCCESS",
		fmt.Sprintf("set_num=%s qty=%d was=%d details=%s", set.SetNum, qty, was, d.Source))
	cond := strings.ToLower(strings.TrimSpace(v[sfCondition]))
	if err := app.legoDB.SetInfo(catalogSetNum(set.SetNum), v[sfLocation], cond, ""); err != nil {
		app.setMsg("Saved the set, but not its location: "+err.Error(), true)
	}
	app.legoSetDraft = nil
	app.setMsg(msg, false)
	app.popTo(scrLegoHub)
	if a := strings.ToLower(strings.TrimSpace(v[sfCheck])); (a == "yes" || a == "y") && qty > 0 {
		startCheck(app, catalogSetNum(set.SetNum), lego.CheckIntake)
		if app.cur == scrSetCheck {
			app.setMsg(msg+" Now mark what is missing (M) or extra (E); F finishes.", false)
		}
	}
}

// catalogSetNum is the catalog's form of a set number ("75192" → "75192-1").
func catalogSetNum(n string) string {
	n = strings.TrimSpace(n)
	if !strings.Contains(n, "-") {
		return n + "-1"
	}
	return n
}

func parseOptionalInt(s string, min, max int) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("out of range")
	}
	return n, nil
}
