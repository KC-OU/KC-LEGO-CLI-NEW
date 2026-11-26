package dashboardapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestListDownloadsStartsEmpty(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := getWithToken(t, srv.URL, "/dashboard/downloads")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Slots []downloadSlot `json:"slots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Slots) != 0 {
		t.Fatalf("slots = %v, want empty", out.Slots)
	}
}

func TestSetDownloadAddsThenUpdatesByName(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/downloads/set", `{"name":"Checklist","url":"https://docs.kcparts.xyz/a","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	// Setting the same name again updates it in place, not a second entry.
	resp2 := postJSON(t, srv.URL, "/dashboard/downloads/set", `{"name":"Checklist","url":"https://docs.kcparts.xyz/b","by":"admin"}`)
	defer resp2.Body.Close()
	var out struct {
		Slots []downloadSlot `json:"slots"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Slots) != 1 || out.Slots[0].URL != "https://docs.kcparts.xyz/b" {
		t.Fatalf("slots = %v", out.Slots)
	}
}

func TestSetDownloadRequiresAllFields(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/downloads/set", `{"name":"Checklist","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing url)", resp.StatusCode)
	}
}

func TestRemoveDownloadDropsTheNamedSlot(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	postJSON(t, srv.URL, "/dashboard/downloads/set", `{"name":"Checklist","url":"https://docs.kcparts.xyz/a","by":"admin"}`).Body.Close()

	resp := postJSON(t, srv.URL, "/dashboard/downloads/remove", `{"name":"Checklist","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Slots []downloadSlot `json:"slots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Slots) != 0 {
		t.Fatalf("slots = %v, want empty after removal", out.Slots)
	}
}

func TestRemoveDownloadRejectsAnUnknownName(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/downloads/remove", `{"name":"Nope","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestSetDownloadDefaultsToCurrentSectionAndStampsUpdatedAt(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/downloads/set", `{"name":"Checklist","url":"https://docs.kcparts.xyz/a","by":"admin"}`)
	defer resp.Body.Close()
	var out struct {
		Slots []downloadSlot `json:"slots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Slots) != 1 || out.Slots[0].Section != "current" {
		t.Fatalf("slots = %v, want one slot defaulted to section current", out.Slots)
	}
	if out.Slots[0].UpdatedAt == "" {
		t.Fatalf("slots = %v, want updated_at stamped", out.Slots)
	}
}

func TestSetDownloadAcceptsAnArchiveSection(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/downloads/set", `{"name":"KC-Parts - Set Overview: Legacy Archive (Pre-2025)","url":"https://docs.kcparts.xyz/legacy","section":"Archive","by":"admin"}`)
	defer resp.Body.Close()
	var out struct {
		Slots []downloadSlot `json:"slots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Slots) != 1 || out.Slots[0].Section != "archive" {
		t.Fatalf("slots = %v, want section archive (case/space normalized)", out.Slots)
	}
}

func TestSetDownloadAcceptsAnyCustomSectionName(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/downloads/set", `{"name":"Checklist","url":"https://docs.kcparts.xyz/a","section":"Guides","by":"admin"}`)
	defer resp.Body.Close()
	var out struct {
		Slots []downloadSlot `json:"slots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Slots) != 1 || out.Slots[0].Section != "guides" {
		t.Fatalf("slots = %v, want a free-text section accepted and case-folded", out.Slots)
	}
}

func TestSetDownloadUpdatingAnExistingSlotCanMoveItToArchive(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	postJSON(t, srv.URL, "/dashboard/downloads/set", `{"name":"Checklist","url":"https://docs.kcparts.xyz/a","by":"admin"}`).Body.Close()
	resp := postJSON(t, srv.URL, "/dashboard/downloads/set", `{"name":"Checklist","url":"https://docs.kcparts.xyz/a","section":"archive","by":"admin"}`)
	defer resp.Body.Close()
	var out struct {
		Slots []downloadSlot `json:"slots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Slots) != 1 || out.Slots[0].Section != "archive" {
		t.Fatalf("slots = %v, want the same slot moved to archive, not duplicated", out.Slots)
	}
}
