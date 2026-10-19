// Package dashboardapi is the new Rust-based KC-Parts web dashboard's only
// way to reach this project's data: a small, additive JSON API over the
// same business logic the TUI, telnet/web gateway, and mobile app already
// use (internal/lego), mirroring internal/mobileapi's dependency-injection
// shape rather than re-implementing anything.
//
// End-user identity is BetterAuth's job, handled entirely in the Rust
// service — this package never sees an individual dashboard user, only the
// Rust service itself, trusted via one shared bearer token
// (config.DashboardAPIToken). That is a service-to-service trust boundary,
// not a replacement for per-user permissions: endpoints here return the
// same admin-level view the TUI's own admin screens show, and per-user
// scoping (if the dashboard ever needs a non-admin view) belongs in a
// later endpoint, not retrofitted onto this token.
package dashboardapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

type Server struct {
	legoDB *lego.DB
	// token is the shared secret the Rust service presents. Empty disables
	// the check entirely -- only ever intentional in tests that build a
	// Server literal directly, the same "nil/empty disables" convention
	// internal/mobileapi's throttle field already uses.
	token string
}

func NewServer(legoDB *lego.DB, token string) *Server {
	return &Server{legoDB: legoDB, token: token}
}

// Serve runs the dashboard API until ctx is cancelled.
func Serve(ctx context.Context, addr string, s *Server) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dashboard/health", s.handleHealth)
	mux.HandleFunc("GET /dashboard/kanban", s.withToken(s.handleKanban))
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// withToken gates everything except /dashboard/health behind the shared
// secret -- a health check needs to work for ops/monitoring without a
// credential, same as internal/mobileapi leaves /mobile/print's open page
// ungated while its session-bearing siblings are not.
func (s *Server) withToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleKanban is the admin board (internal/lego.KanbanBoardAll): the
// shared backlog, every open ticket across every checker, recently
// abandoned and recently finished work -- exactly what the TUI's own admin
// Kanban screen shows, not a new view of the data.
func (s *Server) handleKanban(w http.ResponseWriter, r *http.Request) {
	cards, err := s.legoDB.KanbanBoardAll()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": cards})
}
