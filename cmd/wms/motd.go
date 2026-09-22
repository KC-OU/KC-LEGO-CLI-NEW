package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func newMOTDCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "motd [message|clear]",
		Short:   "Show or set the message of the day on the sign-on screen",
		Example: "  wms motd \"Stock check Saturday 10:00\"\n  wms motd clear",
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			if len(args) > 0 {
				msg := strings.TrimSpace(strings.Join(args, " "))
				if msg == "clear" {
					msg = ""
				}
				if err := config.SetOverride(config.MOTD, msg); err != nil {
					return err
				}
				_ = audit.New().Log(cliUser(), "", "MOTD_SET", "SUCCESS", msg)
			}
			cur := config.Get(config.MOTD)
			say(ui.Fact(t, "Message of the day", orDash(cur)))
			return emit(map[string]string{"motd": cur})
		},
	}
	return cmd
}
