package dashboardapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A fake `docker` on PATH stands in for the CrowdSec container.
func fakeDocker(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCrowdsecViewAndActions(t *testing.T) {
	fakeDocker(t, `case "$*" in
*"decisions list"*) echo '[{"id":1,"value":"1.2.3.4","type":"ban","scenario":"ssh-bf","origin":"crowdsec","duration":"3h"}]';;
*"alerts list"*) echo '[{"id":9,"scenario":"ssh-bf","events_count":11,"created_at":"x","source":{"ip":"1.2.3.4","cn":"GB"}}]';;
*"bouncers list"*) echo '[{"name":"host-firewall","type":"fw","last_pull":"now"}]';;
*) echo "ok $*";; esac`)
	s := &Server{}

	rec := httptest.NewRecorder()
	s.handleCrowdsec(rec, httptest.NewRequest("GET", "/dashboard/security/crowdsec", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "1.2.3.4") || !strings.Contains(rec.Body.String(), "host-firewall") {
		t.Fatalf("view = %d %s", rec.Code, rec.Body)
	}

	post := func(action, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleCrowdsecAction(action)(rec, httptest.NewRequest("POST", "/x", strings.NewReader(body)))
		return rec
	}
	if r := post("unban", `{"ip":"1.2.3.4","by":"t"}`); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "decisions delete --ip 1.2.3.4") {
		t.Fatalf("unban = %d %s", r.Code, r.Body)
	}
	if r := post("unban", `{"ip":"1.2.3.4; rm -rf /","by":"t"}`); r.Code != http.StatusBadRequest {
		t.Fatalf("a non-IP must be refused, got %d", r.Code)
	}
	if r := post("ban", `{"ip":"5.6.7.8","duration":"bogus","by":"t"}`); !strings.Contains(r.Body.String(), "--duration 4h") {
		t.Fatalf("bad duration should fall back to 4h: %s", r.Body)
	}
}
