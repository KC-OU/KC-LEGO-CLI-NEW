package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/brickowl"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/labels"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Set checks, missing parts, orders, spend and labels from the shell (the same as
// the TUI's Set Workshop).

func catalogNum(n string) string {
	n = strings.TrimSpace(n)
	if !strings.Contains(n, "-") {
		return n + "-1"
	}
	return n
}

func cliActor() string {
	if u := os.Getenv("SUDO_USER"); u != "" {
		return u
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "cli"
}

// parseLineEdits reads "3001:4:2,3023:blue:1" (part:colour id or name:qty).
func applyEdits(c *lego.SetCheck, spec string, apply func(l *lego.CheckLine, n int)) error {
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		f := strings.Split(item, ":")
		if len(f) != 3 {
			return usageError("%q should be part:colour:qty (colour = Rebrickable id or name)", item)
		}
		n, err := strconv.Atoi(f[2])
		if err != nil || n < 0 {
			return usageError("%q: quantity must be a whole number", item)
		}
		found := false
		for i := range c.Lines {
			l := &c.Lines[i]
			if !strings.EqualFold(l.PartNum, f[0]) {
				continue
			}
			if id, err := strconv.Atoi(f[1]); (err == nil && id == l.ColorID) || strings.EqualFold(f[1], l.ColorName) {
				apply(l, n)
				found = true
				break
			}
		}
		if !found {
			return usageError("set %s has no %s in colour %s", c.SetNum, f[0], f[1])
		}
	}
	return nil
}

func newLegoCheckCmd() *cobra.Command {
	var missing, extra, by string
	var stocktake, noPartDB bool
	cmd := &cobra.Command{
		Use:   "check <set>",
		Short: "Check a set's parts: everything is 'have' except what you list as missing or extra",
		Long: "Records a parts check (or, with --stocktake, a recount) for a set: the set's parts go into its own Part-DB storage\n" +
			"location (LEGO / Sets / <num> <name>), extras become loose parts that remember this set, and the set is flagged\n" +
			"INCOMPLETE when anything is missing. The TUI's Set Workshop does the same interactively.",
		Example: "  wms lego check 75192 --missing 3001:red:2,3023:1:1 --extra 3710:black:3\n  wms lego check 10696 --stocktake --by alex",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, rb, err := openLegoWithClient()
			if err != nil {
				return err
			}
			defer db.Close()
			num := catalogNum(args[0])
			kind := lego.CheckIntake
			if stocktake {
				kind = lego.CheckRecount
			}
			if by == "" {
				by = cliActor()
			}
			db.SetActor(by)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			c, err := db.NewCheck(ctx, rb, num, kind, by)
			if err != nil {
				return withCode(exitNotFound, err)
			}
			c.CheckedBy = by
			if kind == lego.CheckIntake {
				for i := range c.Lines {
					c.Lines[i].Have, c.Lines[i].Extra = c.Lines[i].Need, 0
				}
			}
			if err := applyEdits(c, missing, func(l *lego.CheckLine, n int) { l.Have = max(0, l.Need-n) }); err != nil {
				return err
			}
			if err := applyEdits(c, extra, func(l *lego.CheckLine, n int) { l.Extra = n }); err != nil {
				return err
			}
			extras, err := db.FinishCheck(c)
			if err != nil {
				return err
			}
			pieces, have, miss, ex, lines := c.Totals()
			_ = audit.New().Log(by, "", map[string]string{lego.CheckIntake: "SET_CHECKED", lego.CheckRecount: "STOCK_CHECK"}[kind], "SUCCESS",
				fmt.Sprintf("set=%s pieces=%d missing=%d extra=%d via cli", num, pieces, miss, ex))
			status := "COMPLETE"
			if miss > 0 {
				status = fmt.Sprintf("INCOMPLETE — %d missing on %d line(s)", miss, lines)
				notify.Dispatch("set_incomplete", fmt.Sprintf("Set %s is missing %d part(s)", num, miss), "checked by "+by)
			}
			say(ui.Fact(t, "Set", num+"  ("+kind+" check by "+by+")"))
			say(ui.Fact(t, "Pieces", fmt.Sprintf("%d, have %d, extra %d", pieces, have, ex)))
			say(ui.Status(t, miss == 0, status))
			out := map[string]any{"set": num, "kind": kind, "by": by, "pieces": pieces, "have": have, "missing": miss, "extra": ex}
			if !noPartDB {
				pdb, err := partdb.Open("")
				if err == nil {
					defer pdb.Close()
					syncer := &lego.PartSyncer{Lego: db, Writer: partdb.NewWriter(pdb)}
					name := ""
					if s, _ := db.CatalogSet(num); s != nil {
						name = s.Name
					}
					res, perr := syncer.PushSet(ctx, c, name)
					switch {
					case perr != nil:
						say(ui.Warn(t, "Part-DB not updated: "+perr.Error()))
					default:
						say(ui.Fact(t, "Part-DB", fmt.Sprintf("%d line(s) in its location, %d new part(s), %d changed, %d failed", res.Lines, res.Created, res.Changed, len(res.Errors))))
						out["partdb_location"] = res.LocationID
						for _, p := range extras {
							_, _ = syncer.Push(ctx, p)
						}
					}
				}
			}
			return emit(out)
		},
	}
	cmd.Flags().StringVar(&missing, "missing", "", "missing parts: part:colour:qty,… (colour = Rebrickable id or name)")
	cmd.Flags().StringVar(&extra, "extra", "", "extra parts: part:colour:qty,…")
	cmd.Flags().StringVar(&by, "by", "", "who checked it (default: your login)")
	cmd.Flags().BoolVar(&stocktake, "stocktake", false, "a recount of a checked set (starts from the last check)")
	cmd.Flags().BoolVar(&noPartDB, "no-partdb", false, "don't update Part-DB")
	return cmd
}

func newLegoShoppingCmd() *cobra.Command {
	var prices bool
	var format, outPath string
	cmd := &cobra.Command{
		Use:     "shopping <set>",
		Short:   "What a checked set is still missing, with BrickLink/BrickOwl prices, spares you hold and where to buy",
		Example: "  wms lego shopping 75192 --prices\n  wms lego shopping 75192 --export bricklink-xml -o falcon-wanted.xml",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			num := catalogNum(args[0])
			lines, err := db.ShoppingList(num)
			if err != nil {
				return withCode(exitNotFound, err)
			}
			if prices {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				var bl lego.PriceFetcher
				if c := newBLClient(db); c.Enabled() {
					bl = blFetcher{c}
				}
				n, errs := db.FetchPrices(ctx, lines, bl, brickowl.FromConfig(), strings.ToUpper(config.Get(config.BricklinkCondition)))
				say(t.Muted.Render(fmt.Sprintf("   fetched %d price(s)", n)))
				for _, e := range errs {
					say(ui.Warn(t, e.Error()))
				}
			}
			if format != "" {
				body, _, _, err := lego.Encode(format, db.ShoppingExport(num, lines))
				if err != nil {
					return usageError("%v", err)
				}
				if outPath == "" {
					_, _ = os.Stdout.Write(body)
					return nil
				}
				abs, err := writeExportFile(outPath, body, true)
				if err != nil {
					return err
				}
				say(ui.Status(t, true, "Wrote "+abs))
				return emit(map[string]any{"file": abs})
			}
			rows := [][]string{}
			total := 0.0
			var out []map[string]any
			for _, l := range lines {
				where, each := l.Cheapest()
				total += each * float64(l.ToBuy())
				rows = append(rows, []string{l.PartNum, orDash(l.ColorName), l.PartName, strconv.Itoa(l.Short), strconv.Itoa(l.OnOrder), strconv.Itoa(l.Spare), fmtPrice(l.BL), fmtPrice(l.BO), orDash(where)})
				links := map[string]string{}
				for _, lk := range db.ShopLinks(l) {
					links[lk[0]] = lk[1]
				}
				out = append(out, map[string]any{"part": l.PartNum, "colour": l.ColorName, "colour_id": l.ColorID, "element_id": l.ElementID, "short": l.Short, "on_order": l.OnOrder,
					"spare_elsewhere": l.Spare, "bricklink_avg": l.BL, "brickowl_avg": l.BO, "cheapest": where, "links": links})
			}
			say(ui.RenderColumns(t, []string{"Part", "Colour", "Name", "Short", "Ordered", "Spare", "BL avg", "BO avg", "Cheapest"}, rows,
				fmt.Sprintf("%s: %d line(s) short — cheapest known total %.2f", num, len(lines), total)))
			return emit(map[string]any{"set": num, "lines": out, "cheapest_total": total})
		},
	}
	cmd.Flags().BoolVar(&prices, "prices", false, "fetch prices older than a week from BrickLink and BrickOwl")
	cmd.Flags().StringVar(&format, "export", "", "write the list instead: bricklink-xml, xlsx, csv, json or html")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "with --export: the file (default: print it)")
	return cmd
}

func fmtPrice(v float64) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.3f", v)
}

func newLegoOrdersCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "orders", Short: "Orders for missing parts: supplier, prices, shipping, invoice, tracking, received"}
	list := &cobra.Command{
		Use: "list", Short: "All orders (--status wanted|ordered|shipped|received|cancelled|open)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			status, _ := cmd.Flags().GetString("status")
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			orders, err := db.ListOrders(status)
			if err != nil {
				return err
			}
			rows := [][]string{}
			for _, o := range orders {
				rows = append(rows, []string{"#" + strconv.FormatInt(o.ID, 10), o.SupplierKind + " " + o.Supplier, strings.ToUpper(o.Status), strconv.Itoa(o.Pieces()),
					fmt.Sprintf("%.2f %s", o.Total(), o.Currency), orDash(o.InvoiceNo), orDash(o.TrackingNo)})
			}
			say(ui.RenderColumns(ui.New(), []string{"Order", "Supplier", "Status", "Parts", "Total", "Invoice", "Tracking"}, rows, fmt.Sprintf("%d order(s)", len(rows))))
			return emit(orders)
		},
	}
	list.Flags().String("status", "", "only this status")
	show := &cobra.Command{
		Use: "show <id>", Short: "An order and its lines", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, o, err := openOrder(args[0])
			if err != nil {
				return err
			}
			defer db.Close()
			t := ui.New()
			say(ui.Fact(t, "Order", fmt.Sprintf("#%d %s %s — %s", o.ID, o.SupplierKind, o.Supplier, strings.ToUpper(o.Status))))
			say(ui.Fact(t, "Numbers", fmt.Sprintf("order %s · invoice %s · tracking %s %s", orDash(o.OrderNo), orDash(o.InvoiceNo), o.Carrier, orDash(o.TrackingNo))))
			rows := [][]string{}
			for _, l := range o.Lines {
				rows = append(rows, []string{strconv.FormatInt(l.ID, 10), orDash(l.SetNum), l.PartNum, orDash(l.ColorName), strconv.Itoa(l.Qty), fmt.Sprintf("%.3f", l.UnitPrice), fmt.Sprintf("%d", l.ReceivedQty)})
			}
			say(ui.RenderColumns(t, []string{"Line", "Set", "Part", "Colour", "Qty", "Each", "Received"}, rows, fmt.Sprintf("shipping %.2f · total %.2f %s", o.Shipping, o.Total(), o.Currency)))
			return emit(o)
		},
	}
	var o lego.Order
	var fromSet string
	newCmd := &cobra.Command{
		Use: "new", Short: "Start an order (--from-set adds everything that set is short)", Args: cobra.NoArgs,
		Example: "  wms lego orders new --kind brickowl --supplier 'Bricks R Us' --shipping 2.50 --from-set 75192",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			o.CreatedBy = cliActor()
			if o.Currency == "" {
				o.Currency = strings.ToUpper(config.Get(config.BricklinkCurrency))
			}
			if fromSet != "" {
				lines, _ := db.ShoppingList(catalogNum(fromSet))
				err = db.OrderFromMissing(catalogNum(fromSet), &o, func(cl lego.CheckLine) float64 {
					for _, l := range lines {
						if l.PartNum == cl.PartNum && l.ColorID == cl.ColorID {
							if o.SupplierKind == "brickowl" && l.BO > 0 {
								return l.BO
							}
							_, p := l.Cheapest()
							return p
						}
					}
					return 0
				})
			} else {
				err = db.SaveOrder(&o)
			}
			if err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("Order #%d created", o.ID)))
			return emit(map[string]any{"id": o.ID})
		},
	}
	f := newCmd.Flags()
	f.StringVar(&o.SupplierKind, "kind", "bricklink", "bricklink, brickowl, pab, rebrickable or other")
	f.StringVar(&o.Supplier, "supplier", "", "store / seller")
	f.StringVar(&o.OrderNo, "order-no", "", "order number")
	f.StringVar(&o.InvoiceNo, "invoice", "", "invoice number")
	f.StringVar(&o.TrackingNo, "tracking", "", "tracking number")
	f.StringVar(&o.Carrier, "carrier", "", "carrier")
	f.StringVar(&o.Currency, "currency", "", "currency (default BRICKLINK_CURRENCY)")
	f.Float64Var(&o.Shipping, "shipping", 0, "shipping cost")
	f.StringVar(&o.Note, "note", "", "note")
	f.StringVar(&fromSet, "from-set", "", "add a line for everything this checked set is short")
	var line lego.OrderLine
	addLine := &cobra.Command{
		Use: "add-line <id>", Short: "Add a part to an order", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, ord, err := openOrder(args[0])
			if err != nil {
				return err
			}
			defer db.Close()
			if line.SetNum != "" {
				line.SetNum = catalogNum(line.SetNum)
			}
			if err := db.AddOrderLine(ord.ID, line); err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, "Added"))
			return nil
		},
	}
	lf := addLine.Flags()
	lf.StringVar(&line.PartNum, "part", "", "part number")
	lf.IntVar(&line.ColorID, "color", -1, "Rebrickable colour id")
	lf.IntVar(&line.Qty, "qty", 1, "quantity")
	lf.Float64Var(&line.UnitPrice, "price", 0, "price each")
	lf.StringVar(&line.SetNum, "set", "", "the set it is for (receiving it fills that set)")
	status := &cobra.Command{
		Use: "status <id> <wanted|ordered|shipped|received|cancelled>", Short: "Move an order along (received fills the sets it was for)", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, ord, err := openOrder(args[0])
			if err != nil {
				return err
			}
			defer db.Close()
			done, err := db.SetOrderStatus(ord.ID, strings.ToLower(args[1]))
			if err != nil {
				return usageError("%v", err)
			}
			t := ui.New()
			say(ui.Status(t, true, fmt.Sprintf("Order #%d is now %s", ord.ID, strings.ToUpper(args[1]))))
			switch strings.ToLower(args[1]) {
			case "shipped":
				notify.Dispatch("order_shipped", fmt.Sprintf("Order #%d from %s has shipped", ord.ID, ord.Supplier), ord.Carrier+" "+ord.TrackingNo)
			case "received":
				notify.Dispatch("order_received", fmt.Sprintf("Order #%d from %s has arrived", ord.ID, ord.Supplier), "")
			}
			for _, s := range done {
				say(ui.Status(t, true, "Set "+s+" is now COMPLETE"))
				notify.Dispatch("set_complete", "Set "+s+" is complete", "All missing parts have arrived.")
			}
			return emit(map[string]any{"id": ord.ID, "status": args[1], "completed_sets": done})
		},
	}
	cmd.AddCommand(list, show, newCmd, addLine, status)
	return cmd
}

func openOrder(arg string) (*lego.DB, *lego.Order, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(arg, "#"), 10, 64)
	if err != nil {
		return nil, nil, usageError("%q is not an order number", arg)
	}
	db, err := openLego()
	if err != nil {
		return nil, nil, err
	}
	o, err := db.GetOrder(id)
	if err != nil {
		db.Close()
		return nil, nil, withCode(exitNotFound, err)
	}
	return db, o, nil
}

func newLegoSpendCmd() *cobra.Command {
	var by string
	cmd := &cobra.Command{
		Use: "spend", Short: "What missing parts have cost (parts + shipping) by set, supplier or month", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			rows, err := db.Spend(by)
			if err != nil {
				return err
			}
			out := [][]string{}
			for _, r := range rows {
				out = append(out, []string{r.Key, strconv.Itoa(r.Orders), strconv.Itoa(r.Pieces), fmt.Sprintf("%.2f", r.Parts), fmt.Sprintf("%.2f", r.Shipping), fmt.Sprintf("%.2f %s", r.Parts+r.Shipping, r.Currency)})
			}
			say(ui.RenderColumns(ui.New(), []string{strings.ToUpper(by[:1]) + by[1:], "Orders", "Parts", "Parts cost", "Shipping", "Total"}, out, "spend by "+by))
			return emit(rows)
		},
	}
	cmd.Flags().StringVar(&by, "by", "set", "set, supplier or month")
	return cmd
}

func newLegoLabelsCmd() *cobra.Command {
	var size, format, outPath string
	cmd := &cobra.Command{
		Use:   "labels <set...|all|incomplete>",
		Short: "Printable set labels (thermal 4x6/100x150/62mm/50x30/40x30, or A4/Letter sheets)",
		Long: "Each label: set number, name, year, pieces, missing / on order, who checked it and when, location, a QR code\n" +
			"and a Code 128 barcode. Print the PDF at 100% with no margins. Sizes:\n" + labelSizes(),
		Example: "  wms lego labels 75192 --size 4x6 -o falcon.pdf\n  wms lego labels incomplete --size a4 --format html -o sheet.html",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			s, err := labels.SizeByID(size)
			if err != nil {
				return usageError("%v", err)
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			sets, err := db.LabelSets(args)
			if err != nil {
				return err
			}
			if len(sets) == 0 {
				return withCode(exitNotFound, fmt.Errorf("no sets to label"))
			}
			var items []labels.Data
			for _, n := range sets {
				items = append(items, db.LabelData(n))
			}
			var body []byte
			switch strings.ToLower(format) {
			case "pdf":
				body = labels.PDF(items, s)
			case "html":
				body = labels.HTML(items, s)
			default:
				return usageError("--format must be pdf or html")
			}
			if outPath == "" {
				_, _ = os.Stdout.Write(body)
				return nil
			}
			abs, err := writeExportFile(outPath, body, true)
			if err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("Wrote %d label(s) to %s", len(items), abs)))
			return emit(map[string]any{"file": abs, "labels": len(items), "size": s.ID})
		},
	}
	cmd.Flags().StringVar(&size, "size", "4x6", "label stock (see above)")
	cmd.Flags().StringVar(&format, "format", "pdf", "pdf or html")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output file (default: print it)")
	return cmd
}

func labelSizes() string {
	var b strings.Builder
	for _, s := range labels.Sizes {
		fmt.Fprintf(&b, "  %-8s %s\n", s.ID, s.Name)
	}
	return b.String()
}

func newLegoSetInfoCmd() *cobra.Command {
	var location, condition, note string
	cmd := &cobra.Command{
		Use: "set-info <set>", Short: "Where a set is kept and its condition (sealed, built, in pieces, displayed)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			num := catalogNum(args[0])
			st := db.GetSetState(num)
			if !cmd.Flags().Changed("location") {
				location = st.Location
			}
			if !cmd.Flags().Changed("condition") {
				condition = st.Condition
			}
			if !cmd.Flags().Changed("note") {
				note = st.ConditionNote
			}
			if err := db.SetInfo(num, location, strings.ToLower(condition), note); err != nil {
				return err
			}
			st = db.GetSetState(num)
			t := ui.New()
			say(ui.Fact(t, "Set", num))
			say(ui.Fact(t, "Location", orDash(st.Location)))
			say(ui.Fact(t, "Condition", orDash(strings.TrimSpace(st.Condition+" "+st.ConditionNote))))
			check := "not checked"
			if c := st.LastCheck; c != nil {
				check = fmt.Sprintf("%s by %s — %d missing, %d on order", c.FinishedAt.Local().Format("2 Jan 2006"), c.CheckedBy, st.MissingQty, st.OnOrderQty)
			}
			say(ui.Fact(t, "Last check", check))
			return emit(st)
		},
	}
	cmd.Flags().StringVar(&location, "location", "", "shelf, box or bin")
	cmd.Flags().StringVar(&condition, "condition", "", "sealed, built, in pieces or displayed")
	cmd.Flags().StringVar(&note, "note", "", "condition note")
	return cmd
}
