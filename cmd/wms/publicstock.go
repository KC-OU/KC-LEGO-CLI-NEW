package main

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/publicstock"
)

func newPublicStockCmd() *cobra.Command {
	var listen string
	var perMinute int
	cmd := &cobra.Command{
		Use:   "public-stock",
		Short: "Serve a read-only 'do you have part X?' endpoint for customers (status only, rate limited)",
		Long: "Answers GET /stock?part=3001 with in stock / low stock / out of stock -- never a quantity, price or\n" +
			"location -- from a read-only handle on lego.db. It listens on loopback by default; put a Cloudflare\n" +
			"tunnel route in front of it to publish it. It shares nothing with the dashboard API.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := publicstock.OpenReadOnly(config.Get(config.LegoDBPath))
			if err != nil {
				return err
			}
			defer db.Close()
			srv := &http.Server{Addr: listen, Handler: publicstock.Handler(db, perMinute),
				ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = srv.Shutdown(shut)
			}()
			say("public stock lookup on " + listen)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:8095", "address to listen on (keep it on loopback; publish through a tunnel)")
	cmd.Flags().IntVar(&perMinute, "per-minute", 30, "requests allowed per client per minute")
	return cmd
}
