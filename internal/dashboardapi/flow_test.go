package dashboardapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func decodeBody(t *testing.T, r *http.Response, v any) {
	t.Helper()
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func uploadPaper(t *testing.T, srvURL string, id int64, user, filename string, data []byte) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("user", user)
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, srvURL+"/dashboard/onboarding/flow/"+flowID(id)+"/paper", &buf)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func flowID(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestInboxEndpointsAdminSendsCheckerRepliesOnly(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	if r := postJSON(t, srv.URL, "/dashboard/messages/send", `{"from":"4821","to":"admin","body":"hi","admin":false}`); r.StatusCode != http.StatusForbidden {
		t.Fatalf("a checker starting a message = %d, want 403", r.StatusCode)
	}
	if r := postJSON(t, srv.URL, "/dashboard/messages/send", `{"from":"admin","to":"4821","body":"Welcome","admin":true}`); r.StatusCode != http.StatusOK {
		t.Fatalf("admin send = %d", r.StatusCode)
	}
	var list struct {
		Messages []messageView
		Unread   int
	}
	decodeBody(t, getWithToken(t, srv.URL, "/dashboard/messages?user=4821"), &list)
	if len(list.Messages) != 1 || list.Unread != 1 || list.Messages[0].Read {
		t.Fatalf("inbox = %+v", list)
	}
	if r := postJSON(t, srv.URL, "/dashboard/messages/reply", `{"from":"4821","parent_id":`+flowID(list.Messages[0].ID)+`,"body":"Thanks"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("reply = %d", r.StatusCode)
	}
	var unread map[string]int
	decodeBody(t, postJSONResp(t, srv.URL, "/dashboard/messages/read", `{"user":"4821","id":0}`), &unread)
	if unread["unread"] != 0 {
		t.Errorf("after mark-all-read unread = %d", unread["unread"])
	}
	decodeBody(t, getWithToken(t, srv.URL, "/dashboard/messages/unread?user=admin"), &unread)
	if unread["unread"] != 1 {
		t.Errorf("the admin should have the reply unread, got %d", unread["unread"])
	}
}

func postJSONResp(t *testing.T, srv, path, body string) *http.Response {
	return postJSON(t, srv, path, body)
}

func TestOnboardingFlowOverHTTP(t *testing.T) {
	t.Setenv(config.LegoAttachmentsDir, t.TempDir())
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users[access.Key("wms", "boss")] = &access.User{Groups: []string{"admin"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	db, srv := testServer(t, "secret")
	defer srv.Close()

	var f flowView
	decodeBody(t, postJSON(t, srv.URL, "/dashboard/onboarding/flow/start", `{"set":"21027-1","user":"4821"}`), &f)
	if f.ID == 0 || f.Status != lego.FlowAwaitingPaper || f.Cleared {
		t.Fatalf("started = %+v", f)
	}
	if r := uploadPaper(t, srv.URL, f.ID, "4821", "sheet.html", []byte("<html>not a saved sheet</html>")); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("a plain html file = %d, want 400", r.StatusCode)
	}
	saved := `<html><script type="application/json" id="kc-state">{"set":"21027-1","lines":[{"p":"3001","c":4,"s":2,"m":"2","x":"0"}],"borrowed":[],"rec":0}</script></html>`
	r := uploadPaper(t, srv.URL, f.ID, "4821", "sorting-sheet-21027-1-filled.html", []byte(saved))
	if r.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(r.Body)
		t.Fatalf("saved sheet upload = %d %s", r.StatusCode, b)
	}
	decodeBody(t, r, &f)
	if f.Status != lego.FlowAwaitingVerify || !f.Digital {
		t.Fatalf("after upload = %+v", f)
	}
	var boss struct{ Unread int }
	decodeBody(t, getWithToken(t, srv.URL, "/dashboard/messages/unread?user=boss"), &boss)
	if boss.Unread != 1 {
		t.Errorf("the admin should be told a paper check is waiting, unread = %d", boss.Unread)
	}
	// the stored sheet is only ever a download
	pr := getWithToken(t, srv.URL, "/dashboard/onboarding/flow/"+flowID(f.ID)+"/paper")
	if ct := pr.Header.Get("Content-Type"); ct != "application/octet-stream" || !strings.HasPrefix(pr.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("an uploaded sheet must download, got %q / %q", ct, pr.Header.Get("Content-Disposition"))
	}
	pr.Body.Close()

	if r := postJSON(t, srv.URL, "/dashboard/onboarding/flow/"+flowID(f.ID)+"/verify", `{"by":"boss","ok":false,"note":""}`); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("send back without a note = %d, want 400", r.StatusCode)
	}
	postJSON(t, srv.URL, "/dashboard/onboarding/flow/"+flowID(f.ID)+"/verify", `{"by":"boss","ok":false,"note":"IDs missing on page 2","tags":"IDs missing"}`).Body.Close()
	var inbox struct{ Messages []messageView }
	decodeBody(t, getWithToken(t, srv.URL, "/dashboard/messages?user=4821"), &inbox)
	if len(inbox.Messages) != 1 || !strings.Contains(inbox.Messages[0].Body, "IDs missing on page 2") {
		t.Fatalf("the checker should be told what to change: %+v", inbox.Messages)
	}
	r = uploadPaper(t, srv.URL, f.ID, "4821", "again.html", []byte(saved))
	r.Body.Close()
	if r := postJSON(t, srv.URL, "/dashboard/onboarding/flow/"+flowID(f.ID)+"/verify", `{"by":"boss","ok":true}`); r.StatusCode != http.StatusOK {
		t.Fatalf("verify = %d", r.StatusCode)
	}
	if !db.OnboardingCleared("21027-1", "4821") {
		t.Fatal("verified checker must be cleared")
	}
	if r := postJSON(t, srv.URL, "/dashboard/onboarding/flow/"+flowID(f.ID)+"/complete", `{"user":"4821","answers":{}}`); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("submit before the check = %d, want 400", r.StatusCode)
	}
	time.Sleep(1100 * time.Millisecond)
	c := &lego.SetCheck{SetNum: "21027-1", Kind: lego.CheckIntake, Status: lego.StatusDraft, CheckedBy: "4821", StartedAt: time.Now(),
		Lines: []lego.CheckLine{{PartNum: "3001", ColorID: 4, ColorName: "Red", Need: 4, Have: 2}}}
	if _, err := db.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	var detail flowView
	decodeBody(t, getWithToken(t, srv.URL, "/dashboard/onboarding/flow/"+flowID(f.ID)), &detail)
	if !detail.CheckDone || len(detail.Diff) != 0 {
		t.Fatalf("detail = %+v, want the check done and the paper (2 missing) matching it", detail)
	}
	// Two parts are still missing and nothing is borrowed: the blog must not be generated, for anyone.
	if r := postJSON(t, srv.URL, "/dashboard/onboarding/flow/"+flowID(f.ID)+"/complete", `{"user":"4821","answers":{"counted":"yes"}}`); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("complete with parts missing = %d, want 400", r.StatusCode)
	}
	if r := getWithToken(t, srv.URL, "/dashboard/onboarding/blog-template?set=21027-1"); r.StatusCode != http.StatusConflict {
		t.Fatalf("blog template with parts missing = %d, want 409", r.StatusCode)
	}
	// Recording the two as borrowed from another set makes the set complete, so the blog may go ahead.
	if _, err := db.AddBorrowedPart(lego.BorrowedPart{BuildSetNum: "21027-1", NeedPartNum: "3001", ColorID: 4, Qty: 2, FromSetNum: "10696-1"}); err != nil {
		t.Fatal(err)
	}
	if r := postJSON(t, srv.URL, "/dashboard/onboarding/flow/"+flowID(f.ID)+"/complete", `{"user":"4821","answers":{"counted":"yes"}}`); r.StatusCode != http.StatusOK {
		t.Fatalf("complete = %d", r.StatusCode)
	}
	if p, err := db.GetPost("21027-1"); err != nil || p == nil {
		t.Error("completing the onboarding should open the set's blog draft")
	}
	tpl := getWithToken(t, srv.URL, "/dashboard/onboarding/blog-template?set=21027-1")
	body, _ := io.ReadAll(tpl.Body)
	tpl.Body.Close()
	if !strings.Contains(string(body), "set: 21027-1") || !strings.HasPrefix(tpl.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("blog template = %q (%s)", body, tpl.Header.Get("Content-Disposition"))
	}
}
