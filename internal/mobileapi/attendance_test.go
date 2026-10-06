package mobileapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func attendanceEnv(t *testing.T) (*Server, *lego.DB) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.AuditLogFile, filepath.Join(dir, "audit.log"))
	db, err := lego.Open(filepath.Join(dir, "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Server{legoDB: db, rebrick: &lego.Client{}, audit: audit.New()}, db
}

func newAttendanceTestServer(s *Server) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mobile/attendance", s.withSession(s.handleAttendanceStatus))
	mux.HandleFunc("POST /mobile/attendance/clock-in", s.withSession(s.handleClockIn))
	mux.HandleFunc("POST /mobile/attendance/clock-out", s.withSession(s.handleClockOut))
	mux.HandleFunc("POST /mobile/attendance/break-start", s.withSession(s.handleBreakStart))
	mux.HandleFunc("POST /mobile/attendance/break-end", s.withSession(s.handleBreakEnd))
	return httptest.NewServer(mux)
}

func TestMobileAttendanceClockInOutAndBreak(t *testing.T) {
	s, db := attendanceEnv(t)
	srv := newAttendanceTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	status := decodeMap(t, authed(t, http.MethodGet, srv.URL+"/mobile/attendance", token, nil))
	if status["clocked_in"] != false {
		t.Fatalf("expected not clocked in yet, got %+v", status)
	}

	resp := authed(t, http.MethodPost, srv.URL+"/mobile/attendance/clock-in", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clock-in: status=%d", resp.StatusCode)
	}
	status = decodeMap(t, resp)
	if status["clocked_in"] != true || status["clocked_in_since"] == "" {
		t.Fatalf("expected clocked in with a timestamp, got %+v", status)
	}

	if resp := authed(t, http.MethodPost, srv.URL+"/mobile/attendance/clock-in", token, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("clocking in twice = %d, want %d", resp.StatusCode, http.StatusConflict)
	}

	resp = authed(t, http.MethodPost, srv.URL+"/mobile/attendance/break-start", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("break-start: status=%d", resp.StatusCode)
	}
	if status = decodeMap(t, resp); status["on_break"] != true {
		t.Fatalf("expected on_break=true, got %+v", status)
	}
	if resp := authed(t, http.MethodPost, srv.URL+"/mobile/attendance/break-start", token, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("starting a second break = %d, want %d", resp.StatusCode, http.StatusConflict)
	}

	resp = authed(t, http.MethodPost, srv.URL+"/mobile/attendance/break-end", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("break-end: status=%d", resp.StatusCode)
	}
	if status = decodeMap(t, resp); status["on_break"] != false {
		t.Fatalf("expected on_break=false after ending it, got %+v", status)
	}

	resp = authed(t, http.MethodPost, srv.URL+"/mobile/attendance/clock-out", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clock-out: status=%d", resp.StatusCode)
	}
	if status = decodeMap(t, resp); status["clocked_in"] != false {
		t.Fatalf("expected clocked_in=false after clocking out, got %+v", status)
	}
	if resp := authed(t, http.MethodPost, srv.URL+"/mobile/attendance/clock-out", token, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("clocking out twice = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
}

func TestMobileAdminRotaRequiresAdminAndListsTheNextSevenDays(t *testing.T) {
	s, db := attendanceEnv(t)
	srv := newAdminTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	if resp := authed(t, http.MethodGet, srv.URL+"/mobile/admin/rota", token, nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin: status=%d, want 403", resp.StatusCode)
	}

	asAdmin(t, "dave")
	today := time.Now().Format("2006-01-02")
	if err := db.SetRota("dave", today, "09:00", "17:00", "", "admin"); err != nil {
		t.Fatal(err)
	}

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/admin/rota", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin: status=%d, want 200", resp.StatusCode)
	}
	var days []rotaDayRow
	if err := json.NewDecoder(resp.Body).Decode(&days); err != nil {
		t.Fatal(err)
	}
	if len(days) != 7 {
		t.Fatalf("expected 7 days, got %d", len(days))
	}
	if days[0].Date != today || len(days[0].Entries) != 1 || days[0].Entries[0].Username != "dave" {
		t.Fatalf("expected today's entry for dave first, got %+v", days[0])
	}
	if len(days[1].Entries) != 0 {
		t.Errorf("expected tomorrow to have no entries, got %+v", days[1])
	}
}

func TestMobileAttendanceStatusShowsTodaysRota(t *testing.T) {
	s, db := attendanceEnv(t)
	srv := newAttendanceTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	status := decodeMap(t, authed(t, http.MethodGet, srv.URL+"/mobile/attendance", token, nil))
	if status["scheduled_today"] != false {
		t.Fatalf("no rota entry: expected scheduled_today=false, got %+v", status)
	}

	if err := db.SetRota("dave", time.Now().Format("2006-01-02"), "09:00", "17:00", "", "admin"); err != nil {
		t.Fatal(err)
	}
	status = decodeMap(t, authed(t, http.MethodGet, srv.URL+"/mobile/attendance", token, nil))
	if status["scheduled_today"] != true || status["start_time"] != "09:00" || status["end_time"] != "17:00" {
		t.Fatalf("expected today's rota entry reflected in status, got %+v", status)
	}
}

// TestMobileClockInRefusesWithoutBeingOnTheRotaOnceTheGateIsOn is the
// mobile half of "refuse clock-in until an admin pre-approves": a proper
// 403 with a clear message (not a 500 the app would show as a generic
// server error), clearing the moment an admin grants a quick NS override.
func TestMobileClockInRefusesWithoutBeingOnTheRotaOnceTheGateIsOn(t *testing.T) {
	s, db := attendanceEnv(t)
	srv := newAttendanceTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)
	t.Setenv("WMS_REQUIRE_CLOCK_IN", "1")

	resp := authed(t, http.MethodPost, srv.URL+"/mobile/attendance/clock-in", token, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("clock-in while unscheduled: status=%d, want 403", resp.StatusCode)
	}
	body := decodeMap(t, resp)
	if body["error"] != "not scheduled to work today" {
		t.Errorf("expected a clear rota-related message, got %+v", body)
	}

	if err := db.QuickNSOverride("dave", time.Now().Format("2006-01-02"), "admin"); err != nil {
		t.Fatal(err)
	}
	resp = authed(t, http.MethodPost, srv.URL+"/mobile/attendance/clock-in", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clock-in after the override: status=%d", resp.StatusCode)
	}
}
