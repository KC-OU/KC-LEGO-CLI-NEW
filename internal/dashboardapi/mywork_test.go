package dashboardapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestMyWorkListsAssignedChecksAndClaimsOneAtATime(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	a, _ := db.AssignTicket(lego.TicketCheck, "21027-1", "Berlin", "10932", "", "", "boss")
	b, _ := db.AssignTicket(lego.TicketCheck, "10294-1", "Titanic", "10932", "", "", "boss")
	_, _ = db.AssignTicket(lego.TicketCheck, "75192-1", "Falcon", "someone-else", "", "", "boss")
	_, _ = db.AssignTicket(lego.TicketCheck, "10696-1", "Open", "", "", "", "boss")

	var list struct{ Tickets []myWorkView }
	decodeBody(t, getWithToken(t, srv.URL, "/dashboard/my-work?user=10932"), &list)
	if len(list.Tickets) != 3 {
		t.Fatalf("want 2 assigned + 1 open check, got %+v", list.Tickets)
	}
	claim := func(id int64, by string) int {
		body, _ := json.Marshal(map[string]any{"id": id, "by": by})
		r := postJSON(t, srv.URL, "/dashboard/my-work/claim", string(body))
		defer r.Body.Close()
		return r.StatusCode
	}
	if c := claim(a.ID, "4821"); c != http.StatusForbidden {
		t.Fatalf("claiming someone else's check = %d, want 403", c)
	}
	if c := claim(a.ID, "10932"); c != http.StatusOK {
		t.Fatalf("claiming my check = %d, want 200", c)
	}
	if cur, _ := db.CurrentTicket("10932"); cur == nil || cur.Target != "21027-1" {
		t.Fatalf("the walk must now see the claimed check, got %+v", cur)
	}
	if c := claim(b.ID, "10932"); c != http.StatusConflict {
		t.Fatalf("a second claim while one is open = %d, want 409", c)
	}
	decodeBody(t, getWithToken(t, srv.URL, "/dashboard/my-work?user=10932"), &list)
	if list.Tickets[0].Status != lego.TicketClaimed || list.Tickets[0].Target != "21027-1" {
		t.Fatalf("the claimed check lists first, got %+v", list.Tickets)
	}
}
