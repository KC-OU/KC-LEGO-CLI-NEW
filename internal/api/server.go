package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/sync"
)

type Server struct {
	engine *sync.Engine
	db     *partdb.DB
	creds  *credentialStore
	links  *linkStore
	mux    *http.ServeMux
}

func NewServer(engine *sync.Engine, db *partdb.DB, credsPath, linkOverridesPath string) *Server {
	s := &Server{
		engine: engine,
		db:     db,
		creds:  newCredentialStore(credsPath),
		links:  newLinkStore(linkOverridesPath),
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func NewHTTPServer(engine *sync.Engine, db *partdb.DB, credsPath, linkOverridesPath string) *http.Server {
	return &http.Server{
		Addr:              ":" + config.Get(config.SyncPort),
		Handler:           NewServer(engine, db, credsPath, linkOverridesPath),
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/status", s.publicGET(s.handleStatus))
	s.mux.HandleFunc("/api/health", s.publicGET(s.handleHealth))
	s.mux.HandleFunc("/api/parts", s.publicGET(s.handleParts))
	s.mux.HandleFunc("/api/categories", s.publicGET(s.handleCategories))
	s.mux.HandleFunc("/api/stock", s.publicGET(s.handleStock))
	s.mux.HandleFunc("/metrics", s.handleMetrics)

	s.mux.HandleFunc("/api/sync", s.authedPOST(s.handleSync))
	s.mux.HandleFunc("/api/parts/update-links", s.authedPOST(s.handleUpdateLinks))
	s.mux.HandleFunc("/api/modernwms/reset-password", s.authedPOST(s.handleResetModernWMSPassword))
	s.mux.HandleFunc("/api/change-password", s.authedPOST(s.handleChangePassword))
}

func (s *Server) publicGET(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.engine.IncAPIRequests()
		h(w, r)
	}
}

// authedPOST requires Basic Auth on every mutating endpoint. The original
// Python service never checked auth on POST routes at all; that gap is
// fixed here rather than replicated, since this is new code.
func (s *Server) authedPOST(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || !s.creds.verify(user, pass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="PartDB-ModernWMS Security"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MiB cap; these bodies are a few small JSON fields
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "timestamp": time.Now().Format(time.RFC3339)})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats := s.engine.Stats()
	lastSync := ""
	var lastDuration float64
	if stats.LastResult != nil {
		lastSync = stats.LastResult.Timestamp.Format("2006-01-02T15:04:05.000000")
		lastDuration = stats.LastResult.DurationMS / 1000.0
	}

	history := make([]map[string]any, 0, len(stats.History))
	for _, h := range stats.History {
		history = append(history, map[string]any{
			"timestamp":       h.Timestamp.Format("2006-01-02 15:04:05"),
			"duration_ms":     h.DurationMS,
			"synced_parts":    h.SyncedParts,
			"total_stock_qty": h.TotalStockQty,
		})
	}

	statusStr := "Success"
	if stats.LastError != "" {
		statusStr = "Error"
	}

	last := sync.SyncResult{}
	if stats.LastResult != nil {
		last = *stats.LastResult
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         statusStr,
		"last_sync_time": lastSync,
		"stats": map[string]any{
			"synced_categories":     last.SyncedCategories,
			"synced_locations":      last.SyncedLocations,
			"synced_parts":          last.SyncedParts,
			"synced_stock_records":  last.SyncedStockRecords,
			"total_stock_qty":       last.TotalStockQty,
			"last_duration_seconds": lastDuration,
			"total_sync_count":      stats.TotalSyncCount,
			"total_error_count":     stats.TotalErrorCount,
			"api_requests_total":    stats.APIRequestsTotal,
		},
		"history": history,
	})
}

func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := s.db.AllCategories()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_categories": len(cats), "categories": cats})
}

func (s *Server) handleStock(w http.ResponseWriter, r *http.Request) {
	lots, err := s.db.AllPartLots()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_lots": len(lots), "lots": lots})
}

type partsAPIRow struct {
	PartDBID      int     `json:"partdb_id"`
	Name          string  `json:"name"`
	SpecCode      string  `json:"spec_code"`
	SPUCode       string  `json:"spu_code"`
	MfgPN         string  `json:"mfg_pn"`
	IPN           string  `json:"ipn"`
	CategoryName  string  `json:"category_name"`
	TotalQuantity float64 `json:"total_quantity"`
	Description   string  `json:"description"`
	PartDBLink    string  `json:"partdb_link"`
	ModernWMSLink string  `json:"modernwms_link"`
}

func (s *Server) handleParts(w http.ResponseWriter, r *http.Request) {
	parts, err := s.db.AllParts()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cats, err := s.db.AllCategories()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	lots, err := s.db.AllPartLots()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	overrides, err := s.links.all()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	catNames := make(map[int]string, len(cats))
	for _, c := range cats {
		catNames[c.ID] = c.Name
	}
	qtyByPart := make(map[int]float64, len(parts))
	for _, l := range lots {
		qtyByPart[l.PartID] += l.Amount
	}

	partdbURL := config.Get(config.PartDBURL)
	modernwmsURL := config.Get(config.ModernWMSURL)

	rows := make([]partsAPIRow, 0, len(parts))
	for _, p := range parts {
		specCode := p.Name
		code := sync.DeriveSPUCode(p.MfgPN, p.IPN, specCode, p.ID)

		row := partsAPIRow{
			PartDBID:      p.ID,
			Name:          p.Name,
			SpecCode:      specCode,
			SPUCode:       code,
			MfgPN:         p.MfgPN,
			IPN:           p.IPN,
			CategoryName:  catNames[p.CategoryID],
			TotalQuantity: qtyByPart[p.ID],
			Description:   p.Description,
			PartDBLink:    partLink(partdbURL, p.ID),
			ModernWMSLink: modernwmsURL,
		}
		if o, ok := overrides[p.ID]; ok {
			if o.PartDBLink != "" {
				row.PartDBLink = o.PartDBLink
			}
			if o.ModernWMSLink != "" {
				row.ModernWMSLink = o.ModernWMSLink
			}
		}
		rows = append(rows, row)
	}

	writeJSON(w, http.StatusOK, map[string]any{"total_parts": len(rows), "parts": rows})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.engine.IncAPIRequests()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(renderMetrics(s.engine.Stats())))
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	result, err := s.engine.SyncNow(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "success",
		"last_sync_time": result.Timestamp.Format(time.RFC3339),
		"duration_ms":    result.DurationMS,
	})
}

func (s *Server) handleUpdateLinks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PartID        int    `json:"part_id"`
		PartDBLink    string `json:"partdb_link"`
		ModernWMSLink string `json:"modernwms_link"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PartID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "message": "part_id is required"})
		return
	}
	if err := s.links.set(body.PartID, linkOverride{PartDBLink: body.PartDBLink, ModernWMSLink: body.ModernWMSLink}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Links saved for Part ID " + strconv.Itoa(body.PartID)})
}

func (s *Server) handleResetModernWMSPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NewPassword string `json:"new_password"`
		Username    string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "message": "invalid request body"})
		return
	}
	if body.Username == "" {
		body.Username = "admin"
	}
	if len(body.NewPassword) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "message": "password must be at least 6 characters"})
		return
	}
	if err := s.engine.ResetModernWMSPassword(r.Context(), body.NewPassword, body.Username); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "ModernWMS user '" + body.Username + "' password successfully updated!"})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username    string `json:"username"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "message": "invalid request body"})
		return
	}
	if body.Username == "" {
		body.Username = "admin"
	}
	if len(body.NewPassword) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "message": "password must be at least 6 characters"})
		return
	}
	if err := s.creds.set(body.Username, body.NewPassword); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Credentials updated"})
}

// partLink is the Part-DB page for a part, or "" when no Part-DB address is configured.
func partLink(base string, id int) string {
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/part/" + strconv.Itoa(id)
}
