package mobileapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
)

type attendanceStatus struct {
	ClockedIn      bool   `json:"clocked_in"`
	ClockedInSince string `json:"clocked_in_since,omitempty"`
	OnBreak        bool   `json:"on_break"`
	ScheduledToday bool   `json:"scheduled_today"`
	StartTime      string `json:"start_time,omitempty"`
	EndTime        string `json:"end_time,omitempty"`
	EmergencyCover bool   `json:"emergency_cover"`
}

// handleAttendanceStatus is the clock-in screen's data: am I clocked in, on
// a break, and scheduled today — everything the app needs to show the right
// buttons and the "NS" state without a second round trip.
func (s *Server) handleAttendanceStatus(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	out := attendanceStatus{}
	shift, err := s.legoDB.CurrentShift(sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if shift != nil {
		out.ClockedIn = true
		out.ClockedInSince = shift.ClockInAt.Format(time.RFC3339)
		if on, err := s.legoDB.OnBreak(sess.Username); err == nil {
			out.OnBreak = on
		}
	}
	if rota, err := s.legoDB.GetRota(sess.Username, time.Now().Format("2006-01-02")); err == nil && rota != nil {
		out.ScheduledToday = true
		out.StartTime = rota.StartTime
		out.EndTime = rota.EndTime
		out.EmergencyCover = rota.EmergencyOverride
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleClockIn(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if _, err := s.legoDB.ClockIn(sess.Username); err != nil {
		if errors.Is(err, lego.ErrAlreadyClockedIn) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "already clocked in"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.handleAttendanceStatus(w, r, sess)
}

func (s *Server) handleClockOut(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if err := s.legoDB.ClockOut(sess.Username); err != nil {
		if errors.Is(err, lego.ErrNotClockedIn) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "not clocked in"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if t, err := s.legoDB.CurrentTicket(sess.Username); err == nil && t != nil {
		label := t.Label
		if label == "" {
			label = t.Target
		}
		notify.Dispatch("attendance_clocked_out_with_ticket",
			sess.Username+" clocked out while still holding a ticket",
			sess.Username+" clocked out but is still assigned an open "+t.Kind+" ticket: "+label+". It wasn't released.")
	}
	s.handleAttendanceStatus(w, r, sess)
}

func (s *Server) handleBreakStart(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if err := s.legoDB.StartBreak(sess.Username); err != nil {
		switch {
		case errors.Is(err, lego.ErrNotClockedIn):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "not clocked in"})
		case errors.Is(err, lego.ErrAlreadyOnBreak):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "already on a break"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	s.handleAttendanceStatus(w, r, sess)
}

type rotaDayRow struct {
	Date    string           `json:"date"`
	Entries []lego.RotaEntry `json:"entries"`
}

// handleAdminRota is the weekly rota view's mobile data: the next 7 days
// starting today, same source as the TUI's Admin hub screen
// (internal/uiapp/rota_screen.go) — admin-only, same reasoning as the other
// /mobile/admin/* endpoints (this is "who's scheduled", not "am I").
func (s *Server) handleAdminRota(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if !s.requireAdmin(w, sess, "ADMIN_VIEW_ROTA") {
		return
	}
	days := make([]rotaDayRow, 0, 7)
	for i := 0; i < 7; i++ {
		date := time.Now().AddDate(0, 0, i).Format("2006-01-02")
		entries, err := s.legoDB.RotaForDate(date)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if entries == nil {
			entries = []lego.RotaEntry{}
		}
		days = append(days, rotaDayRow{Date: date, Entries: entries})
	}
	writeJSON(w, http.StatusOK, days)
}

func (s *Server) handleBreakEnd(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if err := s.legoDB.EndBreak(sess.Username); err != nil {
		switch {
		case errors.Is(err, lego.ErrNotClockedIn):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "not clocked in"})
		case errors.Is(err, lego.ErrNotOnBreak):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "not on a break"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	s.handleAttendanceStatus(w, r, sess)
}
