package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

func newReceiveCmd() *cobra.Command {
	var as string
	cmd := &cobra.Command{
		Use:   "receive <part-name-or-code-or-id> <quantity>",
		Short: "Receive stock instantly via an ASN ticket, no login/menu needed",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			qty, err := strconv.Atoi(args[1])
			if err != nil {
				return usageError("quantity must be a valid integer")
			}
			result, err := wmsdb.NewClient().ReceiveStock(context.Background(), args[0], qty, as)
			if err != nil {
				say(ui.Status(t, false, err.Error()))
				return err
			}
			say(ui.RenderColumns(t, []string{"Field", "Value"}, [][]string{
				{"Receipt Ticket (ASN)", result.AsnNo},
				{"Part", fmt.Sprintf("%s (#%d)", result.SpuName, result.PartID)},
				{"Commodity Code", result.SpuCode},
				{"Supplier / Location", "KCLEGO (Main Warehouse)"},
				{"Quantity Added", fmt.Sprintf("+%d pcs", result.AddedQty)},
				{"Previous Stock", strconv.Itoa(result.PreviousQty)},
				{"New Stock Total", strconv.Itoa(result.NewQty)},
			}, "Stock Receipt Complete (Notice on Arrival / ASN)"))
			return emit(map[string]any{
				"asn": result.AsnNo, "part_id": result.PartID, "part": result.SpuName, "code": result.SpuCode,
				"added": result.AddedQty, "previous": result.PreviousQty, "new_total": result.NewQty,
			})
		},
	}
	cmd.Flags().StringVar(&as, "as", envOr("WMS_USER", "cli"), "user_num recorded as the ASN creator")
	return cmd
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
