package uiapp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// A live registry of connected sessions (see internal/lego/sessions.go), purely
// diagnostic: who's on, from where, how long, doing what. It's also the tool that
// actually answers whether two sessions freezing each other (rather than one
// slow session) is really what's happening, instead of guessing at it.
const (
	scrLiveSessions = "live_sessions"
	sessionMaxAge   = 20 * time.Minute // older than one idle-lock window: surely gone, not just quiet
)

func (a *App) startHeartbeat() {
	if a.sessionID != "" {
		return // already running (e.g. re-entered enterHub without a fresh process)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	a.sessionID = hex.EncodeToString(b)
	a.heartbeat()
}

func (a *App) heartbeat() {
	if a.sessionID == "" || a.session == nil {
		return
	}
	_ = a.legoDB.Heartbeat(a.sessionID, a.session.Username, a.session.Role, a.transport, a.remoteAddr, a.cur)
}

func liveSessionsScreen() screenModel {
	return &selectList{
		panelID:   "LIVSES",
		title:     "Live Sessions",
		hint:      "↑/↓ choose  K log off this session now",
		emptyHint: "No sessions seen in the last " + sessionMaxAge.String() + ".",
		rows:      liveSessionRows,
		keys:      liveSessionKeys,
	}
}

func liveSessionRows(app *App) ([]string, [][]string, []string) {
	ss, err := app.legoDB.LiveSessions(sessionMaxAge)
	if err != nil {
		app.setMsg(err.Error(), true)
	}
	rows := make([][]string, len(ss))
	keys := make([]string, len(ss))
	for i, s := range ss {
		rows[i] = []string{orDash(s.Username), orDash(s.Role), orDash(s.Transport), orDash(s.RemoteAddr), s.Screen,
			s.StartedAt.Format("15:04"), s.LastSeen.Format("15:04:05")}
		keys[i] = s.SessionID
	}
	return []string{"User", "Role", "Via", "From", "Screen", "Since", "Last seen"}, rows, keys
}

// ---- Admin: kick a live session off ----

type kickDraft struct {
	sessionID, username string
}

const forceLogoffMessage = "You've been logged off by an admin — don't worry, your accuracy won't be affected. If this was unexpected, message an admin."

func liveSessionKeys(app *App, sessionID string, msg tea.KeyMsg) {
	if sessionID == "" || !isKey(msg, 'k') {
		return
	}
	if sessionID == app.sessionID {
		app.setMsg("You can't log off your own session.", true)
		return
	}
	ss, _ := app.legoDB.LiveSessions(sessionMaxAge)
	for _, s := range ss {
		if s.SessionID == sessionID {
			app.kick = &kickDraft{sessionID: s.SessionID, username: s.Username}
			startPick(app, &pickState{
				Header:    "Log off " + s.Username + " — message to show them:",
				Prompt:    "Choose",
				Items:     []pickItem{{Key: "standard", Label: forceLogoffMessage}},
				AllowFree: true,
				FreeHint:  "or write your own",
				OnPick:    func(app *App, it pickItem) { finishKick(app, it.Label) },
				OnFree:    func(app *App, text string) { finishKick(app, text) },
			})
			return
		}
	}
}

func finishKick(app *App, message string) {
	k := app.kick
	if k == nil {
		app.onBack()
		return
	}
	app.kick = nil
	if err := app.legoDB.ForceLogoff(k.sessionID, message); err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	_ = app.legoDB.LogEvent(lego.EventForcedOff, app.userName(), k.username, "live session kicked: "+message)
	app.notifyEvent(lego.EventForcedOff, fmt.Sprintf("%s's session kicked by %s: %s", k.username, app.userName(), message), "")
	app.audit.Log(app.userName(), "", "SESSION_KICKED", "SUCCESS", k.username)
	app.setMsg("Logging "+k.username+" off now.", false)
	app.onBack()
}
