package main

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func newNotifyCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "notify", Short: "Opt-in alerts to an ntfy topic or a webhook (set NOTIFY_URL to turn them on)"}
	cmd.AddCommand(&cobra.Command{
		Use:   "test",
		Short: "Send a test alert to NOTIFY_URL",
		Long: "Alerts are off unless NOTIFY_URL is set (an ntfy topic such as https://ntfy.sh/your-secret-topic, or any\n" +
			"webhook that accepts JSON; NOTIFY_FORMAT=ntfy|json overrides the guess). The gateway then reports failed\n" +
			"sign-in bursts, a broken audit chain, a stale backup, low stock and, daily, the audit chain's head hash.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			s := notify.FromConfig()
			if s == nil {
				return usageError("alerts are off: set NOTIFY_URL (for the gateway service, add it to its environment file)")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := s.Send(ctx, notify.Message{Title: "wms-go test alert", Body: "If you can read this, alerts work.", Priority: 3, Tag: "white_check_mark"}); err != nil {
				say(ui.Status(t, false, err.Error()))
				return withCode(exitNetwork, errors.New("test alert failed: "+err.Error()))
			}
			say(ui.Status(t, true, "Test alert sent"))
			return emit(map[string]any{"sent": true, "format": s.Format})
		},
	})
	return cmd
}
