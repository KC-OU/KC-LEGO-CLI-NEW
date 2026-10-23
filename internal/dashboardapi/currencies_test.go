package dashboardapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeFXServer stands in for open.er-api.com: a single success response for
// any base, holding USD/EUR/GBP so add/refresh have something real to find.
func fakeFXServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": "success",
			"rates":  map[string]float64{"GBP": 1, "USD": 1.27, "EUR": 1.15},
		})
	}))
	old := fxAPIBaseURL
	fxAPIBaseURL = srv.URL
	t.Cleanup(func() {
		fxAPIBaseURL = old
		srv.Close()
	})
	return srv
}

func TestListCurrenciesDefaultsToTheBaseAloneWithNoCachedRates(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := getWithToken(t, srv.URL, "/dashboard/currencies")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out currenciesView
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Base != "GBP" || len(out.Codes) != 1 || out.Codes[0] != "GBP" {
		t.Fatalf("got %+v", out)
	}
}

func TestAddCurrencyFetchesRealRatesAndPersists(t *testing.T) {
	fakeFXServer(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/currencies/add", `{"code":"usd","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out currenciesView
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Codes) != 2 || out.Codes[1] != "USD" {
		t.Fatalf("codes = %v", out.Codes)
	}
	if out.Rates["USD"] != 1.27 {
		t.Fatalf("rates = %v", out.Rates)
	}
}

func TestAddCurrencyRejectsAnUnknownCode(t *testing.T) {
	fakeFXServer(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/currencies/add", `{"code":"ZZZ","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if !strings.Contains(out["error"], "unknown currency code") {
		t.Fatalf("error = %q", out["error"])
	}
}

func TestRemoveCurrencyRefusesToRemoveTheBase(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/currencies/remove", `{"code":"GBP","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestRemoveCurrencyDropsAPreviouslyAddedOne(t *testing.T) {
	fakeFXServer(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	postJSON(t, srv.URL, "/dashboard/currencies/add", `{"code":"USD","by":"admin"}`).Body.Close()

	resp := postJSON(t, srv.URL, "/dashboard/currencies/remove", `{"code":"USD","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out currenciesView
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Codes) != 1 {
		t.Fatalf("codes = %v", out.Codes)
	}
}

func TestRefreshCurrenciesForcesANewFetchRegardlessOfStaleness(t *testing.T) {
	fakeFXServer(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/currencies/refresh", `{"by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out currenciesView
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.UpdatedAt == "" || out.Rates["GBP"] != 1 {
		t.Fatalf("got %+v", out)
	}
}

func TestCurrencyActionsRequireBy(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/currencies/add", `{"code":"USD"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
