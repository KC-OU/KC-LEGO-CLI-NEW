package uiapp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
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
	return &tableScreen{
		panelID: "LIVSES",
		title:   "Live Sessions",
		columns: []string{"User", "Role", "Via", "From", "Screen", "Since", "Last seen"},
		fetch: func(app *App) ([][]string, string, error) {
			ss, err := app.legoDB.LiveSessions(sessionMaxAge)
			rows := make([][]string, len(ss))
			for i, s := range ss {
				rows[i] = []string{orDash(s.Username), orDash(s.Role), orDash(s.Transport), orDash(s.RemoteAddr), s.Screen,
					s.StartedAt.Format("15:04"), s.LastSeen.Format("15:04:05")}
			}
			return rows, fmt.Sprintf("%d session(s) seen in the last %s", len(rows), sessionMaxAge), err
		},
	}
}
