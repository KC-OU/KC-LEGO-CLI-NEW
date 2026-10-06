package main

import (
	"strings"
	"testing"
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
