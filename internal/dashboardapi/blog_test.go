package dashboardapi

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// finishSet records a finished intake check of set with one line needing need parts and holding have.
func finishSet(t *testing.T, db *lego.DB, set string, need, have int) {
	t.Helper()
	c := &lego.SetCheck{SetNum: set, Kind: lego.CheckIntake, Status: lego.StatusDraft, CheckedBy: "4821", StartedAt: time.Now(),
		Lines: []lego.CheckLine{{PartNum: "3001", ColorID: 4, ColorName: "Red", Need: need, Have: have}}}
	if _, err := db.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
}

func TestBlogIsDeniedWhileAnyPartIsMissingAndNotBorrowed(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	post := func(path, body string) (int, string) {
		r := postJSON(t, srv.URL, path, body)
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b)
	}
	if c, b := post("/dashboard/blog", `{"slug":"21027-1","set_num":"21027-1","title":"Berlin","body":"hi","by":"boss"}`); c != 200 {
		t.Fatalf("save = %d %s", c, b)
	}
	finishSet(t, db, "21027-1", 4, 2) // two missing, none borrowed
	for _, st := range []string{"review", "published"} {
		if c, b := post("/dashboard/blog/21027-1/state", `{"state":"`+st+`","by":"boss"}`); c != 409 || !strings.Contains(b, "still missing") {
			t.Fatalf("%s with parts missing (even by an admin) = %d %s, want 409", st, c, b)
		}
	}
	if _, err := db.AddBorrowedPart(lego.BorrowedPart{BuildSetNum: "21027-1", NeedPartNum: "3001", ColorID: 4, Qty: 2, FromSetNum: "10696-1"}); err != nil {
		t.Fatal(err)
	}
	if why := db.BlogBlock("21027-1"); why != "" {
		t.Fatalf("with both missing parts borrowed and recorded the block should lift, got %q", why)
	}
}

func TestBlogSaveReviewApprove(t *testing.T) {
	db, srv := testServer(t, "tok")
	do := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer tok")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		buf := make([]byte, 1<<16)
		n, _ := resp.Body.Read(buf)
		return resp.StatusCode, string(buf[:n])
	}
	if c, _ := do("POST", "/dashboard/blog", `{"slug":"Bad Slug","title":"x","by":"kc"}`); c != 400 {
		t.Fatalf("bad slug should be 400, got %d", c)
	}
	if c, b := do("POST", "/dashboard/blog", `{"slug":"21027-1","set_num":"21027-1","title":"Berlin","body":"hi","by":"kc"}`); c != 200 || !strings.Contains(b, `"state":"draft"`) {
		t.Fatalf("save = %d %s", c, b)
	}
	if c, b := do("POST", "/dashboard/blog/21027-1/state", `{"state":"review","by":"kc"}`); c != 409 || !strings.Contains(b, "no finished check") {
		t.Fatalf("a post for a set with no finished check = %d %s, want 409", c, b)
	}
	finishSet(t, db, "21027-1", 4, 4)
	if c, _ := do("POST", "/dashboard/blog/21027-1/state", `{"state":"published","by":"kc"}`); c != 400 {
		t.Fatal("a draft cannot jump straight to published")
	}
	for _, st := range []string{"review", "published"} {
		if c, b := do("POST", "/dashboard/blog/21027-1/state", `{"state":"`+st+`","by":"kc"}`); c != 200 {
			t.Fatalf("%s = %d %s", st, c, b)
		}
	}
	if c, b := do("GET", "/dashboard/blog?state=published", ""); c != 200 || !strings.Contains(b, "Berlin") {
		t.Fatalf("list = %d %s", c, b)
	}
	// Queuing a check for a new set auto-drafts a private post.
	db.AssignTicket("check", "75192-1", "Falcon", "", "", "", "admin")
	if p, _ := db.GetPost("75192-1"); p == nil || p.State != "draft" || p.Title != "Falcon" {
		t.Fatalf("auto-draft missing: %+v", p)
	}
}

func TestBlogStepsReadinessReviewNotesAndMessages(t *testing.T) {
	t.Setenv(config.LegoAttachmentsDir, t.TempDir())
	db, srv := testServer(t, "secret")
	defer srv.Close()
	post := func(path, body string) (int, string) {
		r := postJSON(t, srv.URL, path, body)
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b)
	}
	if c, b := post("/dashboard/blog", `{"slug":"21027-1","set_num":"21027-1","title":"Berlin","body":"A fine post.","by":"4821"}`); c != 200 {
		t.Fatalf("save = %d %s", c, b)
	}
	if err := db.SetBlogWriter("21027-1", "4821"); err != nil {
		t.Fatal(err)
	}
	finishSet(t, db, "21027-1", 4, 4)
	if c, b := post("/dashboard/blog/21027-1/state", `{"state":"review","by":"4821"}`); c != http.StatusConflict || !strings.Contains(b, "Overview confirmed") {
		t.Fatalf("sending an unfinished post = %d %s, want 409 naming what is missing", c, b)
	}
	c := &lego.SetCheck{SetNum: "21027-1", Kind: lego.CheckIntake, Status: lego.StatusDraft, CheckedBy: "4821", StartedAt: time.Now(),
		Lines: []lego.CheckLine{{PartNum: "3001", ColorID: 4, ColorName: "Red", Need: 4, Have: 4}}}
	if _, err := db.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	if code, b := post("/dashboard/blog/21027-1/pull-check", `{"by":"4821"}`); code != 200 || !strings.Contains(b, `"pieces":4`) {
		t.Fatalf("pull = %d %s", code, b)
	}
	if code, b := post("/dashboard/blog/21027-1/facts", `{"by":"4821","name":"Different","confirmed":true}`); code != 400 {
		t.Fatalf("a changed fact without a reason = %d %s, want 400", code, b)
	}
	if code, b := post("/dashboard/blog/21027-1/facts", `{"by":"4821","name":"Berlin","note":"","confirmed":true}`); code != 400 && code != 200 {
		t.Fatalf("facts = %d %s", code, b)
	}
	if code, b := post("/dashboard/blog/21027-1/facts", `{"by":"4821","name":"Berlin","note":"matches","confirmed":true}`); code != 200 {
		t.Fatalf("facts with a note = %d %s", code, b)
	}
	if code, _ := post("/dashboard/blog/21027-1/build", `{"by":"4821","bags":1,"stamp_ack":true}`); code != 200 {
		t.Fatal("build")
	}
	saved := `<html><script type="application/json" id="kc-state">{"set":"21027-1","lines":[{"p":"3001","c":4,"s":1,"m":"","x":"","i":"4821","k":"4821","d":"24/11/26"}],"borrowed":[],"rec":0}</script></html>`
	for _, up := range []struct {
		which, name string
		data        []byte
	}{{"sheet", "sheet.html", []byte(saved)}, {"instr", "book.pdf", []byte("%PDF-1.4 book")}} {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("which", up.which)
		_ = mw.WriteField("by", "4821")
		fw, _ := mw.CreateFormFile("file", up.name)
		_, _ = fw.Write(up.data)
		mw.Close()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/blog/21027-1/file", &buf)
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("upload %s: %v %v", up.which, err, resp)
		}
		resp.Body.Close()
	}
	for _, cap := range []string{"blog:Bag 1", "blog:Finished set"} {
		_, _ = db.AttachSetPhoto("21027-1", "jpeg", "x.jpg", cap, "4821")
	}
	if code, b := post("/dashboard/blog/21027-1/state", `{"state":"review","by":"4821"}`); code != 200 {
		t.Fatalf("a complete post should go for review: %d %s", code, b)
	}
	if code, _ := post("/dashboard/blog/21027-1/state", `{"state":"changes","by":"boss","note":""}`); code != 400 {
		t.Fatal("changes requested without a note must be refused")
	}
	if code, b := post("/dashboard/blog/21027-1/state", `{"state":"changes","by":"boss","note":"Add a clearer photo of bag 1"}`); code != 200 {
		t.Fatalf("changes = %d %s", code, b)
	}
	var inbox struct{ Messages []messageView }
	decodeBody(t, getWithToken(t, srv.URL, "/dashboard/messages?user=4821"), &inbox)
	if len(inbox.Messages) != 1 || !strings.Contains(inbox.Messages[0].Body, "clearer photo of bag 1") {
		t.Fatalf("the writer should be told what to fix: %+v", inbox.Messages)
	}
	var steps stepsView
	decodeBody(t, getWithToken(t, srv.URL, "/dashboard/blog/21027-1/steps"), &steps)
	if steps.State != "changes" || len(steps.Notes) < 1 || !steps.CanSend || steps.Writer != "4821" {
		t.Fatalf("steps = %+v", steps)
	}
	if code, _ := post("/dashboard/blog/21027-1/state", `{"state":"review","by":"4821"}`); code != 200 {
		t.Fatal("the writer can send it again after fixing")
	}
	if code, _ := post("/dashboard/blog/21027-1/state", `{"state":"published","by":"boss"}`); code != 200 {
		t.Fatal("approve")
	}
	pr := getWithToken(t, srv.URL, "/dashboard/blog/21027-1/file?which=sheet&name=nothere.html")
	pr.Body.Close()
	if pr.StatusCode != http.StatusNotFound {
		t.Errorf("a file that is not on the post must 404, got %d", pr.StatusCode)
	}
}
