package uiapp

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
)

// Sends a just-made export to your Discord DMs (a real bot message, with the
// QR code as a real image attachment — see internal/notify/discordbot.go),
// with its own expiry chosen at send time rather than the site-wide LinkTTL:
// exactly for "might be busy, don't make me redo the whole export later".

var discordExpiryChoices = []struct {
	Key, Label string
	TTL        time.Duration
}{
	{"1", "30 minutes", 30 * time.Minute},
	{"2", "2 hours", 2 * time.Hour},
	{"3", "8 hours", 8 * time.Hour},
	{"4", "24 hours", 24 * time.Hour},
}

func discordAskBody(app *App) string {
	t := app.theme
	s := t.Strong.Render("Send to Discord — pick an expiry:") + "\n"
	for _, c := range discordExpiryChoices {
		s += fmt.Sprintf("  %s  %s\n", t.Accent.Render(c.Key), t.Text.Render(c.Label))
	}
	return s + t.Muted.Render("Esc to cancel")
}

func handleDiscordAskKey(app *App, msg tea.KeyMsg) {
	if msg.Type == tea.KeyEsc {
		app.discordAsking = false
		return
	}
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return
	}
	for _, c := range discordExpiryChoices {
		if string(msg.Runes) == c.Key {
			app.discordAsking = false
			sendExportToDiscord(app, c.TTL)
			return
		}
	}
}

// discordSentMsg reports a background Discord DM send (see startBusy in
// app.go); handled in App.Update.
type discordSentMsg struct{ err error }

func (a *App) discordSent(m discordSentMsg) {
	a.stopBusy()
	if m.err != nil {
		a.setMsg("Discord: "+m.err.Error(), true)
		return
	}
	a.setMsg("Sent to Discord.", false)
}

func sendExportToDiscord(app *App, ttl time.Duration) {
	r := app.exportRes
	if r == nil {
		return
	}
	bot := notify.DiscordBotFromConfig()
	dir := exports.Dir()
	token, err := exports.NewLinkWithTTL(dir, r.Path, app.userKey(), ttl)
	if err != nil {
		app.setMsg("Discord: "+err.Error(), true)
		return
	}
	link := exports.URL(token)
	png, _ := exports.QRPNG(link, 512)
	what := ""
	if app.exportJob != nil {
		what = app.exportJob.What
	}
	text := fmt.Sprintf("%s — %s, expires %s\n%s", app.userName(), what, time.Now().Add(ttl).Format("15:04"), link)
	app.startBusy("Sending to Discord…", func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return discordSentMsg{err: bot.DM(ctx, text, png, "qr.png")}
	})
}
