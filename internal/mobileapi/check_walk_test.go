package mobileapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func jsonBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(b)
}

// checkWalkEnv wires a scratch lego.db with a real catalog (so NewCheck has
// something to build a check from) and a check ticket already claimed by
// "dave" — no Part-DB in this environment, so every location resolves to ""
// (LocationNames' own documented behavior when pdb is nil), which is itself
// worth covering: the walk must still work with no known locations.
func checkWalkEnv(t *testing.T) (*Server, *lego.DB) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.AuditLogFile, filepath.Join(dir, "audit.log"))
	db, err := lego.Open(filepath.Join(dir, "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	for _, q := range []string{
		`INSERT INTO cat_categories (id, name) VALUES (11, 'Bricks')`,
		`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('3001', 'Brick 2 x 4', 11)`,
		`INSERT INTO cat_colors (id, name, rgb, is_trans) VALUES (4, 'Red', 'C91A09', 0), (1, 'Blue', '0055BF', 0)`,
		`INSERT INTO cat_elements (part_num, color_id) VALUES ('3001', 4), ('3001', 1)`,
		`INSERT INTO cat_themes (id, name, parent_id) VALUES (158, 'Star Wars', 0)`,
		`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES ('75192-1', 'Millennium Falcon', 2017, 158, 16, '')`,
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10),(1,'3001',1,6)`,
		`INSERT INTO cat_meta (key, value) VALUES ('refreshed_ms', '1789900000000')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}

	if _, err := db.AssignTicket(lego.TicketCheck, "75192-1", "75192-1 Falcon", "", "", "", "setup"); err != nil {
		t.Fatal(err)
	}
	tickets, err := db.AllOpenTickets()
	if err != nil || len(tickets) != 1 {
		t.Fatalf("AllOpenTickets = %+v, %v", tickets, err)
	}
	if err := db.ClaimTicket(tickets[0].ID, "dave"); err != nil {
		t.Fatal(err)
	}

	s := &Server{legoDB: db, rebrick: &lego.Client{}, audit: audit.New()}
	return s, db
}

func newCheckWalkTestServer(s *Server) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mobile/next", s.withSession(s.handleNext))
	mux.HandleFunc("GET /mobile/next/all", s.withSession(s.handleFull))
	mux.HandleFunc("POST /mobile/confirm", s.withSession(s.handleConfirm))
	mux.HandleFunc("POST /mobile/finish", s.withSession(s.handleFinish))
	mux.HandleFunc("GET /mobile/bag-find", s.withSession(s.handleBagFind))
	mux.HandleFunc("GET /mobile/messages", s.withSession(s.handleMessages))
	mux.HandleFunc("POST /mobile/message-admin", s.withSession(s.handleMessageAdmin))
	mux.HandleFunc("POST /mobile/request-set", s.withSession(s.handleRequestSet))
	return httptest.NewServer(mux)
}

// issueToken bypasses login (already covered by mobileapi_test.go) so these
// tests focus purely on the walk/confirm logic.
func issueToken(t *testing.T, db *lego.DB) string {
	t.Helper()
	tok, err := db.StartMobileLogin("dave", "modernwms", "checker", false)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func getLine(t *testing.T, srv *httptest.Server, token string, pos int) (int, lineView) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/mobile/next?pos="+strconv.Itoa(pos), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v lineView
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

// TestNextShowsElapsedAndEstimatedTime is the direct test for the time-
// estimate feature's picker/checker-facing half: elapsed tracks the current
// ticket's own claim (checkWalkEnv already claimed one for dave, so it's
// present but small), and estimated stays 0 until there's finished-ticket
// history to average, then reflects it.
func TestNextShowsElapsedAndEstimatedTime(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	_, v := getLine(t, srv, token, 0)
	if v.ElapsedSeconds < 0 {
		t.Errorf("elapsed_seconds = %d, want >= 0 for a just-claimed ticket", v.ElapsedSeconds)
	}
	if v.EstimatedSeconds != 0 {
		t.Errorf("estimated_seconds = %d, want 0 with no finished-ticket history yet", v.EstimatedSeconds)
	}

	// A finished check from 10 minutes ago gives EstimatePace something to average.
	tk, err := db.AssignTicket(lego.TicketCheck, "10696-1", "Classic Box", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(tk.ID, "sam"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := db.Exec(`UPDATE job_tickets SET status = ?, claimed_at = ?, done_at = ? WHERE id = ?`,
		lego.TicketDone, now.Add(-10*time.Minute).Format(time.RFC3339), now.Format(time.RFC3339), tk.ID); err != nil {
		t.Fatal(err)
	}

	_, v = getLine(t, srv, token, 0)
	if v.EstimatedSeconds != 600 {
		t.Errorf("estimated_seconds = %d, want 600 (10 minutes) once there's history", v.EstimatedSeconds)
	}
}

func TestNextReturnsTheFirstLineInShelfOrder(t *testing.T) {
	s, _ := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, s.legoDB)

	status, v := getLine(t, srv, token, 0)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if v.Target != "75192-1" || v.SetName != "Millennium Falcon" || v.Total != 2 || v.Position != 1 {
		t.Fatalf("line = %+v", v)
	}
	if v.PartNum != "3001" {
		t.Errorf("part = %q, want 3001", v.PartNum)
	}
	// No Part-DB in this test env — confirms the "no known location" path
	// resolves to blank cleanly rather than erroring.
	if v.Location != "" || v.NextLocation != "" {
		t.Errorf("location = %q, next_location = %q, want both blank with no Part-DB", v.Location, v.NextLocation)
	}
}

func TestNextIncludesColorAndPartImageForPictures(t *testing.T) {
	s, _ := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, s.legoDB)

	status, v := getLine(t, srv, token, 0)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if v.ColorRGB != "C91A09" {
		t.Errorf("color_rgb = %q, want C91A09 (red, from cat_colors)", v.ColorRGB)
	}
	if !strings.Contains(v.PartImageURL, "3001") {
		t.Errorf("part_image_url = %q, want it to mention the part number", v.PartImageURL)
	}
	// The fixture's cat_sets row has an empty img_url — confirms this passes
	// through cleanly rather than inventing one.
	if v.SetImageURL != "" {
		t.Errorf("set_image_url = %q, want blank (fixture has no img_url)", v.SetImageURL)
	}
}

func TestNextClampsAnOutOfRangePosition(t *testing.T) {
	s, _ := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, s.legoDB)

	status, v := getLine(t, srv, token, 999)
	if status != http.StatusOK || v.Position != v.Total {
		t.Fatalf("status = %d, v = %+v, want clamped to the last line", status, v)
	}
}

// TestFullReturnsEveryLineMatchingNextPositionByPosition is the regression
// test for full offline mode's prefetch (GET /mobile/next/all): it must
// return the exact same lines handleNext would give one at a time, in the
// same order, so a client that caches this response and later falls back to
// it while offline sees identical data to what the network call would have.
func TestFullReturnsEveryLineMatchingNextPositionByPosition(t *testing.T) {
	s, _ := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, s.legoDB)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/mobile/next/all", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Lines []lineView `json:"lines"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || len(body.Lines) != 2 {
		t.Fatalf("status=%d lines=%+v, want 2 lines", resp.StatusCode, body.Lines)
	}
	for i, want := range body.Lines {
		_, got := getLine(t, srv, token, i)
		if got.PartNum != want.PartNum || got.ColorName != want.ColorName || got.Position != want.Position {
			t.Errorf("line %d: /next/all gave %+v, /next gave %+v, want matching", i, want, got)
		}
	}
}

func TestNextWithNoAssignedTicketSaysDone(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.AuditLogFile, filepath.Join(dir, "audit.log"))
	db, err := lego.Open(filepath.Join(dir, "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{legoDB: db, rebrick: &lego.Client{}, audit: audit.New()}
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	status, v := getLine(t, srv, token, 0)
	if status != http.StatusOK || !v.Done {
		t.Fatalf("status = %d, v = %+v, want done=true with nothing assigned", status, v)
	}
}

func TestConfirmHaveAllMarksTheLineAndPersists(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/confirm", jsonBody(t, confirmRequest{Position: 0, Action: "have_all"}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v lineView
	_ = json.NewDecoder(resp.Body).Decode(&v)
	if resp.StatusCode != http.StatusOK || v.Have != v.Need || v.Missing != 0 {
		t.Fatalf("status = %d, v = %+v, want have == need", resp.StatusCode, v)
	}

	// Persisted — a fresh /next call sees the same state, not just the response.
	_, again := getLine(t, srv, token, 0)
	if again.Have != again.Need {
		t.Errorf("after confirming, a fresh /next shows have=%d need=%d — not persisted", again.Have, again.Need)
	}
}

func TestConfirmMissingSetsHaveBelowNeed(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/confirm", jsonBody(t, confirmRequest{Position: 0, Action: "missing", Count: 3}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v lineView
	_ = json.NewDecoder(resp.Body).Decode(&v)
	if resp.StatusCode != http.StatusOK || v.Missing != 3 {
		t.Fatalf("status = %d, v = %+v, want missing=3", resp.StatusCode, v)
	}
}

// TestConfirmReportsTheWholeChecksRunningMissingAndExtraTotals is the mobile
// half of the TUI's always-visible "MISSING N / EXTRA N" badge: lineView
// carries the whole check's running totals, not just the current line's own,
// so the app can show the same at-a-glance summary without adding it up
// itself across every line.
func TestConfirmReportsTheWholeChecksRunningMissingAndExtraTotals(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	_, v := postConfirm(t, srv, token, confirmRequest{Position: 0, Action: "missing", Count: 3})
	if v.Missing != 3 || v.TotalMissing != 3 || v.TotalExtra != 0 {
		t.Fatalf("after marking line 0 missing: %+v", v)
	}

	_, v = postConfirm(t, srv, token, confirmRequest{Position: 1, Action: "extra", Count: 2})
	if v.Extra != 2 || v.TotalMissing != 3 || v.TotalExtra != 2 {
		t.Fatalf("after also marking line 1 extra, totals should cover the whole check, not just this line: %+v", v)
	}
}

// TestConfirmDamagedFlagsConditionWithoutChangingHaveOrMissing is roadmap
// item 6: a condition flag, not a quantity -- have_all already marked this
// line fully present, and marking 2 of them damaged must leave that alone.
func TestConfirmDamagedFlagsConditionWithoutChangingHaveOrMissing(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/confirm", jsonBody(t, confirmRequest{Position: 0, Action: "have_all"}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("have_all: status = %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/mobile/confirm", jsonBody(t, confirmRequest{Position: 0, Action: "damaged", Count: 2}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v lineView
	_ = json.NewDecoder(resp.Body).Decode(&v)
	if resp.StatusCode != http.StatusOK || v.Damaged != 2 || v.Have != v.Need || v.Missing != 0 {
		t.Fatalf("status = %d, v = %+v, want damaged=2, have unchanged at need, missing=0", resp.StatusCode, v)
	}

	// Persisted — a fresh /next call sees the same state, not just the response.
	_, again := getLine(t, srv, token, 0)
	if again.Damaged != 2 {
		t.Errorf("after confirming, a fresh /next shows damaged=%d — not persisted", again.Damaged)
	}
}

func TestConfirmRejectsAnUnknownAction(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/confirm", jsonBody(t, confirmRequest{Position: 0, Action: "nonsense"}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
