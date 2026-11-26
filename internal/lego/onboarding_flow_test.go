package lego

import (
	"strings"
	"testing"
	"time"
)

func finishedCheck(t *testing.T, db *DB, set string, have, extra int) *SetCheck {
	t.Helper()
	c := &SetCheck{SetNum: set, Kind: CheckIntake, Status: StatusDraft, CheckedBy: "4821", StartedAt: time.Now(),
		Lines: []CheckLine{{PartNum: "3001", PartName: "Brick", ColorID: 4, ColorName: "Red", Need: 4, Have: have, Extra: extra}}}
	if _, err := db.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestOnboardingFlowLifecycleAndGate(t *testing.T) {
	db := newTestDB(t)
	if db.OnboardingCleared("21027-1", "4821") {
		t.Fatal("a new checker must not be cleared before the paper check is verified")
	}
	if err := db.OnboardingGate("21027-1", CheckRecount, "4821"); err != nil {
		t.Errorf("a recount is never gated: %v", err)
	}
	if err := db.OnboardingGate("21027-1", CheckIntake, "4821"); err == nil || !strings.Contains(err.Error(), "onboarding first") {
		t.Fatalf("gate = %v, want the onboarding message", err)
	}
	f, err := db.StartFlow("21027-1", "4821")
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := db.StartFlow("21027-1", "4821"); again.ID != f.ID {
		t.Error("starting again should return the same open onboarding")
	}
	if err := db.SubmitPaper(f.ID, "9999", "upload", "x.pdf", ""); err == nil {
		t.Error("someone else must not upload for this onboarding")
	}
	if err := db.SubmitPaper(f.ID, "4821", "upload", "x.pdf", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.VerifyFlow(f.ID, "admin", false, "  ", ""); err == nil {
		t.Error("sending it back needs a note")
	}
	got, err := db.VerifyFlow(f.ID, "admin", false, "IDs are missing", "IDs missing")
	if err != nil || got.Status != FlowChanges || got.VerifyNote != "IDs are missing" {
		t.Fatalf("after rejecting: %+v, %v", got, err)
	}
	if err := db.SubmitPaper(f.ID, "4821", "upload", "y.pdf", ""); err != nil {
		t.Fatalf("re-upload after changes: %v", err)
	}
	if got, err = db.VerifyFlow(f.ID, "admin", true, "", ""); err != nil || got.Status != FlowVerified {
		t.Fatalf("after verifying: %+v, %v", got, err)
	}
	if !db.OnboardingCleared("21027-1", "4821") || db.OnboardingCleared("21027-1", "5555") {
		t.Error("only the verified checker is cleared for that set")
	}
	if _, err := db.CompleteFlow(f.ID, "4821", "{}"); err == nil {
		t.Error("submitting before a check is finished must fail")
	}
	time.Sleep(1100 * time.Millisecond) // the check has to finish after the verification (second resolution)
	finishedCheck(t, db, "21027-1", 4, 0)
	if got, err = db.CompleteFlow(f.ID, "4821", `{"ok":1}`); err != nil || got.Status != FlowComplete {
		t.Fatalf("complete: %+v, %v", got, err)
	}
	if next, _ := db.StartFlow("21027-1", "4821"); next.ID == f.ID {
		t.Error("a later check of the same set starts a new onboarding")
	}
	if err := db.WaiveOnboarding("*", "admin", true); err != nil {
		t.Fatal(err)
	}
	if !db.OnboardingCleared("99999-1", "7777") {
		t.Error("the * waiver clears every set")
	}
}

func TestSheetDiffFindsWhatThePaperAndTheCheckDisagreeOn(t *testing.T) {
	db := newTestDB(t)
	finishedCheck(t, db, "21027-1", 2, 1) // short 2, 1 extra
	same := `{"set":"21027-1","lines":[{"p":"3001","c":4,"s":2,"m":"2","x":"1"}],"borrowed":[],"rec":0}`
	if d := db.SheetDiff(&OnboardingFlow{SetNum: "21027-1", SheetJSON: same}); len(d) != 0 {
		t.Errorf("matching sheet should have no differences, got %v", d)
	}
	off := `{"set":"21027-1","lines":[{"p":"3001","c":4,"s":3,"m":"1","x":"0"}],"borrowed":[["3001","Red","2","10696-1","",""]],"rec":0}`
	d := db.SheetDiff(&OnboardingFlow{SetNum: "21027-1", SheetJSON: off})
	joined := strings.Join(d, "\n")
	for _, want := range []string{"paper says 1 missing, the check says 2", "paper says 0 extra, the check says 1", "still marked ?", "the paper lists 2, the system has 0"} {
		if !strings.Contains(joined, want) {
			t.Errorf("diff is missing %q:\n%s", want, joined)
		}
	}
	if d := db.SheetDiff(&OnboardingFlow{SetNum: "21027-1"}); d != nil {
		t.Error("a photo or scan (no saved sheet) has nothing to diff")
	}
}

func TestParseSheetStateReadsASavedSheet(t *testing.T) {
	html := `<html><body><script type="application/json" id="kc-state">{"set":"1-1","lines":[]}</script><script>x</script></body></html>`
	if got := ParseSheetState([]byte(html)); !strings.Contains(got, `"set":"1-1"`) {
		t.Errorf("state = %q", got)
	}
	if ParseSheetState([]byte("<html>not a sheet</html>")) != "" || ParseSheetState([]byte(`<script type="application/json" id="kc-state">{bad</script>`)) != "" {
		t.Error("a file without valid saved state must give nothing")
	}
}

func TestInboxListReadAndReply(t *testing.T) {
	db := newTestDB(t)
	id, err := db.SendMessageRef("admin", "4821", "Please redo the paper check", 0)
	if err != nil {
		t.Fatal(err)
	}
	if db.UnreadCount("4821") != 1 || db.UnreadCount("admin") != 0 {
		t.Fatal("one unread for the checker, none for the admin")
	}
	if _, err := db.ReplyMessage("9999", id, "hi"); err == nil {
		t.Error("only the recipient may reply")
	}
	if _, err := db.ReplyMessage("4821", id, "Done, uploaded again"); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.ListMessages("admin", 10); len(got) != 1 || got[0].ParentID != id || got[0].From != "4821" {
		t.Errorf("admin inbox = %+v, want the reply threaded under #%d", got, id)
	}
	if err := db.MarkRead("9999", id); err != nil || db.UnreadCount("4821") != 1 {
		t.Error("marking someone else's message read must change nothing")
	}
	if err := db.MarkRead("4821", id); err != nil || db.UnreadCount("4821") != 0 {
		t.Error("the recipient can mark it read")
	}
	if _, err := db.SendMessageRef("admin", "4821", "  ", 0); err == nil {
		t.Error("an empty message is refused")
	}
}
