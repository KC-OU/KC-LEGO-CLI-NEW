package uiapp

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// One "Add / Update a part" flow, shared by Part-DB's Create New Part, the LEGO
// owned-parts entry and the Quick Add drawer:
//
//	part number -> lookup (offline catalog, then live Rebrickable, then what you
//	already hold) -> colour (from the colours that part exists in) -> category
//	(Lego > <Rebrickable's>, else pick one) -> quantity you type -> yes/no.
//
// Every step has a manual fallback, so nothing blocks: a colour that isn't
// listed can be typed, a category can be picked or created, and a part nobody
// knows is entered field by field. LEGO parts are recorded in the LEGO
// collection first and then pushed to Part-DB through its REST API, so a
// missing token or an unreachable Part-DB never loses what you typed.

const scrPartConfirm = "part_confirm"

type partFlow struct {
	Num          string
	Origin       string // screen to return to when done
	Manual       bool   // no source knew this part: name/description/manufacturer no. are typed
	Name         string
	Description  string
	MfgPN        string
	Category     string // the source's category name (LEGO parts)
	CategoryPath []string
	CategoryID   int // >0 when the user picked an existing Part-DB category
	CategoryText string
	ColorID      int
	ColorName    string
	Source       string
	Colors       []lego.Color
	MinQty       int // warn when the stock falls below this (LEGO parts); 0 = don't track
	Existing     *lego.OwnedPart
	PartDBID     int
	PartDBQty    float64
}

func (f *partFlow) isLego() bool { return !f.Manual }

// ipn is the Part-DB internal part number this flow will create or update.
func (f *partFlow) ipn() string {
	if f.Manual {
		return f.Num
	}
	return lego.IPNFor(f.Num, f.ColorID, f.ColorName)
}

func partAddScreen(id string) screenModel {
	return &formScreen{
		panelID:      "PARTADD",
		title:        "Add / Update a Part",
		writeGated:   true,
		deniedAction: "ADD_PART",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Part number (e.g. 3001)"}}
		},
		preamble: recentsHint,
		submit:   func(app *App, values []string) { startPartFlow(app, values[0]) },
	}
}

const recentShown = 8

// recentsHint lists the part numbers used lately; typing !1 .. !8 picks one.
func recentsHint(app *App) string {
	recent, err := app.legoDB.Recents(recentShown)
	if err != nil || len(recent) == 0 {
		return ""
	}
	parts := make([]string, len(recent))
	for i, r := range recent {
		parts[i] = fmt.Sprintf("!%d %s", i+1, r)
	}
	return app.theme.Muted.Render("Recent: " + strings.Join(parts, "   ") + "   (type ! and its number to reuse one)")
}

// expandRecent turns "!2" into the second recent part number; anything else is returned as typed.
func expandRecent(app *App, input string) (string, bool) {
	input = strings.TrimSpace(input)
	if len(input) < 2 || input[0] != '!' {
		return input, true
	}
	n, err := strconv.Atoi(input[1:])
	recent, _ := app.legoDB.Recents(recentShown)
	if err != nil || n < 1 || n > len(recent) {
		return input, false
	}
	return recent[n-1], true
}

func catalogHint(app *App) string {
	if parts, _, _, err := app.legoDB.CatalogStatus(); err == nil && parts == 0 && !app.rebrick.Enabled() {
		return " There is no offline catalog yet (run `wms lego catalog refresh`) and no Rebrickable key (Admin > Settings & API Keys)."
	}
	return ""
}

func startPartFlow(app *App, input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		app.setMsg("Enter a part number, e.g. 3001.", true)
		return
	}
	var ok bool
	if input, ok = expandRecent(app, input); !ok {
		app.setMsg("There is no recent part with that number — see the list above the prompt.", true)
		return
	}
	if err := lego.CheckPartNum(input); err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	origin := scrHub
	if n := len(app.stack); n > 0 {
		origin = app.stack[n-1]
	}
	f := &partFlow{Num: input, Origin: origin, ColorID: lego.NoColor}
	// A LEGO element ID (the 6-8 digit number on a bag or brick) means one part in one
	// colour. A real part number of the same digits wins.
	elementNote := ""
	if cp, err := app.legoDB.CatalogPart(input); err != nil || cp == nil {
		if part, colorID, ok := app.legoDB.ElementPart(input); ok {
			if c, cok := app.legoDB.ColorByID(colorID); cok {
				elementNote = fmt.Sprintf("Element %s is part %s in %s.", input, part, c.Name)
				input, f.Num, f.ColorID, f.ColorName = part, part, c.ID, c.Name
			}
		}
	}
	info := app.legoDB.LookupPart(app.ctx(), app.rebrick, input)
	var notes []string
	if info != nil {
		f.Num, f.Name, f.Category, f.Source, f.Colors = info.Num, info.Name, info.Category, info.Source, info.Colors
		notes = info.Notes
	} else {
		f.Manual, f.Source = true, "not found — enter the details by hand"
		notes = append(notes, fmt.Sprintf("No source knows part %q.%s", input, catalogHint(app)))
	}
	app.partFlow = f

	switch {
	case f.Manual:
		startCategoryPick(app)
	case elementNote != "": // the colour is already known: skip the colour pick
		afterColor(app)
	default:
		startColorPick(app)
	}
	if elementNote != "" {
		notes = append([]string{elementNote}, notes...)
	}
	if len(notes) > 0 && elementNote == "" { // goTo clears the message, so set it after navigating
		app.setMsg(strings.Join(notes, " "), true)
	} else if elementNote != "" {
		app.setMsg(strings.Join(notes, " "), len(notes) > 1)
	} else {
		app.setMsg(fmt.Sprintf("%s — %s (from %s).", f.Num, f.Name, f.Source), false)
	}
}

// ---- colour ----

func startColorPick(app *App) {
	f := app.partFlow
	items := make([]pickItem, len(f.Colors))
	for i, c := range f.Colors {
		items[i] = pickItem{Key: strconv.Itoa(c.ID), Label: c.Name, RGB: c.RGB}
	}
	header := fmt.Sprintf("Colours %s comes in:", f.Num)
	if len(items) == 0 {
		header = fmt.Sprintf("No colour list for %s — type the colour yourself.", f.Num)
	}
	startPick(app, &pickState{
		Header:     header,
		Prompt:     "Colour",
		Items:      items,
		AllowFree:  true,
		FreeHint:   "Anything else is kept as a typed colour.",
		BlankLabel: "no colour",
		OnPick: func(app *App, it pickItem) {
			f.ColorID, _ = strconv.Atoi(it.Key)
			f.ColorName = it.Label
			afterColor(app)
		},
		OnFree: func(app *App, text string) {
			f.ColorID, f.ColorName = lego.NoColor, text
			afterColor(app)
		},
	})
}

func afterColor(app *App) {
	f := app.partFlow
	if f.Category != "" {
		f.CategoryPath = lego.CategoryPath(f.Category)
		f.CategoryID = 0
		f.CategoryText = strings.Join(f.CategoryPath, " > ") + " (created if missing)"
		goToPartConfirm(app)
		return
	}
	startCategoryPick(app)
}

// ---- category ----

// categoryItems lists every Part-DB category by its full path ("Lego > Bricks").
func categoryItems(app *App) []pickItem {
	cats, err := app.pdb.AllCategories()
	if err != nil {
		return nil
	}
	byID := map[int]partdb.Category{}
	for _, c := range cats {
		byID[c.ID] = c
	}
	path := func(c partdb.Category) string {
		parts := []string{c.Name}
		for depth := 0; c.ParentID.Valid && depth < 10; depth++ {
			p, ok := byID[int(c.ParentID.Int64)]
			if !ok {
				break
			}
			parts = append([]string{p.Name}, parts...)
			c = p
		}
		return strings.Join(parts, " > ")
	}
	items := make([]pickItem, 0, len(cats))
	for _, c := range cats {
		items = append(items, pickItem{Key: strconv.Itoa(c.ID), Label: path(c)})
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label) })
	return items
}

func startCategoryPick(app *App) {
	f := app.partFlow
	free := "A name that isn't listed creates a new category."
	if f.isLego() {
		free = "A name that isn't listed creates it under Lego."
	}
	startPick(app, &pickState{
		Header:    fmt.Sprintf("Which Part-DB category should %s go in?", f.Num),
		Prompt:    "Category",
		Items:     categoryItems(app),
		AllowFree: true,
		FreeHint:  free,
		OnPick: func(app *App, it pickItem) {
			f.CategoryID, _ = strconv.Atoi(it.Key)
			f.CategoryPath = nil
			f.CategoryText = it.Label
			if f.isLego() { // remember the name too, for the LEGO collection row
				f.Category = it.Label[strings.LastIndex(it.Label, "> ")+1:]
				f.Category = strings.TrimSpace(f.Category)
			}
			goToPartConfirm(app)
		},
		OnFree: func(app *App, text string) {
			f.CategoryID = 0
			if f.isLego() {
				f.Category = text
				f.CategoryPath = lego.CategoryPath(text)
			} else {
				f.CategoryPath = []string{text}
			}
			f.CategoryText = strings.Join(f.CategoryPath, " > ") + " (new)"
			goToPartConfirm(app)
		},
	})
}

// ---- confirm ----

// Field positions on the confirm screen. Read-only summary fields come first
// (the classic prompt style only shows fields up to the first editable one).
const (
	cfName, cfDesc, cfMfg, cfQty, cfMin, cfConfirm = 5, 6, 7, 8, 9, 10
)

func goToPartConfirm(app *App) {
	f := app.partFlow
	f.Existing, f.PartDBID, f.PartDBQty, f.MinQty = nil, 0, 0, 0
	if f.isLego() {
		if own, err := app.legoDB.GetOwnedPart(f.Num, f.ColorID, f.ColorName); err == nil {
			f.Existing = own
			if own != nil {
				f.MinQty = own.MinQty
			}
		}
	}
	if id, err := app.pdb.FindPartByIPN(f.ipn()); err == nil && id > 0 {
		f.PartDBID = id
		f.PartDBQty, _ = app.pdbw.Stock(id)
	}
	app.goTo(scrPartConfirm)
	app.setMsg("Check the details, enter how many you have, then type yes to save.", false)
}

func partConfirmScreen() screenModel {
	return &formScreen{
		panelID:      "PARTCNF",
		title:        "Confirm Part",
		writeGated:   true,
		deniedAction: "ADD_PART",
		build: func(app *App) []ui.Field {
			f := app.partFlow
			if f == nil {
				return []ui.Field{{Label: "Nothing to confirm — go back and enter a part number", Protected: true}}
			}
			have, qty := "not recorded yet", "1"
			switch {
			case f.Existing != nil:
				have = fmt.Sprintf("%d in your LEGO collection", f.Existing.Qty)
				qty = strconv.Itoa(f.Existing.Qty)
			case f.PartDBID > 0:
				have = fmt.Sprintf("%g in Part-DB (part #%d)", f.PartDBQty, f.PartDBID)
				qty = strconv.FormatFloat(f.PartDBQty, 'f', -1, 64)
			}
			if f.Existing != nil && f.PartDBID > 0 {
				have += fmt.Sprintf(", %g in Part-DB", f.PartDBQty)
			}
			colour := "—"
			if f.isLego() {
				colour = f.ColorName
				if colour == "" {
					colour = "no colour"
				}
				if f.ColorID >= 0 {
					colour += fmt.Sprintf(" (Rebrickable colour %d)", f.ColorID)
				}
			}
			desc := f.Description
			if f.isLego() {
				desc = "LEGO part " + f.Num
			}
			minField := ui.Field{Label: "Warn me when below (0 = don't)", Value: strconv.Itoa(f.MinQty), Fresh: true}
			if !f.isLego() {
				minField = ui.Field{Label: "Warn me when below", Value: "n/a (LEGO parts only)", Protected: true}
			}
			return []ui.Field{
				{Label: "Part", Value: f.Num, Protected: true},
				{Label: "Details from", Value: f.Source, Protected: true},
				{Label: "Category", Value: f.CategoryText, Protected: true},
				{Label: "Colour", Value: colour, Protected: true},
				{Label: "Already have", Value: have, Protected: true},
				{Label: "Name", Value: f.Name, Protected: !f.Manual},
				{Label: "Description", Value: desc, Protected: !f.Manual},
				{Label: "Manufacturer part no.", Value: f.MfgPN, Protected: !f.Manual},
				{Label: "How many do you have", Value: qty, Fresh: true},
				minField,
				{Label: "Save this? (yes / no)"},
			}
		},
		submit: submitPartConfirm,
	}
}

func submitPartConfirm(app *App, v []string) {
	f := app.partFlow
	if f == nil {
		app.onBack()
		return
	}
	qty, err := strconv.Atoi(strings.TrimSpace(v[cfQty]))
	if err != nil || qty < 0 || qty > maxOwnedQty {
		app.setMsg(fmt.Sprintf("How many you have must be a whole number from 0 to %d.", maxOwnedQty), true)
		return
	}
	if f.isLego() {
		minQty := 0
		if txt := strings.TrimSpace(v[cfMin]); txt != "" {
			n, err := strconv.Atoi(txt)
			if err != nil || n < 0 || n > maxOwnedQty {
				app.setMsg(fmt.Sprintf("The minimum must be a whole number from 0 to %d (0 = don't track).", maxOwnedQty), true)
				return
			}
			minQty = n
		}
		f.MinQty = minQty
	}
	if f.Manual {
		f.Name, f.Description, f.MfgPN = strings.TrimSpace(v[cfName]), strings.TrimSpace(v[cfDesc]), strings.TrimSpace(v[cfMfg])
		if f.Name == "" {
			app.setMsg("Name is required.", true)
			return
		}
	}
	switch strings.ToLower(strings.TrimSpace(v[cfConfirm])) {
	case "yes", "y":
	case "no", "n":
		app.setMsg("Cancelled — nothing was saved.", false)
		app.popTo(f.Origin)
		return
	default:
		app.setMsg(`Type "yes" to save or "no" to cancel.`, true)
		return
	}
	if f.isLego() {
		saveLegoPart(app, f, qty)
	} else {
		saveManualPart(app, f, qty)
	}
}

// partDBCategoryID resolves the flow's category to a Part-DB id, creating it if it is new.
func partDBCategoryID(app *App, f *partFlow) (int, error) {
	if f.CategoryID > 0 {
		return f.CategoryID, nil
	}
	ctx, cancel := app.pdbw.Ctx()
	defer cancel()
	return app.pdbw.ResolveCategory(ctx, f.CategoryPath)
}

func saveLegoPart(app *App, f *partFlow, qty int) {
	owned := lego.OwnedPart{PartNum: f.Num, Name: f.Name, Category: f.Category, ColorID: f.ColorID, ColorName: f.ColorName, Qty: qty, MinQty: f.MinQty}
	if err := app.legoDB.AddOwnedPart(owned); err != nil {
		app.setMsg("Could not save to your LEGO collection: "+err.Error(), true)
		return
	}
	if err := app.legoDB.SetMinQty(f.Num, f.ColorID, f.ColorName, f.MinQty); err != nil { // AddOwnedPart leaves an existing row's minimum alone
		app.setMsg("Saved, but could not set the minimum: "+err.Error(), true)
		return
	}
	_ = app.legoDB.AddRecent(f.Num)
	row, err := app.legoDB.GetOwnedPart(f.Num, f.ColorID, f.ColorName)
	if err != nil || row == nil {
		app.setMsg("Saved, but could not read the row back to send it to Part-DB.", true)
		return
	}

	label := f.Num
	if f.ColorName != "" {
		label += " " + f.ColorName
	}
	verb, prev := "added", 0
	if f.Existing != nil {
		verb, prev = "updated", f.Existing.Qty
	}
	action := "ADD_PART"
	if f.Existing != nil {
		action = "UPDATE_PART"
	}

	restoreLego := func() error {
		if f.Existing == nil {
			return app.legoDB.DeleteOwnedPart(f.Num, f.ColorID, f.ColorName)
		}
		if err := app.legoDB.AddOwnedPart(*f.Existing); err != nil {
			return err
		}
		return app.legoDB.SetMinQty(f.Num, f.ColorID, f.ColorName, f.Existing.MinQty)
	}

	syncer := &lego.PartSyncer{Lego: app.legoDB, Writer: app.pdbw}
	ctx, cancel := app.pdbw.Ctx()
	defer cancel()
	catID, err := partDBCategoryID(app, f)
	var res *partdb.UpsertResult
	if err == nil {
		res, err = syncer.PushIn(ctx, *row, catID)
	}
	if err != nil {
		app.audit.Log(app.session.Username, app.session.Role, action, "PARTIAL",
			fmt.Sprintf("part=%s qty=%d details=%s part-db=%v", f.Num, qty, f.Source, err))
		app.pushUndo("add part "+label, restoreLego)
		app.partFlow = nil
		msg := fmt.Sprintf("%s saved in your LEGO collection (%d), but NOT in Part-DB: %v", label, qty, err)
		if !errors.Is(err, partdb.ErrNoToken) {
			msg += ". `wms lego sync-parts` will retry it."
		}
		app.popTo(f.Origin)
		app.setMsg(msg, true)
		return
	}

	app.pushUndo(verb+" part "+label, func() error {
		if err := res.Undo(); err != nil {
			return err
		}
		return restoreLego()
	})
	app.audit.Log(app.session.Username, app.session.Role, action, "SUCCESS",
		fmt.Sprintf("part=%s colour=%q qty=%d was=%d part_db_id=%d details=%s", f.Num, f.ColorName, qty, prev, res.PartID, f.Source))
	app.partFlow = nil
	app.emit("part_"+verb, map[string]any{"part": f.Num, "name": f.Name, "colour": f.ColorName, "qty": qty, "was": prev, "part_db_id": res.PartID})
	app.popTo(f.Origin)
	app.setMsg(fmt.Sprintf("%s %s: %d in your LEGO collection and Part-DB (part #%d). F9 undoes it.", label, verb, qty, res.PartID), false)
}

func saveManualPart(app *App, f *partFlow, qty int) {
	catID, err := partDBCategoryID(app, f)
	if err != nil {
		app.setMsg("Could not prepare the category: "+err.Error(), true)
		return
	}
	spec := partdb.PartSpec{Name: f.Name, Description: f.Description, IPN: f.ipn(), MfgPN: f.MfgPN, CategoryID: catID}
	ctx, cancel := app.pdbw.Ctx()
	defer cancel()
	res, err := app.pdbw.UpsertPart(ctx, spec, float64(qty))
	if err != nil {
		app.setMsg("Could not save to Part-DB: "+err.Error(), true) // nothing was saved; stay so it can be retried
		return
	}
	verb, action := "created", "ADD_PART"
	if !res.Created {
		verb, action = "updated", "UPDATE_PART"
	}
	app.pushUndo(verb+" part "+f.Num, res.Undo)
	_ = app.legoDB.AddRecent(f.Num)
	app.audit.Log(app.session.Username, app.session.Role, action, "SUCCESS",
		fmt.Sprintf("part=%s qty=%d part_db_id=%d manual=true", f.Num, qty, res.PartID))
	app.partFlow = nil
	evt := "part_added" // Part-DB says "created", the events say "added"
	if !res.Created {
		evt = "part_updated"
	}
	app.emit(evt, map[string]any{"part": f.Num, "name": f.Name, "qty": qty, "part_db_id": res.PartID, "manual": true})
	app.popTo(f.Origin)
	app.setMsg(fmt.Sprintf("Part %s %s in Part-DB (part #%d, stock %d). F9 undoes it.", f.Num, verb, res.PartID, qty), false)
}
