package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/pflag"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// discordFlags is the --discord/--discord-expires pair shared by every export-producing
// command (report, stocksheet, export) — see internal/uiapp/discord.go for the TUI's
// equivalent (its own expiry prompt after the fact, rather than a flag up front).
type discordFlags struct {
	send    bool
	expires time.Duration
}

func (f *discordFlags) register(fs *pflag.FlagSet) {
	fs.BoolVar(&f.send, "discord", false, "also DM this file's download link (with a QR code) to your configured Discord bot recipient")
	fs.DurationVar(&f.expires, "discord-expires", 2*time.Hour, "how long the Discord link stays valid")
}

// sendToDiscord DMs abs (an already-saved export file) as a QR code + link, expiring
// after f.expires. what is the short description shown in the message ("missing parts
// for 75192"). A no-op, not an error, when --discord wasn't passed.
func sendToDiscord(t ui.Theme, f discordFlags, abs, what string) error {
	if !f.send {
		return nil
	}
	bot := notify.DiscordBotFromConfig()
	if !bot.Enabled() {
		return usageError("--discord needs WMS_DISCORD_BOT_TOKEN and WMS_DISCORD_BOT_USER_ID set (Admin → Settings)")
	}
	if exports.URL("x") == "" {
		return usageError("--discord needs WMS_PUBLIC_URL set, so the link it sends is reachable")
	}
	token, err := exports.NewLinkWithTTL(exports.Dir(), abs, cliActor(), f.expires)
	if err != nil {
		return err
	}
	link := exports.URL(token)
	png, _ := exports.QRPNG(link, 512)
	text := fmt.Sprintf("%s — %s, expires %s\n%s", cliActor(), what, time.Now().Add(f.expires).Format("15:04"), link)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := withSpinner("sending to Discord", func() error { return bot.DM(ctx, text, png, "qr.png") }); err != nil {
		return err
	}
	say(ui.Status(t, true, "Sent to Discord (expires "+time.Now().Add(f.expires).Format("15:04")+")"))
	return nil
}
