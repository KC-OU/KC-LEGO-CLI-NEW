package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/gateway"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
)

func newGatewayCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "gateway", Short: "Telnet + web-terminal gateway (secondary to the CLI)"}
	cmd.AddCommand(newGatewayServeCmd())
	return cmd
}

func newGatewayServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Telnet daemon and the ttyd-fronted web terminal together",
		RunE: func(cmd *cobra.Command, args []string) error {
			wmsBinaryPath, err := os.Executable()
			if err != nil {
				return err
			}

			ports, err := parsePorts(config.Get(config.ListenPorts))
			if err != nil {
				return err
			}
			gatewayPort, err := strconv.Atoi(config.Get(config.GatewayPort))
			if err != nil {
				return err
			}
			ttydPort, err := strconv.Atoi(config.Get(config.TTYDPort))
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if sender := notify.FromConfig(); sender != nil {
				if err := sender.Validate(); err != nil {
					return err
				}
			}
			if notify.Enabled() { // notify channels and/or the ntfy / webhook sender
				mon := notify.NewMonitor(notify.Routed, audit.New(), config.Get(config.ModernWMSBackupDir))
				mon.LowStock = lowStockLines
				mon.PriceDrops = priceDropLines
				mon.OnEvent = func(event string, lines []string) {
					go pluginManager().Emit(ctx, event, map[string]any{"lines": lines})
				}
				go mon.Run(ctx)
			}

			if hours, err := strconv.Atoi(strings.TrimSpace(config.Get(config.CatalogAutoRefreshHours))); err == nil && hours > 0 {
				go autoRefreshCatalog(ctx, time.Duration(max(hours, 24))*time.Hour)
			}

			g, gctx := errgroup.WithContext(ctx)
			host := config.Get(config.GatewayListenHost)
			g.Go(func() error { return gateway.RunTelnetServer(gctx, host, ports, wmsBinaryPath) })
			g.Go(func() error { return gateway.RunWebGateway(gctx, host, gatewayPort, ttydPort, wmsBinaryPath) })
			return g.Wait()
		},
	}
}

// autoRefreshCatalog keeps the offline catalog current (CATALOG_AUTO_REFRESH_HOURS, off by default).
// Rebrickable allows automated downloads once a day, so the interval is never shorter than that,
// and RefreshCatalog itself skips files that have not changed.
func autoRefreshCatalog(ctx context.Context, every time.Duration) {
	logger := audit.New()
	wait := time.Minute // let the gateway settle first
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = every
		db, err := openLego()
		if err != nil {
			logger.Log("system", "", "CATALOG_AUTO_REFRESH", "FAILED", err.Error())
			continue
		}
		res, err := db.RefreshCatalog(ctx, lego.CatalogOptions{})
		db.Close()
		if err != nil {
			logger.Log("system", "", "CATALOG_AUTO_REFRESH", "FAILED", err.Error())
			continue
		}
		logger.Log("system", "", "CATALOG_AUTO_REFRESH", "SUCCESS", res.Message)
		if res.Updated > 0 {
			go pluginManager().Emit(ctx, "catalog_refreshed", map[string]any{"files_updated": res.Updated, "rows": res.Rows})
		}
	}
}

func parsePorts(csv string) ([]int, error) {
	parts := strings.Split(csv, ",")
	ports := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		ports = append(ports, n)
	}
	return ports, nil
}
