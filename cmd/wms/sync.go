package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/api"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/sync"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

func newSyncEngine() (*sync.Engine, error) {
	db, err := partdb.Open("")
	if err != nil {
		return nil, err
	}
	return sync.NewEngine(wmsdb.NewClient(), db), nil
}

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "PartDB -> ModernWMS sync engine: status, trigger, or serve the REST API",
	}
	cmd.AddCommand(newSyncStatusCmd(), newSyncTriggerCmd(), newSyncServeCmd(), newSyncSetPasswordCmd())
	return cmd
}

func newSyncStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print the last sync run's stats",
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			engine, err := newSyncEngine()
			if err != nil {
				return err
			}
			stats := engine.Stats()
			if stats.LastResult == nil {
				say(ui.Warn(t, "No sync has run yet in this process — run 'wms sync trigger'."))
				return emit(map[string]any{"last_result": nil})
			}
			printSyncResult(t, *stats.LastResult)
			return emit(map[string]any{"last_result": stats.LastResult})
		},
	}
}

func newSyncTriggerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trigger",
		Short: "Run one sync cycle now and print the result",
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			engine, err := newSyncEngine()
			if err != nil {
				return err
			}
			result, err := engine.SyncNow(context.Background())
			if err != nil {
				fmt.Println(ui.Status(t, false, err.Error()))
				return err
			}
			fmt.Println(ui.Status(t, true, "Sync complete"))
			printSyncResult(t, *result)
			return nil
		},
	}
}

func printSyncResult(t ui.Theme, r sync.SyncResult) {
	say(ui.RenderColumns(t, []string{"Field", "Value"}, [][]string{
		{"Timestamp", r.Timestamp.Format("2006-01-02 15:04:05")},
		{"Duration", fmt.Sprintf("%.1f ms", r.DurationMS)},
		{"Categories Synced", strconv.Itoa(r.SyncedCategories)},
		{"Parts Synced", strconv.Itoa(r.SyncedParts)},
		{"Stock Records Synced", strconv.Itoa(r.SyncedStockRecords)},
		{"Total Stock Qty", strconv.Itoa(r.TotalStockQty)},
	}, "Sync Result"))
}

func newSyncServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the REST API + /metrics and the background sync loop (blocks)",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := newSyncEngine()
			if err != nil {
				return err
			}
			db, err := partdb.Open("")
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			srv := api.NewHTTPServer(engine, db, "", "")
			go engine.BackgroundLoop(ctx, 2*time.Second)

			errCh := make(chan error, 1)
			go func() { errCh <- srv.ListenAndServe() }()

			select {
			case <-ctx.Done():
				return srv.Close()
			case err := <-errCh:
				return err
			}
		},
	}
}

func newSyncSetPasswordCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-password",
		Short: "Set the sync dashboard's REST API credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			reader := bufio.NewReader(os.Stdin)

			fmt.Print("Username [admin]: ")
			username, _ := reader.ReadString('\n')
			username = strings.TrimSpace(username)
			if username == "" {
				username = "admin"
			}

			password, err := readPasswordTwice()
			if err != nil {
				return err
			}
			if len(password) < 6 {
				return fmt.Errorf("password must be at least 6 characters")
			}

			keyHex, saltHex := auth.HashPBKDF2(password, nil)
			path := config.Get(config.CredentialsFile)
			if err := writeCredentialsFile(path, username, keyHex, saltHex); err != nil {
				return err
			}
			fmt.Println(ui.Status(t, true, "Credentials updated"))
			fmt.Println(ui.Fact(t, "File", path))
			return nil
		},
	}
}

func readPasswordTwice() (string, error) {
	fmt.Print("New password: ")
	p1, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	fmt.Print("Confirm password: ")
	p2, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	if string(p1) != string(p2) {
		return "", fmt.Errorf("passwords do not match")
	}
	return string(p1), nil
}

// writeCredentialsFile matches internal/api's unexported credentialStore
// format exactly ({username, password_hash, salt, updated_at}) so this CLI
// path and the running server's /api/change-password stay interchangeable.
func writeCredentialsFile(path, username, keyHex, saltHex string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]string{
		"username":      username,
		"password_hash": keyHex,
		"salt":          saltHex,
		"updated_at":    time.Now().Format("2006-01-02T15:04:05Z07:00"),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
