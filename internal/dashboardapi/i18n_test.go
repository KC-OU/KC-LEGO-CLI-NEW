package dashboardapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestListLanguagesAlwaysIncludesEnglishAtFullCompleteness(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := getWithToken(t, srv.URL, "/dashboard/admin/languages")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Languages []languageView `json:"languages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Languages) != 1 || out.Languages[0].Code != "en" || out.Languages[0].Completeness != 100 {
		t.Fatalf("got %+v", out.Languages)
	}
}

func TestAddLanguageSeedsEveryEnglishKeyAsBlank(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	postJSON(t, srv.URL, "/dashboard/admin/languages/en/string", `{"key":"nav.item.dashboard","value":"Dashboard","by":"admin"}`).Body.Close()

	resp := postJSON(t, srv.URL, "/dashboard/admin/languages", `{"code":"fr","name":"Français","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var v languageView
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	if v.Code != "fr" || v.Keys != 1 || v.Translated != 0 {
		t.Fatalf("got %+v", v)
	}

	get := getWithToken(t, srv.URL, "/dashboard/admin/languages/fr")
	defer get.Body.Close()
	var got struct {
		Strings map[string]string `json:"strings"`
	}
	if err := json.NewDecoder(get.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if val, ok := got.Strings["nav.item.dashboard"]; !ok || val != "" {
		t.Fatalf("strings = %v", got.Strings)
	}
}

func TestAddLanguageRejectsADuplicateCode(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	postJSON(t, srv.URL, "/dashboard/admin/languages", `{"code":"fr","name":"French","by":"admin"}`).Body.Close()
	resp := postJSON(t, srv.URL, "/dashboard/admin/languages", `{"code":"fr","name":"French again","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestUpsertLanguageStringRejectsAKeyNotInEnglish(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	postJSON(t, srv.URL, "/dashboard/admin/languages", `{"code":"fr","name":"French","by":"admin"}`).Body.Close()
	resp := postJSON(t, srv.URL, "/dashboard/admin/languages/fr/string", `{"key":"made.up.key","value":"x","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestUpsertLanguageStringUpdatesCompleteness(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	postJSON(t, srv.URL, "/dashboard/admin/languages/en/string", `{"key":"nav.item.sets","value":"Sets","by":"admin"}`).Body.Close()
	postJSON(t, srv.URL, "/dashboard/admin/languages", `{"code":"fr","name":"French","by":"admin"}`).Body.Close()
	postJSON(t, srv.URL, "/dashboard/admin/languages/fr/string", `{"key":"nav.item.sets","value":"Ensembles","by":"admin"}`).Body.Close()

	resp := getWithToken(t, srv.URL, "/dashboard/admin/languages")
	defer resp.Body.Close()
	var out struct {
		Languages []languageView `json:"languages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	var fr *languageView
	for i := range out.Languages {
		if out.Languages[i].Code == "fr" {
			fr = &out.Languages[i]
		}
	}
	if fr == nil || fr.Completeness != 100 {
		t.Fatalf("got %+v", out.Languages)
	}
}

func TestDeleteLanguageRefusesWithoutConfirm(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	postJSON(t, srv.URL, "/dashboard/admin/languages", `{"code":"fr","name":"French","by":"admin"}`).Body.Close()
	resp := postJSON(t, srv.URL, "/dashboard/admin/languages/fr/remove", `{"by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestDeleteLanguageRefusesEnglish(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/admin/languages/en/remove", `{"by":"admin","confirm":"yes"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestDeleteLanguageRemovesIt(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	postJSON(t, srv.URL, "/dashboard/admin/languages", `{"code":"fr","name":"French","by":"admin"}`).Body.Close()
	resp := postJSON(t, srv.URL, "/dashboard/admin/languages/fr/remove", `{"by":"admin","confirm":"yes"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	list := getWithToken(t, srv.URL, "/dashboard/admin/languages")
	defer list.Body.Close()
	var out struct {
		Languages []languageView `json:"languages"`
	}
	if err := json.NewDecoder(list.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Languages) != 1 {
		t.Fatalf("got %+v", out.Languages)
	}
}
