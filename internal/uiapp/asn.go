package uiapp

import (
	"fmt"
	"strconv"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func asnScreen() screenModel {
	return &formScreen{
		panelID:      "ASNRCV",
		title:        "Inbound ASN — Stock Receiving",
		writeGated:   true,
		deniedAction: "RECEIVE_STOCK_ASN",
		build: func(app *App) []ui.Field {
			return []ui.Field{
				{Label: "Commodity Code / SPU Name / ID"},
				{Label: "Quantity"},
			}
		},
		submit: func(app *App, values []string) {
			qty, err := strconv.Atoi(values[1])
			if err != nil {
				app.setMsg("Quantity must be a valid integer.", true)
				return
			}
			result, err := app.wms.ReceiveStock(app.ctx(), values[0], qty, app.session.Username)
			if err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.session.Username, app.session.Role, "RECEIVE_STOCK_ASN", "SUCCESS",
				fmt.Sprintf("asn=%s part=%d qty=%d", result.AsnNo, result.PartID, result.AddedQty))
			app.setMsg(fmt.Sprintf("Receipt %s complete: %s now %d (+%d).", result.AsnNo, result.SpuName, result.NewQty, result.AddedQty), false)
			app.onBack()
		},
	}
}
