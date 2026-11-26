package dashboardapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestListOnboardingItemsSeedsTheDefaultChecklist(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := getWithToken(t, srv.URL, "/dashboard/onboarding/items")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Items []onboardingItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) == 0 {
		t.Fatal("expected the default checklist to be seeded")
	}
}

func TestSubmitOnboardingRejectsAMissingRequiredAnswer(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/onboarding/submit", `{"set_num":"75192-1","by":"dave","answers":[]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (no required items answered)", resp.StatusCode)
	}
}

// submitWithAllRequiredAnswered fetches the (default) checklist and
// submits it for setNum with every required item marked "yes" -- every
// test past the validation test itself needs a submission that actually
// gets recorded, not rejected for missing required answers.
func submitWithAllRequiredAnswered(t *testing.T, srvURL, setNum, by string) *http.Response {
	t.Helper()
	itemsResp := getWithToken(t, srvURL, "/dashboard/onboarding/items")
	var itemsOut struct {
		Items []onboardingItem `json:"items"`
	}
	if err := json.NewDecoder(itemsResp.Body).Decode(&itemsOut); err != nil {
		t.Fatal(err)
	}
	itemsResp.Body.Close()

	answers := make([]map[string]string, 0)
	for _, it := range itemsOut.Items {
		if it.Required {
			answers = append(answers, map[string]string{"item_id": it.ID, "value": "yes"})
		}
	}
	body, err := json.Marshal(map[string]any{"set_num": setNum, "by": by, "answers": answers})
	if err != nil {
		t.Fatal(err)
	}
	return postJSON(t, srvURL, "/dashboard/onboarding/submit", string(body))
}

func TestSubmitOnboardingSucceedsWhenEveryRequiredItemIsAnswered(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := submitWithAllRequiredAnswered(t, srv.URL, "75192-1", "dave")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	lookup := getWithToken(t, srv.URL, "/dashboard/onboarding/submissions?set=75192-1")
	defer lookup.Body.Close()
	var lookupOut struct {
		Submissions []onboardingSubmissionView `json:"submissions"`
	}
	if err := json.NewDecoder(lookup.Body).Decode(&lookupOut); err != nil {
		t.Fatal(err)
	}
	if len(lookupOut.Submissions) != 1 {
		t.Fatalf("submissions = %v", lookupOut.Submissions)
	}
	if lookupOut.Submissions[0].Status != "submitted" {
		t.Fatalf("status = %q", lookupOut.Submissions[0].Status)
	}
}

func TestLookupOnboardingSubmissionsOnlyReturnsMatchingSet(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	submitWithAllRequiredAnswered(t, srv.URL, "75192-1", "dave").Body.Close()
	submitWithAllRequiredAnswered(t, srv.URL, "10220-1", "dave").Body.Close()

	resp := getWithToken(t, srv.URL, "/dashboard/onboarding/submissions?set=10220-1")
	defer resp.Body.Close()
	var out struct {
		Submissions []onboardingSubmissionView `json:"submissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Submissions) != 1 || out.Submissions[0].SetNum != "10220-1" {
		t.Fatalf("submissions = %v", out.Submissions)
	}
}

func TestReviewOnboardingSubmissionUpdatesStatusAndNote(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	submitWithAllRequiredAnswered(t, srv.URL, "75192-1", "dave").Body.Close()
	lookup := getWithToken(t, srv.URL, "/dashboard/onboarding/submissions?set=75192-1")
	var lookupOut struct {
		Submissions []onboardingSubmissionView `json:"submissions"`
	}
	if err := json.NewDecoder(lookup.Body).Decode(&lookupOut); err != nil {
		t.Fatal(err)
	}
	lookup.Body.Close()
	id := lookupOut.Submissions[0].ID

	body, err := json.Marshal(map[string]string{"id": id, "status": "flagged", "note": "counts look off", "by": "admin"})
	if err != nil {
		t.Fatal(err)
	}
	resp := postJSON(t, srv.URL, "/dashboard/onboarding/submissions/review", string(body))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	lookup2 := getWithToken(t, srv.URL, "/dashboard/onboarding/submissions?set=75192-1")
	defer lookup2.Body.Close()
	var out2 struct {
		Submissions []onboardingSubmissionView `json:"submissions"`
	}
	if err := json.NewDecoder(lookup2.Body).Decode(&out2); err != nil {
		t.Fatal(err)
	}
	if out2.Submissions[0].Status != "flagged" || out2.Submissions[0].ReviewNote != "counts look off" {
		t.Fatalf("got %+v", out2.Submissions[0])
	}
}

func TestVerifyOnboardingUploadReportsNotConfiguredWithNoTeedySetUp(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := getWithToken(t, srv.URL, "/dashboard/onboarding/verify-upload?filename=75192-1_Dave_2026-10-24.pdf")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Configured bool `json:"configured"`
		Found      bool `json:"found"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Configured || out.Found {
		t.Fatalf("got %+v, want both false with no TEEDY_* env set", out)
	}
}

func TestSetOnboardingItemAddsThenUpdatesByID(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/onboarding/items/set", `{"id":"custom-1","text":"Did you check the box?","required":true,"by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	resp2 := postJSON(t, srv.URL, "/dashboard/onboarding/items/set", `{"id":"custom-1","text":"Did you check the box twice?","required":false,"by":"admin"}`)
	defer resp2.Body.Close()
	var out struct {
		Items []onboardingItem `json:"items"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range out.Items {
		if it.ID == "custom-1" {
			found = true
			if it.Text != "Did you check the box twice?" || it.Required {
				t.Fatalf("got %+v", it)
			}
		}
	}
	if !found {
		t.Fatal("custom-1 not found after update")
	}
}

func TestRemoveOnboardingItemRejectsAnUnknownID(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/onboarding/items/remove", `{"id":"nope","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
