// Multi-language support for the web dashboard only (the CLI/TUI is a
// separate, much larger i18n problem and out of scope here). One JSON file
// per locale under a translations directory, "en" always present as the
// reference language every other locale's completeness is measured
// against. Shipped as infrastructure first: the Rust side's own first real
// user is the shared sidebar nav, with the rest of ~35 templates' strings
// extracted incrementally in later passes -- see docs/guides for the plan.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

const baseLocale = "en"

// langCodeRE matches a 2-3 letter lowercase locale code (en, fr, de, pt-br
// would need more, but this round only needs plain ISO 639-1 codes).
var langCodeRE = regexp.MustCompile(`^[a-z]{2,3}$`)

// langStore persists the language manifest and each locale's key->string
// map as plain JSON files -- same one-file-per-concern pattern as
// internal/api's linkStore and this package's own currencyStore.
type langStore struct {
	dir string
	mu  sync.Mutex
}

func newLangStore(dir string) *langStore {
	if dir == "" {
		dir = translationsDir()
	}
	return &langStore{dir: dir}
}

func translationsDir() string {
	if d := config.Get(config.TranslationsDir); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(config.Get(config.LegoDBPath)), "translations")
}

func (s *langStore) manifestPath() string { return filepath.Join(s.dir, "languages.json") }
func (s *langStore) localePath(code string) string {
	return filepath.Join(s.dir, strings.ToLower(code)+".json")
}

// manifest loads the code->display-name map, always including "en" even if
// the file doesn't exist yet (a fresh install has no translations
// directory at all until the first write).
func (s *langStore) manifest() (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.manifestLocked()
}

func (s *langStore) manifestLocked() (map[string]string, error) {
	data, err := os.ReadFile(s.manifestPath())
	if os.IsNotExist(err) {
		return map[string]string{baseLocale: "English"}, nil
	}
	if err != nil {
		return nil, err
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out[baseLocale] == "" {
		out[baseLocale] = "English"
	}
	return out, nil
}

func (s *langStore) saveManifestLocked(m map[string]string) error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.manifestPath(), data, 0644)
}

// locale loads one locale's key->string map, never erroring on a missing
// file (a brand new locale, or "en" before its first string is set, is
// just empty).
func (s *langStore) locale(code string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.localeLocked(code)
}

func (s *langStore) localeLocked(code string) (map[string]string, error) {
	data, err := os.ReadFile(s.localePath(code))
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *langStore) saveLocaleLocked(code string, m map[string]string) error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.localePath(code), data, 0644)
}

type languageView struct {
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Keys         int     `json:"keys"`         // how many keys "en" currently has
	Translated   int     `json:"translated"`   // how many of those this locale has a non-empty value for
	Completeness float64 `json:"completeness"` // percent, 0-100; "en" is always 100
}

// handleListLanguages is GET /dashboard/admin/languages.
func (s *Server) handleListLanguages(w http.ResponseWriter, r *http.Request) {
	m, err := s.lang.manifest()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	en, err := s.lang.locale(baseLocale)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	codes := make([]string, 0, len(m))
	for code := range m {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	views := make([]languageView, 0, len(codes))
	for _, code := range codes {
		if code == baseLocale {
			views = append(views, languageView{Code: code, Name: m[code], Keys: len(en), Translated: len(en), Completeness: 100})
			continue
		}
		loc, err := s.lang.locale(code)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		translated := 0
		for k := range en {
			if strings.TrimSpace(loc[k]) != "" {
				translated++
			}
		}
		pct := 100.0
		if len(en) > 0 {
			pct = 100 * float64(translated) / float64(len(en))
		}
		views = append(views, languageView{Code: code, Name: m[code], Keys: len(en), Translated: translated, Completeness: pct})
	}
	writeJSON(w, http.StatusOK, map[string]any{"languages": views})
}

// handleGetLanguage is GET /dashboard/admin/languages/{code} -- every key
// "en" currently has, filled in with this locale's stored value (or "" for
// a key added to English after this locale was last touched).
func (s *Server) handleGetLanguage(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(r.PathValue("code"))
	en, err := s.lang.locale(baseLocale)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	loc, err := s.lang.locale(code)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	strs := make(map[string]string, len(en))
	for k, enVal := range en {
		if code == baseLocale {
			strs[k] = enVal
			continue
		}
		strs[k] = loc[k]
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "strings": strs})
}

type upsertLanguageStringRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	By    string `json:"by"`
}

// handleUpsertLanguageString is POST /dashboard/admin/languages/{code}/string.
// The key must already exist in "en" -- new keys are added by a code-side
// extraction pass (see i18n.go's own package comment), not typed in by an
// admin, so a translation can never silently orphan itself from the
// reference language's key set.
func (s *Server) handleUpsertLanguageString(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(r.PathValue("code"))
	var req upsertLanguageStringRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Key, req.By = strings.TrimSpace(req.Key), strings.TrimSpace(req.By)
	if req.Key == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key and by are required"})
		return
	}
	en, err := s.lang.locale(baseLocale)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, ok := en[req.Key]; !ok && code != baseLocale {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown key " + req.Key + " (not in the English reference set)"})
		return
	}
	m, err := s.lang.locale(code)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m[req.Key] = req.Value
	if err := s.lang.saveLocale(code, m); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_LANGUAGE_STRING_SET", "SUCCESS", code+": "+req.Key)
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (s *langStore) saveLocale(code string, m map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocaleLocked(code, m)
}

type addLanguageRequest struct {
	Code string `json:"code"`
	Name string `json:"name"`
	By   string `json:"by"`
}

// handleAddLanguage is POST /dashboard/admin/languages -- seeds the new
// locale with every "en" key mapped to "", ready for a translator to fill
// in via handleUpsertLanguageString.
func (s *Server) handleAddLanguage(w http.ResponseWriter, r *http.Request) {
	var req addLanguageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Code = strings.ToLower(strings.TrimSpace(req.Code))
	req.Name, req.By = strings.TrimSpace(req.Name), strings.TrimSpace(req.By)
	if req.Name == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code, name and by are required"})
		return
	}
	if !langCodeRE.MatchString(req.Code) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code must be 2-3 lowercase letters, e.g. fr, de, es"})
		return
	}
	s.lang.mu.Lock()
	defer s.lang.mu.Unlock()
	m, err := s.lang.manifestLocked()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, exists := m[req.Code]; exists {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "language " + req.Code + " already exists"})
		return
	}
	m[req.Code] = req.Name
	if err := s.lang.saveManifestLocked(m); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	en, err := s.lang.localeLocked(baseLocale)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	seed := make(map[string]string, len(en))
	for k := range en {
		seed[k] = ""
	}
	if err := s.lang.saveLocaleLocked(req.Code, seed); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_LANGUAGE_ADDED", "SUCCESS", req.Code+" ("+req.Name+")")
	writeJSON(w, http.StatusOK, languageView{Code: req.Code, Name: req.Name, Keys: len(en), Translated: 0, Completeness: 0})
}

type deleteLanguageRequest struct {
	Confirm string `json:"confirm"`
	By      string `json:"by"`
}

// handleDeleteLanguage is POST /dashboard/admin/languages/{code}/remove --
// confirm-gated and irreversible (the translated strings are gone, not
// just hidden), same convention as every other destructive dashboard
// action in this package.
func (s *Server) handleDeleteLanguage(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(r.PathValue("code"))
	if code == baseLocale {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot remove the English reference language"})
		return
	}
	var req deleteLanguageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.By = strings.TrimSpace(req.By)
	if req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	if err := requireConfirm(req.Confirm); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.lang.mu.Lock()
	defer s.lang.mu.Unlock()
	m, err := s.lang.manifestLocked()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, exists := m[code]; !exists {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "language not found"})
		return
	}
	delete(m, code)
	if err := s.lang.saveManifestLocked(m); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := os.Remove(s.lang.localePath(code)); err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_LANGUAGE_REMOVED", "SUCCESS", code)
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}
