package main

import (
	"context"
	"fmt"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/bricklink"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/brickset"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/dashboardapi"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
)

// roundBLoop is the gateway's background work for the selling side (and a daily
// Brickset refresh of retiring-set dates and LEGO.com prices): import new
// BrickLink store orders every few minutes (alerting on each new one), and once
// a day report owned parts whose price moved notably. Both are no-ops without
// BrickLink credentials. Failures are logged by the next tick succeeding; an
// alert is best effort, never a reason to stop the gateway.
func roundBLoop(ctx context.Context, db *lego.DB, bl *bricklink.Client) error {
	feed := dashboardapi.NewBLSalesFeed(bl)
	orders := time.NewTicker(5 * time.Minute)
	movers := time.NewTicker(24 * time.Hour)
	retail := time.NewTicker(24 * time.Hour)
	defer orders.Stop()
	defer movers.Stop()
	defer retail.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-retail.C:
			if bs := brickset.FromConfig(); bs.Enabled() {
				if _, changes, err := dashboardapi.RefreshRetail(ctx, db, bs); err == nil {
					dashboardapi.NotifyRetailChanges(changes)
				}
			}
		case <-orders.C:
			if !bl.Enabled() {
				continue
			}
			if n, err := db.SyncSalesOrders(ctx, feed); err == nil && n > 0 {
				notify.Dispatch("order_received", fmt.Sprintf("%d new BrickLink order(s)", n), "Open Sales orders to pick them.")
			}
		case <-movers.C:
			if !bl.Enabled() {
				continue
			}
			if m, err := db.PriceMovers("U", 7, 20, 5); err == nil && len(m) > 0 {
				top := m[0]
				notify.Dispatch("price_drop", fmt.Sprintf("%d part price(s) moved", len(m)),
					fmt.Sprintf("Biggest: %s %.0f%% (%.2f -> %.2f) across %d held.", top.Label, top.Pct, top.From, top.To, top.Qty))
			}
		}
	}
}
