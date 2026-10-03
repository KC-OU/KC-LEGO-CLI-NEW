package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/botapi"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/gateway"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/metrics"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/mobileapi"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
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
			if hours, err := strconv.Atoi(strings.TrimSpace(config.Get(config.RetirementAutoRefreshHours))); err == nil && hours > 0 {
				go retirementAutoRefresh(ctx, time.Duration(hours)*time.Hour)
			}

			testBinaryPath := config.Get(config.TestBinaryPath)
			var testEnv []string
			if testBinaryPath != "" {
				if envFile := config.Get(config.TestEnvFile); envFile != "" {
					if testEnv, err = config.ReadEnvFile(envFile); err != nil {
						return fmt.Errorf("reading WMS_TEST_ENV_FILE: %w", err)
					}
				} else {
					testBinaryPath = "" // both or neither — see docs/guides/telnet-and-web.md
				}
			}

			g, gctx := errgroup.WithContext(ctx)
			host := config.Get(config.GatewayListenHost)
			g.Go(func() error {
				return gateway.RunTelnetServer(gctx, host, ports, wmsBinaryPath, testBinaryPath, testEnv)
			})

			// botSrv is shared between the loopback-only /bot/action webhook
			// (n8n or any local script) and the public /discord/interactions
			// route (a Discord slash command, no n8n needed) — one Server, so
			// neither path can reach anything the other can't. Built once,
			// gated on either being configured, so Discord-only setups don't
			// need WMS_BOT_API_PORT set too.
			botAPIPort := strings.TrimSpace(config.Get(config.BotAPIPort))
			discordPublicKey := strings.TrimSpace(config.Get(config.DiscordPublicKey))
			var botSrv *botapi.Server
			if botAPIPort != "" || discordPublicKey != "" {
				legoDB, err := openLego()
				if err != nil {
					return fmt.Errorf("opening lego db for the bot API: %w", err)
				}
				legoDB.SetActor("botapi")
				defer legoDB.Close()
				botSrv = botapi.NewServer(legoDB, audit.New())
			}

			// The /exports dashboard (a browsable alternative to a one-off /dl/
			// link — see internal/gateway/exports_dashboard.go) needs the same
			// account check the TUI's own sign-on screen uses; cheap to open
			// unconditionally, same as botapi's own legoDB above.
			webPDB, err := partdb.Open("")
			if err != nil {
				return fmt.Errorf("opening Part-DB for the web gateway's exports page: %w", err)
			}
			defer webPDB.Close()

			g.Go(func() error {
				return gateway.RunWebGateway(gctx, host, gatewayPort, ttydPort, wmsBinaryPath, testBinaryPath, testEnv, strings.TrimSpace(config.Get(config.WebSoloDirectPort)), botSrv, discordPublicKey, wmsdb.NewClient(), webPDB)
			})
			if port := strings.TrimSpace(config.Get(config.MetricsPort)); port != "" {
				g.Go(func() error { return metrics.Serve(gctx, "0.0.0.0:"+port) })
			}
			if botAPIPort != "" {
				g.Go(func() error { return botapi.Serve(gctx, "127.0.0.1:"+botAPIPort, botSrv) })
			}
			// The mobile pick/check app's API — off until WMS_MOBILE_API_PORT is
			// set, same convention as the bot API. Binds GatewayListenHost, not
			// loopback-only: a phone has to reach it (through a tunnel, same as
			// the rest of this gateway), unlike the bot API's loopback-only trust
			// model built for a local script/n8n.
			if mobileAPIPort := strings.TrimSpace(config.Get(config.MobileAPIPort)); mobileAPIPort != "" {
				pdb, err := partdb.Open("")
				if err != nil {
					return fmt.Errorf("opening Part-DB for the mobile API: %w", err)
				}
				defer pdb.Close()
				legoDB, err := openLego()
				if err != nil {
					return fmt.Errorf("opening lego db for the mobile API: %w", err)
				}
				legoDB.SetActor("mobileapi")
				defer legoDB.Close()
				mobileSrv := mobileapi.NewServer(wmsdb.NewClient(), pdb, legoDB, audit.New())
				g.Go(func() error { return mobileapi.Serve(gctx, host+":"+mobileAPIPort, mobileSrv) })
			}
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
