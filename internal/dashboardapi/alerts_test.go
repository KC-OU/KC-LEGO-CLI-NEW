package dashboardapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestAlertsListsOpenEscalations(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	if _, _, err := db.RecordCheckOutcome("dave", lego.AccuracyChecker, lego.TicketCheck, "75192-1", 100, 25); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/admin/alerts", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Alerts []lego.AccuracyEscalation `json:"alerts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Alerts) != 1 || body.Alerts[0].Username != "dave" || body.Alerts[0].Missing != 25 {
		t.Fatalf("expected the one open escalation, got %+v", body.Alerts)
	}
}

func TestFileAccuracyReportClearsTheAlertAndRecordsIt(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	if _, _, err := db.RecordCheckOutcome("dave", lego.AccuracyChecker, lego.TicketCheck, "75192-1", 100, 25); err != nil {
		t.Fatal(err)
	}
	open, err := db.OpenAccuracyEscalations()
	if err != nil || len(open) != 1 {
		t.Fatalf("setup: %v, %+v", err, open)
	}

	body := fmt.Sprintf(`{"escalation_id":%d,"summary":"missed a whole bag","action_taken":"retrained on the checklist","talk_requested":true,"by":"admin"}`, open[0].ID)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/alerts/file", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	stillOpen, err := db.OpenAccuracyEscalations()
	if err != nil || len(stillOpen) != 0 {
		t.Fatalf("expected the alert to clear once filed, got %+v (err %v)", stillOpen, err)
	}
	filed, err := db.AccuracyReportsFor("dave", lego.AccuracyChecker)
	if err != nil || len(filed) != 1 || filed[0].Summary != "missed a whole bag" || !filed[0].TalkRequested {
		t.Fatalf("expected the filed report, got %+v (err %v)", filed, err)
	}
	msgs, err := db.UndeliveredMessages("dave")
	if err != nil || len(msgs) != 1 {
		t.Fatalf("expected a talk-requested message to dave, got %+v (err %v)", msgs, err)
	}
}

func TestFileAccuracyReportRequiresEveryField(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"escalation_id":1,"summary":"","action_taken":"x","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/alerts/file", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestFileAccuracyReportOnAnUnknownEscalationIs404(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"escalation_id":999,"summary":"x","action_taken":"y","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/alerts/file", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAccuracyReportsForReturnsAPersonsFiledHistory(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	if _, _, err := db.RecordCheckOutcome("dave", lego.AccuracyChecker, lego.TicketCheck, "75192-1", 100, 25); err != nil {
		t.Fatal(err)
	}
	open, _ := db.OpenAccuracyEscalations()
	if _, err := db.FileAccuracyReport(open[0].ID, "dave", lego.AccuracyChecker, "admin", "summary", "action", false); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/admin/accuracy-reports/dave?role=checker", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Reports []lego.AccuracyReport `json:"reports"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Reports) != 1 || body.Reports[0].Summary != "summary" {
		t.Fatalf("expected the one filed report, got %+v", body.Reports)
	}
}

func TestAlertRoutesRequireTheSharedToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/dashboard/admin/alerts")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	resp, err = http.Post(srv.URL+"/dashboard/admin/alerts/file", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
