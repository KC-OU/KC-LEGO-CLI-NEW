package main

import (
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestAttendanceClockInOut(t *testing.T) {
	legoEnv(t)

	stdout, code := run(t, "attendance", "clock-in", "dave")
	if code != 0 || !strings.Contains(stdout, "dave clocked in") {
		t.Fatalf("clock-in: code=%d out=%q", code, stdout)
	}
	if _, code := run(t, "attendance", "clock-in", "dave"); code != exitUsage {
		t.Errorf("clocking in twice = %d, want %d", code, exitUsage)
	}

	stdout, code = run(t, "attendance", "status", "dave")
	if code != 0 || !strings.Contains(stdout, "Clocked in") || !strings.Contains(stdout, "since") {
		t.Fatalf("status while clocked in: code=%d out=%q", code, stdout)
	}

	stdout, code = run(t, "attendance", "clock-out", "dave")
	if code != 0 || !strings.Contains(stdout, "dave clocked out") {
		t.Fatalf("clock-out: code=%d out=%q", code, stdout)
	}
	if _, code := run(t, "attendance", "clock-out", "dave"); code != exitUsage {
		t.Errorf("clocking out twice = %d, want %d", code, exitUsage)
	}
}

func TestAttendanceRotaSetClearAndShow(t *testing.T) {
	legoEnv(t)

	stdout, code := run(t, "attendance", "rota", "set", "dave", "2026-10-10", "09:00", "17:00")
	if code != 0 || !strings.Contains(stdout, "dave scheduled on 2026-10-10") {
		t.Fatalf("rota set: code=%d out=%q", code, stdout)
	}

	stdout, code = run(t, "attendance", "rota", "show", "2026-10-10")
	if code != 0 || !strings.Contains(stdout, "dave") || !strings.Contains(stdout, "09:00 - 17:00") {
		t.Fatalf("rota show: code=%d out=%q", code, stdout)
	}

	stdout, code = run(t, "attendance", "rota", "show", "2026-10-11")
	if code != 0 || !strings.Contains(stdout, "Nobody scheduled") {
		t.Fatalf("rota show on an empty day: code=%d out=%q", code, stdout)
	}

	stdout, code = run(t, "attendance", "rota", "clear", "dave", "2026-10-10")
	if code != 0 || !strings.Contains(stdout, "cleared") {
		t.Fatalf("rota clear: code=%d out=%q", code, stdout)
	}
	stdout, _ = run(t, "attendance", "rota", "show", "2026-10-10")
	if !strings.Contains(stdout, "Nobody scheduled") {
		t.Errorf("expected the cleared day to show nobody scheduled, got %q", stdout)
	}
}

func TestAttendanceOverrideDoesNotClobberAPlannedShift(t *testing.T) {
	legoEnv(t)

	if _, code := run(t, "attendance", "rota", "override", "dave", "2026-10-12"); code != 0 {
		t.Fatalf("override: code=%d", code)
	}
	stdout, _ := run(t, "attendance", "rota", "show", "2026-10-12")
	if !strings.Contains(stdout, "emergency cover") {
		t.Errorf("expected the override to show as emergency cover, got %q", stdout)
	}

	if _, code := run(t, "attendance", "rota", "set", "sam", "2026-10-13", "08:00", "16:00"); code != 0 {
		t.Fatalf("rota set: code=%d", code)
	}
	if _, code := run(t, "attendance", "rota", "override", "sam", "2026-10-13"); code != 0 {
		t.Fatalf("override on an already-scheduled day: code=%d", code)
	}
	stdout, _ = run(t, "attendance", "rota", "show", "2026-10-13")
	if strings.Contains(stdout, "emergency cover") || !strings.Contains(stdout, "08:00 - 16:00") {
		t.Errorf("a quick override on a day already scheduled must leave the planned shift as-is, got %q", stdout)
	}
}

func TestAttendanceStatusShowsNSWhenNotScheduled(t *testing.T) {
	legoEnv(t)
	stdout, code := run(t, "attendance", "status", "dave")
	if code != 0 || !strings.Contains(stdout, "NS") {
		t.Fatalf("status with no rota entry for today: code=%d out=%q", code, stdout)
	}
}

func TestAttendanceBreakStartEnd(t *testing.T) {
	legoEnv(t)

	if _, code := run(t, "attendance", "break-start", "dave"); code != exitUsage {
		t.Errorf("starting a break with no shift = %d, want %d", code, exitUsage)
	}
	if _, code := run(t, "attendance", "clock-in", "dave"); code != 0 {
		t.Fatal("clock-in failed")
	}

	stdout, code := run(t, "attendance", "break-start", "dave")
	if code != 0 || !strings.Contains(stdout, "now on a break") {
		t.Fatalf("break-start: code=%d out=%q", code, stdout)
	}
	if _, code := run(t, "attendance", "break-start", "dave"); code != exitUsage {
		t.Errorf("starting a second break = %d, want %d", code, exitUsage)
	}

	stdout, _ = run(t, "attendance", "status", "dave")
	if !strings.Contains(stdout, "On a break") {
		t.Errorf("status should show the open break, got %q", stdout)
	}

	stdout, code = run(t, "attendance", "break-end", "dave")
	if code != 0 || !strings.Contains(stdout, "break has ended") {
		t.Fatalf("break-end: code=%d out=%q", code, stdout)
	}
	if _, code := run(t, "attendance", "break-end", "dave"); code != exitUsage {
		t.Errorf("ending an already-ended break = %d, want %d", code, exitUsage)
	}
}

// TestClockOutWarnsWhenStillHoldingATicket covers the "clocked-out-but-
// still-holding-a-ticket" alert: it must not block the clock-out itself,
// just warn about it.
func TestClockOutWarnsWhenStillHoldingATicket(t *testing.T) {
	db := legoEnv(t)
	tk, err := db.AssignTicket(lego.TicketCheck, "75192-1", "Millennium Falcon", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	if _, code := run(t, "attendance", "clock-in", "dave"); code != 0 {
		t.Fatal("clock-in failed")
	}

	stdout, code := run(t, "attendance", "clock-out", "dave")
	if code != 0 {
		t.Fatalf("clock-out must still succeed: code=%d out=%q", code, stdout)
	}
	if !strings.Contains(stdout, "still holding an open") {
		t.Errorf("expected a warning about the still-claimed ticket, got %q", stdout)
	}

	// Nothing to warn about once the ticket's actually released.
	if err := db.FinishTicket(lego.TicketCheck, "75192-1", "dave"); err != nil {
		t.Fatal(err)
	}
	if _, code := run(t, "attendance", "clock-in", "dave"); code != 0 {
		t.Fatal("clock-in failed")
	}
	stdout, code = run(t, "attendance", "clock-out", "dave")
	if code != 0 || strings.Contains(stdout, "still holding") {
		t.Errorf("no open ticket left: expected no warning, got code=%d out=%q", code, stdout)
	}
}

// TestAttendanceClockInRefusesWithoutBeingOnTheRotaOnceTheGateIsOn is the
// CLI's half of "refuse clock-in until an admin pre-approves": a clear,
// actionable error (not a bare "not scheduled to work today"), and the
// suggested override command actually works.
func TestAttendanceClockInRefusesWithoutBeingOnTheRotaOnceTheGateIsOn(t *testing.T) {
	legoEnv(t)
	t.Setenv(config.RequireClockIn, "1")

	stdout, code := run(t, "attendance", "clock-in", "dave")
	if code != exitUsage || !strings.Contains(stdout, "isn't on today's rota") || !strings.Contains(stdout, "rota override dave") {
		t.Fatalf("clock-in while unscheduled: code=%d out=%q", code, stdout)
	}

	if _, code := run(t, "attendance", "rota", "override", "dave"); code != 0 {
		t.Fatalf("rota override: code=%d", code)
	}
	stdout, code = run(t, "attendance", "clock-in", "dave")
	if code != 0 || !strings.Contains(stdout, "dave clocked in") {
		t.Fatalf("clock-in after the override: code=%d out=%q", code, stdout)
	}
}
