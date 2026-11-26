package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/publicblog"
)

func blogAudioDir() string {
	return filepath.Join(filepath.Dir(config.Get(config.LegoDBPath)), "blog-audio")
}

func newPublicBlogCmd() *cobra.Command {
	var listen, baseURL, dashURL string
	var perMinute int
	cmd := &cobra.Command{
		Use:   "public-blog",
		Short: "Serve the public sets blog (published posts only, read-only, rate limited)",
		Long: "Serves the public sets site from a read-only handle on lego.db: only posts an admin has approved, their\n" +
			"public photos and a one-line parts-check result. Listens on loopback by default; publish it through a\n" +
			"Cloudflare tunnel route (e.g. sets.kcparts.xyz). It shares nothing with the dashboard API.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := publicblog.OpenReadOnly(config.Get(config.LegoDBPath))
			if err != nil {
				return err
			}
			defer db.Close()
			att := config.Get(config.LegoAttachmentsDir)
			if att == "" {
				att = filepath.Join(filepath.Dir(config.Get(config.LegoDBPath)), "attachments")
			}
			h := publicblog.Handler(publicblog.Config{DB: db, AttachmentsDir: att, AudioDir: blogAudioDir(), BaseURL: baseURL, DashboardURL: dashURL, PerMinute: perMinute})
			srv := &http.Server{Addr: listen, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = srv.Shutdown(shut)
			}()
			say("public sets blog on " + listen)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:8096", "address to listen on (keep it on loopback; publish through a tunnel)")
	cmd.Flags().StringVar(&dashURL, "dashboard-url", "https://dashboard.kcparts.xyz", "where staff sign in (the dashboard, which has its own login); a link only, this site has no login")
	cmd.Flags().StringVar(&baseURL, "base-url", "https://sets.kcparts.xyz", "public address, used in the feed and sitemap")
	cmd.Flags().IntVar(&perMinute, "per-minute", 120, "requests allowed per client per minute")
	return cmd
}

func newBlogCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "blog", Short: "Manage public blog posts"}
	var dir string
	sync := &cobra.Command{
		Use:   "sync",
		Short: "Import Markdown posts from a git checkout into the review queue (never straight to live)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := os.Stat(dir); err != nil {
				return err
			}
			db, err := lego.Open(config.Get(config.LegoDBPath))
			if err != nil {
				return err
			}
			defer db.Close()
			lines, err := db.SyncPostsDir(dir, blogAudioDir(), "git")
			if err != nil {
				return err
			}
			if len(lines) == 0 {
				say("nothing new to import")
			}
			for _, l := range lines {
				say(l)
			}
			return nil
		},
	}
	sync.Flags().StringVar(&dir, "dir", "/root/kc-blog/posts", "folder of .md posts (and audio/) to import")
	cmd.AddCommand(sync)
	return cmd
}
