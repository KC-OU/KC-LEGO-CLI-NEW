package mobileapi

import (
	"net/http"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

type historyRow struct {
	Kind            string `json:"kind"`
	Target          string `json:"target"`
	Label           string `json:"label"`
	DoneAt          string `json:"done_at"`
	DurationSeconds int64  `json:"duration_seconds,omitempty"`
}

// handleHistory is the check/pick history screen's data: the signed-in
// user's own finished tickets, most recent first (lego.DB.TicketHistory).
// Read-only, and deliberately just the ticket itself — see TicketHistory's
// own comment for why outcome detail (missing/extra counts) isn't here.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	tickets, err := s.legoDB.TicketHistory(sess.Username, 50)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]historyRow, 0, len(tickets))
	for _, t := range tickets {
		row := historyRow{Kind: t.Kind, Target: t.Target, Label: t.Label, DoneAt: t.DoneAt.Format(time.RFC3339)}
		if !t.ClaimedAt.IsZero() && !t.DoneAt.IsZero() {
			row.DurationSeconds = int64(t.DoneAt.Sub(t.ClaimedAt).Seconds())
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}
