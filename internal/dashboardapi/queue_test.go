package dashboardapi

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestQueueListCancelAndSweep(t *testing.T) {
	db, srv := testServer(t, "tok")
	old, _ := db.AssignTicket("check", "75192-1", "Falcon", "", "", "", "admin")
	keep, _ := db.AssignTicket("check", "10294-1", "Titanic", "", "", "", "admin")
	db.Exec(`UPDATE job_tickets SET created_at = ? WHERE id = ?`, time.Now().Add(-9*24*time.Hour).Format("2006-01-02T15:04:05.999999"), old.ID)

	do := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer tok")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b strings.Builder
		buf := make([]byte, 4096)
		for n, _ := resp.Body.Read(buf); n > 0; n, _ = resp.Body.Read(buf) {
			b.Write(buf[:n])
		}
		return resp.StatusCode, b.String()
	}
	if c, b := do("GET", "/dashboard/queue?days=7", ""); c != 200 || !strings.Contains(b, `"stale":1`) {
		t.Fatalf("list = %d %s", c, b)
	}
	if c, _ := do("POST", "/dashboard/queue/cancel", `{"id":`+itoa(keep.ID)+`,"by":"kc"}`); c != 400 {
		t.Fatalf("missing reason should be 400, got %d", c)
	}
	if c, b := do("POST", "/dashboard/queue/cancel-stale", `{"days":7,"by":"kc"}`); c != 200 || !strings.Contains(b, `"removed":1`) {
		t.Fatalf("sweep = %d %s", c, b)
	}
	if c, b := do("POST", "/dashboard/queue/cancel", `{"id":`+itoa(keep.ID)+`,"by":"kc","reason":"added by mistake"}`); c != 200 || !strings.Contains(b, `"removed":1`) {
		t.Fatalf("cancel = %d %s", c, b)
	}
	open, _ := db.AllOpenTickets()
	if len(open) != 0 {
		t.Fatalf("queue should be empty, has %d", len(open))
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
