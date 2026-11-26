package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/dashboardapi"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func newSalesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sales", Short: "Orders your customers placed in your BrickLink store (import, list, pick list)"}
	cmd.AddCommand(&cobra.Command{
		Use: "sync", Short: "Import new BrickLink store orders", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			bl := newBLClient(db)
			if !bl.Enabled() {
				return blExit(usageError("BrickLink is not configured"))
			}
			n, err := db.SyncSalesOrders(context.Background(), dashboardapi.NewBLSalesFeed(bl))
			if err != nil {
				return blExit(err)
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("%d new order(s) imported", n)))
			return emit(map[string]int{"added": n})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "list", Short: "Open sales orders, newest first", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			orders, err := db.ListSalesOrders(true)
			if err != nil {
				return err
			}
			rows := [][]string{}
			for _, o := range orders {
				rows = append(rows, []string{o.OrderID, o.Buyer, o.Status, strconv.Itoa(o.Pieces), fmt.Sprintf("%.2f %s", o.Total, o.Currency), orDash(o.PickedAt)})
			}
			say(ui.RenderColumns(ui.New(), []string{"Order", "Buyer", "Status", "Pieces", "Total", "Picked"}, rows, fmt.Sprintf("%d open", len(rows))))
			return emit(orders)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "pick <order>", Short: "An order's lines in shelf order", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			pdb, _ := partdb.Open("") // nil when Part-DB is unreachable: lines then simply have no location
			if pdb != nil {
				defer pdb.Close()
			}
			o, err := db.SalesPickList(pdb, args[0])
			if err != nil {
				return err
			}
			if o == nil {
				return withCode(exitNotFound, fmt.Errorf("no sales order %s", args[0]))
			}
			rows := [][]string{}
			for _, l := range o.Lines {
				rows = append(rows, []string{orDash(l.Location), l.ItemNo, l.ColorName, strconv.Itoa(l.Qty), l.Cond, strconv.Itoa(l.Picked)})
			}
			say(ui.RenderColumns(ui.New(), []string{"Where", "Part", "Colour", "Qty", "Cond", "Picked"}, rows, fmt.Sprintf("order %s for %s", o.OrderID, o.Buyer)))
			return emit(o)
		},
	})
	return cmd
}

func newAcquireCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "acquire", Short: "Follow a purchase from bought to listed, with what it cost"}
	var supplier, set, currency, note string
	var cost float64
	add := &cobra.Command{
		Use: "add <label>", Short: "Record a purchase", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			id, err := db.AddAcquisition(lego.Acquisition{Label: args[0], Supplier: supplier, SetNum: set, Currency: currency, Note: note, Cost: cost, CreatedBy: "cli"})
			if err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("acquisition #%d recorded (purchased)", id)))
			return emit(map[string]int64{"id": id})
		},
	}
	add.Flags().StringVar(&supplier, "supplier", "", "where it was bought")
	add.Flags().StringVar(&set, "set", "", "set number, if it is a set")
	add.Flags().StringVar(&currency, "currency", "GBP", "currency of --cost")
	add.Flags().StringVar(&note, "note", "", "free text")
	add.Flags().Float64Var(&cost, "cost", 0, "what it cost, shipping included")
	advance := &cobra.Command{
		Use: "advance <id>", Short: "Move to the next stage (purchased > received > checked > shelved > listed)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return usageError("id must be a number")
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			stage, err := db.AdvanceAcquisition(id)
			if err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("#%d is now %s", id, stage)))
			return emit(map[string]string{"stage": stage})
		},
	}
	list := &cobra.Command{
		Use: "list [stage]", Short: "Acquisitions, newest first", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stage := ""
			if len(args) == 1 {
				stage = args[0]
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			items, err := db.ListAcquisitions(stage)
			if err != nil {
				return err
			}
			rows := [][]string{}
			for _, a := range items {
				rows = append(rows, []string{strconv.FormatInt(a.ID, 10), a.Label, a.Stage, fmt.Sprintf("%.2f %s", a.Cost, a.Currency), a.Supplier})
			}
			say(ui.RenderColumns(ui.New(), []string{"#", "What", "Stage", "Cost", "From"}, rows, fmt.Sprintf("%d", len(rows))))
			return emit(items)
		},
	}
	cmd.AddCommand(add, advance, list)
	return cmd
}
