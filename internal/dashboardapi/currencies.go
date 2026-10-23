// Multi-currency: the user wants to add their own currencies and see live
// exchange rates, refreshed periodically and cached server-side rather than
// fetched on every page load (see this round's scope interview). Rates come
// from open.er-api.com's free, keyless endpoint -- no signup, no API key,
// one call returns every currency's rate against a base in one response.
package dashboardapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// fxAPIBaseURL is a var, not a const, so tests can point it at a local fake
// server instead of the real network.
var fxAPIBaseURL = "https://open.er-api.com/v6/latest"

// currencyRefreshInterval is how stale the cached rates must be before
// handleListCurrencies bothers refreshing them on read.
const currencyRefreshInterval = 12 * time.Hour

type currencyData struct {
	Base      string             `json:"base"`
	Codes     []string           `json:"codes"`
	Rates     map[string]float64 `json:"rates"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// currencyStore persists the configured currency list and the last-fetched
// rates to one JSON file -- same pattern as internal/api's linkStore.
type currencyStore struct {
	path string
	mu   sync.Mutex
}

func newCurrencyStore(path string) *currencyStore {
	if path == "" {
		path = currenciesFile()
	}
	return &currencyStore{path: path}
}

func currenciesFile() string {
	if p := config.Get(config.CurrenciesFile); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(config.Get(config.LegoDBPath)), "currencies.json")
}

func (s *currencyStore) load() (currencyData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		base := config.Get(config.BricklinkCurrency)
		if base == "" {
			base = "GBP"
		}
		return currencyData{Base: base, Codes: []string{base}, Rates: map[string]float64{}}, nil
	}
	if err != nil {
		return currencyData{}, err
	}
	var out currencyData
	if err := json.Unmarshal(data, &out); err != nil {
		return currencyData{}, err
	}
	if out.Rates == nil {
		out.Rates = map[string]float64{}
	}
	return out, nil
}

func (s *currencyStore) save(d currencyData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// refreshRates fetches every rate for d.Base in one call and overwrites
// d.Rates/d.UpdatedAt in place -- it never touches d.Codes.
func refreshRates(d *currencyData) error {
	resp, err := http.Get(fmt.Sprintf("%s/%s", fxAPIBaseURL, d.Base))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fx rates: %s", strings.TrimSpace(string(body)))
	}
	var out struct {
		Result string             `json:"result"`
		Rates  map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return err
	}
	if out.Result != "success" {
		return fmt.Errorf("fx rates: provider reported %q", out.Result)
	}
	d.Rates = out.Rates
	d.UpdatedAt = time.Now().UTC()
	return nil
}

type currenciesView struct {
	Base      string             `json:"base"`
	Codes     []string           `json:"codes"`
	Rates     map[string]float64 `json:"rates"`
	UpdatedAt string             `json:"updated_at,omitempty"`
}

func currenciesToView(d currencyData) currenciesView {
	at := ""
	if !d.UpdatedAt.IsZero() {
		at = d.UpdatedAt.Format("2006-01-02 15:04")
	}
	return currenciesView{Base: d.Base, Codes: d.Codes, Rates: d.Rates, UpdatedAt: at}
}

// handleListCurrencies is GET /dashboard/currencies -- best-effort refresh
// if the cached rates are stale, then serve whatever's cached either way;
// a refresh failure (no network, provider down) must never break the page.
func (s *Server) handleListCurrencies(w http.ResponseWriter, r *http.Request) {
	d, err := s.currencies.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if time.Since(d.UpdatedAt) > currencyRefreshInterval {
		if rerr := refreshRates(&d); rerr == nil {
			_ = s.currencies.save(d)
		}
	}
	writeJSON(w, http.StatusOK, currenciesToView(d))
}

type currencyActionRequest struct {
	Code string `json:"code"`
	By   string `json:"by"`
}

// handleAddCurrency is POST /dashboard/currencies/add.
func (s *Server) handleAddCurrency(w http.ResponseWriter, r *http.Request) {
	var req currencyActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Code = strings.ToUpper(strings.TrimSpace(req.Code))
	req.By = strings.TrimSpace(req.By)
	if req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	if len(req.Code) != 3 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code must be a 3-letter currency code"})
		return
	}
	d, err := s.currencies.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, c := range d.Codes {
		if c == req.Code {
			writeJSON(w, http.StatusOK, currenciesToView(d)) // already added, no-op
			return
		}
	}
	if err := refreshRates(&d); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not reach the exchange-rate provider: " + err.Error()})
		return
	}
	if _, ok := d.Rates[req.Code]; !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown currency code " + req.Code})
		return
	}
	d.Codes = append(d.Codes, req.Code)
	if err := s.currencies.save(d); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_CURRENCY_ADDED", "SUCCESS", req.Code)
	writeJSON(w, http.StatusOK, currenciesToView(d))
}

// handleRemoveCurrency is POST /dashboard/currencies/remove.
func (s *Server) handleRemoveCurrency(w http.ResponseWriter, r *http.Request) {
	var req currencyActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Code = strings.ToUpper(strings.TrimSpace(req.Code))
	req.By = strings.TrimSpace(req.By)
	if req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	d, err := s.currencies.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if req.Code == d.Base {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot remove the base currency"})
		return
	}
	kept := make([]string, 0, len(d.Codes))
	found := false
	for _, c := range d.Codes {
		if c == req.Code {
			found = true
			continue
		}
		kept = append(kept, c)
	}
	if !found {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "currency not configured"})
		return
	}
	d.Codes = kept
	if err := s.currencies.save(d); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_CURRENCY_REMOVED", "SUCCESS", req.Code)
	writeJSON(w, http.StatusOK, currenciesToView(d))
}

type refreshCurrenciesRequest struct {
	By string `json:"by"`
}

// handleRefreshCurrencies is POST /dashboard/currencies/refresh -- a manual
// override of the on-read staleness check, for "I just added a currency
// and want today's rate right now."
func (s *Server) handleRefreshCurrencies(w http.ResponseWriter, r *http.Request) {
	var req refreshCurrenciesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.By = strings.TrimSpace(req.By)
	if req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	d, err := s.currencies.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := refreshRates(&d); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not reach the exchange-rate provider: " + err.Error()})
		return
	}
	if err := s.currencies.save(d); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_CURRENCY_REFRESHED", "SUCCESS", fmt.Sprintf("%d rates", len(d.Rates)))
	writeJSON(w, http.StatusOK, currenciesToView(d))
}
