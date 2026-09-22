package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func newNotifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Alerts to Discord, Slack, Telegram, Teams, email, Pushover, Gotify, WhatsApp (webhook) or ntfy",
		Long: "Channels come from projectdiscovery/notify's provider-config.yaml at WMS_NOTIFY_PROVIDERS (default\n" +
			"/root/docker-server/wms/notify-provider-config.yaml); each entry's id is a channel. NOTIFY_URL adds the\n" +
			"channel \"ntfy\" (an ntfy topic or a JSON webhook) and is the fallback when there is no provider file.\n" +
			"Events: " + strings.Join(notify.Events, ", ") + ". By default an event goes to every channel;\n" +
			"`wms notify route` narrows it.",
	}
	var channel string
	test := &cobra.Command{
		Use: "test", Short: "Send a test alert to every channel (or --channel id) and report each result", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			chans, ferr := notify.Channels()
			if ferr != nil {
				say(ui.Warn(t, "provider file not used: "+ferr.Error()))
			}
			if len(chans) == 0 {
				return usageError("no channels: add a provider file at %s, or set NOTIFY_URL", config.Get(config.NotifyProviders))
			}
			failed := 0
			var out []map[string]any
			for _, c := range chans {
				if channel != "" && c.ID != channel {
					continue
				}
				err := c.Send("KC-PARTS test alert\nIf you can read this, the " + c.Kind + " channel \"" + c.ID + "\" works.")
				say(ui.Status(t, err == nil, fmt.Sprintf("%-14s %-10s %s", c.ID, c.Kind, errText(err))))
				out = append(out, map[string]any{"channel": c.ID, "kind": c.Kind, "ok": err == nil, "error": errText(err)})
				if err != nil {
					failed++
				}
			}
			if err := emit(out); err != nil {
				return err
			}
			if failed > 0 {
				return withCode(exitNetwork, errors.New("some channels failed"))
			}
			return nil
		},
	}
	test.Flags().StringVar(&channel, "channel", "", "only this channel id")
	channels := &cobra.Command{
		Use: "channels", Short: "List the channels and where each event goes", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			chans, ferr := notify.Channels()
			if ferr != nil {
				say(ui.Warn(t, "provider file not used: "+ferr.Error()))
			}
			rows := [][]string{}
			for _, c := range chans {
				rows = append(rows, []string{c.ID, c.Kind})
			}
			say(ui.RenderColumns(t, []string{"Channel", "Kind"}, rows, fmt.Sprintf("%d channel(s)", len(rows))))
			routes := map[string][]string{}
			for _, e := range notify.Events {
				var ids []string
				for _, c := range notify.Route(e, chans) {
					ids = append(ids, c.ID)
				}
				routes[e] = ids
				say(ui.Fact(t, fmt.Sprintf("%-15s", e), orDash(strings.Join(ids, ", "))))
			}
			return emit(map[string]any{"channels": rows, "routes": routes})
		},
	}
	route := &cobra.Command{
		Use: "route <event> <channel,...|all>", Short: "Send an event only to these channels (all = every channel)", Args: cobra.ExactArgs(2),
		Example: "  wms notify route security email,discord\n  wms notify route set_complete whatsapp\n  wms notify route set_complete all",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !slices.Contains(notify.Events, args[0]) {
				return usageError("event must be one of %s", strings.Join(notify.Events, ", "))
			}
			ids := splitList(args[1])
			if slices.Contains(ids, "all") {
				ids = nil
			}
			_, err := changePolicy("notify route "+args[0]+"="+args[1], func(p *access.Policy) error {
				if p.Settings.NotifyRoutes == nil {
					p.Settings.NotifyRoutes = map[string][]string{}
				}
				if ids == nil {
					delete(p.Settings.NotifyRoutes, args[0])
				} else {
					p.Settings.NotifyRoutes[args[0]] = ids
				}
				return nil
			})
			if err == nil {
				say(ui.Status(ui.New(), true, args[0]+" → "+args[1]))
			}
			return err
		},
	}
	cmd.AddCommand(test, channels, route)
	return cmd
}

func errText(err error) string {
	if err == nil {
		return "sent"
	}
	return err.Error()
}
