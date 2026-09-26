package uiapp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/brickowl"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// The Set Workshop: parts checks and stock checks, the completion dashboard, a set's
// missing parts with BrickLink/BrickOwl prices and shop links, orders for them
// (supplier, invoice, tracking, shipping) through to received, and the spend report.

const (
	scrWorkshop         = "workshop"
	scrCheckAsk         = "check_ask"
	scrCompletion       = "completion"
	scrSetMissing       = "set_missing"
	scrShopLinks        = "shop_links"
	scrOrders           = "orders"
	scrOrderEdit        = "order_edit"
	scrOrderLines       = "order_lines"
	scrLinePrice        = "order_line_price"
	scrLineReceive      = "order_line_receive"
	scrSpend            = "spend"
	scrSetInfo          = "set_info"
	scrLabelsAsk        = "labels_ask"
	scrReports          = "reports"
	scrReportsSet       = "reports_set"
	scrRetiring         = "retiring"
	scrReportsStocktake = "reports_stocktake"
	scrReportsArchive   = "reports_archive"
	workshopBanner      = "workshop_banner"
	defaultCurrency     = "GBP"
)

type workshopState struct {
	set      string
	lines    []lego.ShoppingLine
	line     int
	orderID  int64
	lineID   int64
	spendBy  string
	orderSet string // the set an order being created is for
}

func workshopScreens() map[string]screenModel {
	return map[string]screenModel{
		scrWorkshop:         workshopHubScreen(),
		scrCheckAsk:         checkAskScreen(),
		scrCompletion:       &selectList{panelID: "SETCMP", title: "Set Completion", rows: completionRows, keys: completionKeys, hint: "↑/↓ choose  Enter missing parts  K check again  L label  I location"},
		scrSetMissing:       &selectList{panelID: "SETMIS", title: "Missing Parts & Prices", rows: setMissingRows, keys: setMissingKeys, hint: "P fetch prices  Enter shop links  T take spare  O order all  W shopping list  L label"},
		scrShopLinks:        &shopLinksScreen{},
		scrOrders:           &selectList{panelID: "ORDERS", title: "Parts Orders", rows: orderRows, keys: orderKeys, hint: "Enter lines  N new  E edit  O ordered  S shipped  R received  C cancel", emptyHint: "N starts a new order once you know what you're buying and from where."},
		scrOrderEdit:        orderEditScreen(),
		scrOrderLines:       &selectList{panelID: "ORDLIN", title: "Order Lines", rows: orderLineRows, keys: orderLineKeys, hint: "P unit price  R receive  E edit order"},
		scrLinePrice:        linePriceScreen(),
		scrLineReceive:      lineReceiveScreen(),
		scrSpend:            spendScreen(),
		scrSetInfo:          setInfoScreen(),
		scrLabelsAsk:        labelsAskScreen(),
		scrReports:          reportsHubScreen(),
		scrReportsSet:       reportsSetAskScreen(),
		scrReportsStocktake: reportsStocktakeAskScreen(),
		scrReportsArchive:   &selectList{panelID: "REPARC", title: "Report Archive", rows: archiveRows, keys: archiveKeys, hint: "Enter: a fresh link for that report (kept 90 days from when it was made)"},
		scrRetiring:         &selectList{panelID: "RETIRE", title: "Retiring Soon", rows: retiringRows, hint: "Sets retiring within 6 months, from the retirement sheet (wms lego retirement refresh)"},
	}
}

func (a *App) ws() *workshopState {
	if a.workshop == nil {
		a.workshop = &workshopState{spendBy: "set"}
	}
	return a.workshop
}

func workshopHubScreen() screenModel {
	return &menuScreen{
		panelID: "WKSHOP",
		title:   "Set Workshop",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Check a set's parts (or stock-check a checked set)", Go: func(app *App) { app.goTo(scrCheckAsk) }, Perm: "sets.check"},
				{Key: "2", Label: "Completion dashboard: every checked set", Go: func(app *App) { app.goTo(scrCompletion) }, Perm: "lego.view"},
				{Key: "3", Label: "Missing parts for a set (compare with loose parts)", Go: func(app *App) { app.goTo(scrLegoMissingAsk) }, Perm: "lego.view"},
				{Key: "4", Label: "Parts orders", Go: func(app *App) { app.goTo(scrOrders) }, Perm: "orders.view"},
				{Key: "5", Label: "Spend report", Go: func(app *App) { app.goTo(scrSpend) }, Perm: "orders.view"},
				{Key: "6", Label: "Print labels", Go: func(app *App) { app.goTo(scrLabelsAsk) }, Perm: "labels.print"},
				{Key: "7", Label: "Reports: stock-take, parts lists, missing/extra parts, sets, orders", Go: func(app *App) { app.goTo(scrReports) }, Perm: "lego.view"},
				{Key: "8", Label: "Retiring soon (owned & watched sets)", Go: func(app *App) { app.goTo(scrRetiring) }, Perm: "lego.view"},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
		intro: workshopNote,
	}
}

// workshopNote is the banner: how many sets are short and what is on order.
func workshopNote(app *App) string {
	inc, err := app.legoDB.IncompleteSets()
	if err != nil || len(inc) == 0 {
		if st, err := app.legoDB.Stats(); err == nil && st.SetTitles == 0 {
			return app.theme.Muted.Render(" No sets yet — add one from the LEGO menu, then come back to check its parts. ")
		}
		return ""
	}
	missing, onOrder := 0, 0
	for _, s := range inc {
		missing += s.MissingQty
		onOrder += s.OnOrderQty
	}
	return app.theme.Warning.Render(fmt.Sprintf(" INCOMPLETE: %d set(s), %d part(s) missing, %d on order ", len(inc), missing, onOrder)) +
		app.theme.Muted.Render("  -> 2 Completion dashboard")
}

func checkAskScreen() screenModel {
	return &formScreen{
		panelID: "CHKASK",
		title:   "Check a Set",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Set number (e.g. 75192 or 10696-1)"}}
		},
		preamble: func(app *App) string {
			return app.theme.Muted.Render("A set never checked gets a parts check (every line starts as have-all);\na checked set gets a stock check (a recount from what was found last time).")
		},
		submit: func(app *App, v []string) {
			num := catalogSetNum(v[0])
			if strings.TrimSpace(v[0]) == "" {
				app.setMsg("Enter a set number.", true)
				return
			}
			kind := lego.CheckIntake
			if app.legoDB.GetSetState(num).Checked() {
				kind = lego.CheckRecount
			}
			app.back()
			startCheck(app, num, kind)
		},
	}
}

// ---- completion dashboard ----

func progressBar(t ui.Theme, have, total, width int) string {
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	n := have * width / total
	return t.Success.Render(strings.Repeat("█", n)) + t.Muted.Render(strings.Repeat("░", width-n))
}

func completionRows(app *App) ([]string, [][]string, []string) {
	sets, _ := app.legoDB.CheckedSets()
	spend := map[string]float64{}
	if rows, err := app.legoDB.Spend("set"); err == nil {
		for _, r := range rows {
			spend[r.Key] = r.Parts + r.Shipping
		}
	}
	var rows [][]string
	var keys []string
	for _, s := range sets {
		c, _ := app.legoDB.GetCheck(s.LastCheck.ID)
		pieces, have := 0, 0
		if c != nil {
			pieces, have, _, _, _ = c.Totals()
		}
		status := "COMPLETE"
		if s.Incomplete() {
			status = fmt.Sprintf("%d missing", s.MissingQty)
			if s.OnOrderQty > 0 {
				status += fmt.Sprintf(", %d ordered", s.OnOrderQty)
			}
		}
		spent := ""
		if v := spend[s.SetNum]; v > 0 {
			spent = fmt.Sprintf("%.2f", v)
		}
		name := ""
		if cs, _ := app.legoDB.CatalogSet(s.SetNum); cs != nil {
			name = cs.Name
		}
		pct := 100
		if pieces > 0 {
			pct = have * 100 / pieces
		}
		rows = append(rows, []string{s.SetNum, name, progressBar(app.theme, have, pieces, 10) + fmt.Sprintf(" %3d%%", pct), status, spent, s.LastCheck.CheckedBy})
		keys = append(keys, s.SetNum)
	}
	return []string{"Set", "Name", "Progress", "Status", "Spent", "Checked by"}, rows, keys
}

func completionKeys(app *App, set string, msg tea.KeyMsg) {
	if set == "" {
		return
	}
	switch {
	case msg.Type == tea.KeyEnter:
		app.ws().set = set
		app.goTo(scrSetMissing)
	case isKey(msg, 'k'):
		startCheck(app, set, lego.CheckRecount)
	case isKey(msg, 'l'):
		startLabels(app, []string{set})
	case isKey(msg, 'i'):
		app.ws().set = set
		app.goTo(scrSetInfo)
	}
}

// ---- a set's missing parts ----

func setMissingRows(app *App) ([]string, [][]string, []string) {
	w := app.ws()
	lines, err := app.legoDB.ShoppingList(w.set)
	if err != nil {
		app.setMsg(err.Error(), true)
	}
	w.lines = lines
	var rows [][]string
	var keys []string
	total := 0.0
	cur := ""
	for i, l := range lines {
		where, each := l.Cheapest()
		if each > 0 {
			total += each * float64(l.ToBuy())
			if cur == "" {
				cur = map[string]string{"BrickLink": l.BLCur, "BrickOwl": l.BOCur}[where]
			}
		}
		spare := ""
		if l.Spare > 0 {
			spare = fmt.Sprintf("%d spare", l.Spare)
		}
		rows = append(rows, []string{l.PartNum, orDash(l.ColorName), l.PartName, strconv.Itoa(l.Short), zeroDash(l.OnOrder), money(l.BL), money(l.BO), where, spare})
		keys = append(keys, strconv.Itoa(i))
	}
	if len(lines) > 0 {
		app.setMsgIfEmpty(fmt.Sprintf("Set %s: %d line(s) short; cheapest known total %.2f %s (P fetches prices).", w.set, len(lines), total, cur))
	}
	return []string{"Part", "Colour", "Name", "Short", "Ordered", "BL avg", "BO avg", "Cheapest", ""}, rows, keys
}

func zeroDash(n int) string {
	if n == 0 {
		return "—"
	}
	return strconv.Itoa(n)
}

func money(v float64) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.3f", v)
}

func (a *App) setMsgIfEmpty(m string) {
	if a.message == "" {
		a.setMsg(m, false)
	}
}

// pricesFetchedMsg reports a background BrickLink/BrickOwl price fetch (see
// startBusy in app.go); handled in App.Update.
type pricesFetchedMsg struct {
	n    int
	errs []error
}

func (a *App) pricesFetched(m pricesFetchedMsg) {
	a.stopBusy()
	msg := fmt.Sprintf("Fetched %d price(s).", m.n)
	for _, e := range m.errs {
		msg += " " + e.Error()
	}
	a.setMsg(msg, len(m.errs) > 0)
}

func setMissingKeys(app *App, key string, msg tea.KeyMsg) {
	w := app.ws()
	idx, _ := strconv.Atoi(key)
	has := key != "" && idx < len(w.lines)
	switch {
	case isKey(msg, 'p'):
		if !app.require("bricklink.price", "FETCH_PRICES") {
			return
		}
		var bl lego.PriceFetcher
		if c := app.blClient(); c.Enabled() {
			bl = priceAdapter{c}
		}
		bo := brickowl.FromConfig()
		if bl == nil && !bo.Enabled() {
			app.setMsg("Neither BrickLink nor BrickOwl is set up (Admin → Settings: BrickLink API; BRICKOWL_API_KEY).", true)
			return
		}
		cond := strings.ToUpper(config.Get(config.BricklinkCondition))
		lines := w.lines // a private snapshot: the fetch runs on bubbletea's goroutine, the next render re-reads prices from the DB
		app.startBusy(fmt.Sprintf("Fetching prices for %d line(s)…", len(lines)), func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			n, errs := app.legoDB.FetchPrices(ctx, lines, bl, bo, cond)
			return pricesFetchedMsg{n: n, errs: errs}
		})
	case msg.Type == tea.KeyEnter && has:
		w.line = idx
		app.goTo(scrShopLinks)
	case isKey(msg, 't') && has:
		l := w.lines[idx]
		take := min(l.Spare, l.ToBuy())
		if take == 0 {
			app.setMsg("No spares of this part to take.", true)
			return
		}
		if _, err := app.legoDB.TakeSpare(w.set, l.PartNum, l.ColorID, take); err != nil {
			app.setMsg(err.Error(), true)
			return
		}
		app.audit.Log(app.userName(), "", "SPARE_MOVED", "SUCCESS", fmt.Sprintf("%d x %s into set %s", take, l.PartNum, w.set))
		app.setMsg(fmt.Sprintf("Moved %d %s from spares into %s.", take, l.PartNum, w.set), false)
	case isKey(msg, 'o'):
		if !app.require("orders.manage", "ORDER_CREATE") {
			return
		}
		w.orderID, w.orderSet = 0, w.set
		app.goTo(scrOrderEdit)
	case isKey(msg, 'w'):
		set := w.set
		lines := w.lines
		startExport(app, &exportJob{What: "shopping list for " + set, Kind: "shopping", Num: set,
			Formats: []string{"bricklink-xml", "xlsx", "csv", "json", "html"},
			Build: func(app *App) (*lego.ExportData, error) {
				return withPictures(app, app.legoDB.ShoppingExport(set, lines)), nil
			}})
	case isKey(msg, 'l'):
		startLabels(app, []string{w.set})
	}
}

// shopLinksScreen shows where to buy one part, with a QR code for each link.
type shopLinksScreen struct {
	base
	sel int
}

func (s *shopLinksScreen) PanelID() string    { return "SHOPLK" }
func (s *shopLinksScreen) Title() string      { return "Where to Buy" }
func (s *shopLinksScreen) OnEnter(app *App)   { s.sel = 0 }
func (s *shopLinksScreen) FKeys() [][2]string { return [][2]string{{"F3", "Exit"}, {"F12", "Cancel"}} }

func (s *shopLinksScreen) HandleKey(app *App, msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyUp:
		s.sel = max(0, s.sel-1)
	case tea.KeyDown:
		s.sel = min(3, s.sel+1)
	}
}

func (s *shopLinksScreen) Body(app *App) string {
	t := app.theme
	w := app.ws()
	if w.line >= len(w.lines) {
		return ""
	}
	l := w.lines[w.line]
	links := app.legoDB.ShopLinks(l)
	var b strings.Builder
	b.WriteString(t.Strong.Render(fmt.Sprintf("%s %s — %s, need %d", l.PartNum, l.ColorName, l.PartName, l.ToBuy())) + "\n")
	if l.ElementID != "" {
		b.WriteString(t.Muted.Render("LEGO element id "+l.ElementID+" (use it at Pick a Brick)") + "\n")
	}
	b.WriteString("\n")
	for i, lk := range links {
		mark := "  "
		if i == s.sel {
			mark = "▶ "
		}
		b.WriteString(fmt.Sprintf("%s%-18s %s\n", mark, lk[0], t.Accent.Render(lk[1])))
	}
	s.sel = min(s.sel, len(links)-1)
	if !t.Mono && (app.height == 0 || app.height >= 36) {
		b.WriteString("\n" + QRCode(links[s.sel][1]))
	} else {
		b.WriteString("\n" + t.Muted.Render("↑/↓ choose a link (a taller window shows its QR code)."))
	}
	return b.String()
}

// ---- orders ----

func orderRows(app *App) ([]string, [][]string, []string) {
	orders, _ := app.legoDB.ListOrders("")
	var rows [][]string
	var keys []string
	for _, o := range orders {
		when := o.OrderedAt
		if when == "" {
			when = o.CreatedAt[:min(10, len(o.CreatedAt))]
		}
		rows = append(rows, []string{"#" + strconv.FormatInt(o.ID, 10), strings.TrimSpace(o.SupplierKind + " " + o.Supplier), strings.ToUpper(o.Status),
			strconv.Itoa(o.Pieces()), fmt.Sprintf("%.2f %s", o.Total(), o.Currency), when, orDash(o.TrackingNo)})
		keys = append(keys, strconv.FormatInt(o.ID, 10))
	}
	return []string{"Order", "Supplier", "Status", "Parts", "Total", "Date", "Tracking"}, rows, keys
}

func orderKeys(app *App, key string, msg tea.KeyMsg) {
	w := app.ws()
	id, _ := strconv.ParseInt(key, 10, 64)
	status := map[rune]string{'o': "ordered", 's': "shipped", 'r': "received", 'c': "cancelled"}
	switch {
	case msg.Type == tea.KeyEnter && id > 0:
		w.orderID = id
		app.goTo(scrOrderLines)
	case isKey(msg, 'n'):
		if app.require("orders.manage", "ORDER_CREATE") {
			w.orderID, w.orderSet = 0, ""
			app.goTo(scrOrderEdit)
		}
	case isKey(msg, 'e') && id > 0:
		if app.require("orders.manage", "ORDER_EDIT") {
			w.orderID = id
			app.goTo(scrOrderEdit)
		}
	case msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && id > 0:
		st, ok := status[toLower(msg.Runes[0])]
		if !ok || !app.require("orders.manage", "ORDER_STATUS") {
			return
		}
		setOrderStatus(app, id, st)
	}
}

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}

func setOrderStatus(app *App, id int64, st string) {
	done, err := app.legoDB.SetOrderStatus(id, st)
	if err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	app.audit.Log(app.userName(), "", "ORDER_STATUS", "SUCCESS", fmt.Sprintf("order #%d %s", id, st))
	o, _ := app.legoDB.GetOrder(id)
	desc := fmt.Sprintf("order #%d", id)
	if o != nil {
		desc = fmt.Sprintf("order #%d from %s %s", id, o.SupplierKind, o.Supplier)
	}
	msg := fmt.Sprintf("Order #%d is now %s.", id, strings.ToUpper(st))
	switch st {
	case "shipped":
		tr := ""
		if o != nil && o.TrackingNo != "" {
			tr = " Tracking " + o.Carrier + " " + o.TrackingNo
		}
		app.notifyEvent("order_shipped", strings.TrimSpace(desc+" has shipped"), tr)
	case "received":
		app.notifyEvent("order_received", desc+" has arrived", "")
	}
	for _, set := range done {
		msg += " Set " + set + " is now COMPLETE!"
		app.notifyEvent("set_complete", "Set "+set+" is complete", "All missing parts have arrived.")
		app.emit("set_complete", map[string]any{"set": set})
	}
	app.setMsg(msg, false)
	if st == "received" {
		startPushReceived(app, id)
	}
}

// orderPartDBSyncedMsg reports the background Part-DB push startPushReceived kicked
// off; handled in App.Update. It carries nothing — pushReceived's own result was
// never surfaced in the order-status message even before this ran in the background,
// so there is nothing new to show, only the spinner to clear.
type orderPartDBSyncedMsg struct{}

func (a *App) orderPartDBSynced(orderPartDBSyncedMsg) { a.stopBusy() }

// startPushReceived runs pushReceived off the key-handling path (see startBusy,
// app.go) so a slow Part-DB sync can no longer freeze the screen.
func startPushReceived(app *App, orderID int64) {
	app.startBusy("Updating Part-DB…", func() tea.Msg {
		pushReceived(app, orderID)
		return orderPartDBSyncedMsg{}
	})
}

// pushReceived updates Part-DB for the sets an order filled.
func pushReceived(app *App, orderID int64) {
	if app.pdbw == nil || !app.pdbw.API().Enabled() {
		return
	}
	o, err := app.legoDB.GetOrder(orderID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	syncer := &lego.PartSyncer{Lego: app.legoDB, Writer: app.pdbw}
	pushed := map[string]bool{}
	for _, l := range o.Lines {
		if l.SetNum == "" || pushed[l.SetNum] {
			continue
		}
		pushed[l.SetNum] = true
		if c, _ := app.legoDB.LastCheck(l.SetNum); c != nil {
			name := ""
			if cs, _ := app.legoDB.CatalogSet(l.SetNum); cs != nil {
				name = cs.Name
			}
			_, _ = syncer.PushSet(ctx, c, name)
		}
	}
}

const (
	ofKind = iota
	ofSupplier
	ofOrderNo
	ofInvoice
	ofTracking
	ofCarrier
	ofCurrency
	ofShipping
	ofNote
)

func orderEditScreen() screenModel {
	return &formScreen{
		panelID: "ORDEDT",
		title:   "Order",
		build: func(app *App) []ui.Field {
			w := app.ws()
			o := &lego.Order{Currency: config.Get(config.BricklinkCurrency)}
			if o.Currency == "" {
				o.Currency = defaultCurrency
			}
			if w.orderID > 0 {
				if got, err := app.legoDB.GetOrder(w.orderID); err == nil {
					o = got
				}
			}
			ship := ""
			if o.Shipping > 0 {
				ship = fmt.Sprintf("%.2f", o.Shipping)
			}
			kind := o.SupplierKind
			if kind == "" {
				kind = "bricklink"
			}
			return []ui.Field{
				{Label: "Bought from: bricklink, brickowl, pab, rebrickable, other", Value: kind, Fresh: w.orderID == 0},
				{Label: "Store / seller name", Value: o.Supplier},
				{Label: "Order number (optional)", Value: o.OrderNo},
				{Label: "Invoice number (optional)", Value: o.InvoiceNo},
				{Label: "Tracking number (optional)", Value: o.TrackingNo},
				{Label: "Carrier (optional)", Value: o.Carrier},
				{Label: "Currency", Value: o.Currency},
				{Label: "Shipping cost (optional)", Value: ship},
				{Label: "Note (optional)", Value: o.Note},
			}
		},
		preamble: func(app *App) string {
			w := app.ws()
			if w.orderID == 0 && w.orderSet != "" {
				return app.theme.Muted.Render("A new order for everything set " + w.orderSet + " is still short (lines at the cheapest known price; change them after).")
			}
			return ""
		},
		submit: func(app *App, v []string) {
			w := app.ws()
			kind := strings.ToLower(strings.TrimSpace(v[ofKind]))
			valid := false
			for _, k := range lego.SupplierKinds {
				valid = valid || k == kind
			}
			if !valid {
				app.setMsg("Bought from must be one of "+strings.Join(lego.SupplierKinds, ", ")+".", true)
				return
			}
			ship := 0.0
			if s := strings.TrimSpace(v[ofShipping]); s != "" {
				var err error
				if ship, err = strconv.ParseFloat(s, 64); err != nil || ship < 0 {
					app.setMsg("Shipping must be a number like 3.50.", true)
					return
				}
			}
			o := &lego.Order{CreatedBy: app.userName()}
			if w.orderID > 0 {
				if got, err := app.legoDB.GetOrder(w.orderID); err == nil {
					o = got
				}
			}
			o.SupplierKind, o.Supplier, o.OrderNo, o.InvoiceNo = kind, strings.TrimSpace(v[ofSupplier]), strings.TrimSpace(v[ofOrderNo]), strings.TrimSpace(v[ofInvoice])
			o.TrackingNo, o.Carrier, o.Currency, o.Shipping, o.Note = strings.TrimSpace(v[ofTracking]), strings.TrimSpace(v[ofCarrier]), strings.ToUpper(strings.TrimSpace(v[ofCurrency])), ship, strings.TrimSpace(v[ofNote])
			var err error
			if o.ID == 0 && w.orderSet != "" {
				lines := w.lines
				err = app.legoDB.OrderFromMissing(w.orderSet, o, func(cl lego.CheckLine) float64 {
					for _, l := range lines {
						if l.PartNum == cl.PartNum && l.ColorID == cl.ColorID {
							_, p := l.Cheapest()
							if kind == "brickowl" && l.BO > 0 {
								p = l.BO
							} else if kind == "bricklink" && l.BL > 0 {
								p = l.BL
							}
							return p
						}
					}
					return 0
				})
			} else {
				err = app.legoDB.SaveOrder(o)
			}
			if err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.userName(), "", "ORDER_SAVED", "SUCCESS", fmt.Sprintf("order #%d %s %s", o.ID, o.SupplierKind, o.Supplier))
			w.orderID, w.orderSet = o.ID, ""
			app.back()
			app.goTo(scrOrderLines)
			app.setMsg(fmt.Sprintf("Order #%d saved. O on the orders list marks it ordered, S shipped, R received.", o.ID), false)
		},
	}
}

func orderLineRows(app *App) ([]string, [][]string, []string) {
	o, err := app.legoDB.GetOrder(app.ws().orderID)
	if err != nil {
		return nil, nil, nil
	}
	var rows [][]string
	var keys []string
	for _, l := range o.Lines {
		rows = append(rows, []string{orDash(l.SetNum), l.PartNum, orDash(l.ColorName), l.PartName, strconv.Itoa(l.Qty), fmt.Sprintf("%.3f", l.UnitPrice),
			fmt.Sprintf("%.2f", float64(l.Qty)*l.UnitPrice), fmt.Sprintf("%d/%d", l.ReceivedQty, l.Qty)})
		keys = append(keys, strconv.FormatInt(l.ID, 10))
	}
	rows = append(rows, []string{"", "", "", "Shipping", "", "", fmt.Sprintf("%.2f", o.Shipping), ""},
		[]string{"", "", "", "TOTAL " + o.Currency, strconv.Itoa(o.Pieces()), "", fmt.Sprintf("%.2f", o.Total()), strings.ToUpper(o.Status)})
	return []string{"Set", "Part", "Colour", "Name", "Qty", "Each", "Line", "Received"}, rows, keys
}

func orderLineKeys(app *App, key string, msg tea.KeyMsg) {
	w := app.ws()
	id, _ := strconv.ParseInt(key, 10, 64)
	switch {
	case isKey(msg, 'p') && id > 0:
		if app.require("orders.manage", "ORDER_PRICE") {
			w.lineID = id
			app.goTo(scrLinePrice)
		}
	case isKey(msg, 'r') && id > 0:
		if app.require("orders.manage", "ORDER_RECEIVE") {
			w.lineID = id
			app.goTo(scrLineReceive)
		}
	case isKey(msg, 'e'):
		if app.require("orders.manage", "ORDER_EDIT") {
			app.goTo(scrOrderEdit)
		}
	}
}

func linePriceScreen() screenModel {
	return &formScreen{
		panelID: "ORDPRC",
		title:   "Unit Price",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Price paid for each part (e.g. 0.045)"}}
		},
		submit: func(app *App, v []string) {
			p, err := strconv.ParseFloat(strings.TrimSpace(v[0]), 64)
			if err != nil || p < 0 {
				app.setMsg("Enter a price like 0.045.", true)
				return
			}
			if err := app.legoDB.SetLinePrice(app.ws().lineID, p); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.onBack()
			app.setMsg("Price saved.", false)
		},
	}
}

func lineReceiveScreen() screenModel {
	return &formScreen{
		panelID: "ORDRCV",
		title:   "Receive Parts",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "How many arrived"}}
		},
		submit: func(app *App, v []string) {
			n, err := strconv.Atoi(strings.TrimSpace(v[0]))
			if err != nil || n <= 0 {
				app.setMsg("Enter how many arrived.", true)
				return
			}
			done, err := app.legoDB.ReceiveLine(app.ws().lineID, n)
			if err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.userName(), "", "ORDER_RECEIVE", "SUCCESS", fmt.Sprintf("line %d x%d", app.ws().lineID, n))
			orderID := app.ws().orderID
			app.onBack()
			msg := fmt.Sprintf("Received %d.", n)
			if done != "" {
				msg += " Set " + done + " is now COMPLETE!"
				app.notifyEvent("set_complete", "Set "+done+" is complete", "All missing parts have arrived.")
			}
			app.setMsg(msg, false)
			startPushReceived(app, orderID)
		},
	}
}

// ---- spend ----

func spendScreen() screenModel {
	return &tableScreen{
		panelID: "SPEND",
		title:   "Spend Report",
		columns: []string{"", "Orders", "Parts", "Parts cost", "Shipping", "Total"},
		fetch: func(app *App) ([][]string, string, error) {
			by := app.ws().spendBy
			rows, err := app.legoDB.Spend(by)
			if err != nil {
				return nil, "", err
			}
			var out [][]string
			var tp, ts float64
			for _, r := range rows {
				out = append(out, []string{r.Key, strconv.Itoa(r.Orders), strconv.Itoa(r.Pieces), fmt.Sprintf("%.2f", r.Parts), fmt.Sprintf("%.2f", r.Shipping), fmt.Sprintf("%.2f %s", r.Parts+r.Shipping, r.Currency)})
				tp, ts = tp+r.Parts, ts+r.Shipping
			}
			out = append(out, []string{"TOTAL", "", "", fmt.Sprintf("%.2f", tp), fmt.Sprintf("%.2f", ts), fmt.Sprintf("%.2f", tp+ts)})
			return out, "by " + by + " — 1 by set, 2 by supplier, 3 by month, X export", nil
		},
		extra: func(app *App, msg tea.KeyMsg) {
			w := app.ws()
			switch {
			case isKey(msg, 'x'):
				by := w.spendBy
				startExport(app, &exportJob{What: "spend by " + by, Kind: "spend", Num: by, Formats: []string{"xlsx", "csv", "json", "html"},
					Build: func(app *App) (*lego.ExportData, error) {
						rows, err := app.legoDB.Spend(by)
						if err != nil {
							return nil, err
						}
						d := &lego.ExportData{Title: "Spend by " + by, When: time.Now()}
						for _, r := range rows {
							d.Facts = append(d.Facts, [2]string{r.Key, fmt.Sprintf("%d order(s), %d part(s): parts %.2f + shipping %.2f = %.2f %s", r.Orders, r.Pieces, r.Parts, r.Shipping, r.Parts+r.Shipping, r.Currency)})
						}
						return d, nil
					}})
			case msg.Type == tea.KeyRunes && len(msg.Runes) == 1:
				if by, ok := map[rune]string{'1': "set", '2': "supplier", '3': "month"}[msg.Runes[0]]; ok {
					w.spendBy = by
					app.screens[scrSpend].OnEnter(app)
				}
			}
		},
	}
}

// ---- location & condition ----

func setInfoScreen() screenModel {
	return &formScreen{
		panelID: "SETINF",
		title:   "Where the Set Is Kept",
		build: func(app *App) []ui.Field {
			st := app.legoDB.GetSetState(app.ws().set)
			return []ui.Field{{Label: "Set", Value: app.ws().set, Protected: true}, {Label: "Location (shelf, box, bin)", Value: st.Location},
				{Label: "Condition: sealed, built, in pieces, displayed", Value: st.Condition}, {Label: "Condition note", Value: st.ConditionNote}}
		},
		submit: func(app *App, v []string) {
			if !app.require("lego.edit", "SET_INFO") {
				return
			}
			if err := app.legoDB.SetInfo(app.ws().set, v[1], strings.ToLower(v[2]), v[3]); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.onBack()
			app.setMsg("Saved.", false)
		},
	}
}

// userName is who is signed in ("local" without a session).
func (a *App) userName() string {
	if a.session != nil {
		return a.session.Username
	}
	return "local"
}
