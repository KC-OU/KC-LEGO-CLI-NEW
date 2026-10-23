// Dock/Credit Accuracy -- the admin write action behind the read-only
// accuracy view handleAccuracy already exposes. Mirrors
// internal/uiapp/accuracy_screens.go's DockAccuracy/CreditAccuracy call;
// deliberately skips that screen's extra LogEvent/notify.Dispatch/
// SendMessage side effects for now (message delivery is its own feature,
// not part of applying the adjustment) -- s.logAudit still records it.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

type adjustAccuracyRequest struct {
	Username string  `json:"username"`
	Role     string  `json:"role"`
	Kind     string  `json:"kind"` // "dock" or "credit"
	Amount   float64 `json:"amount"`
	Reason   string  `json:"reason"`
	By       string  `json:"by"`
}

func (s *Server) handleAdjustAccuracy(w http.ResponseWriter, r *http.Request) {
	var req adjustAccuracyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Username, req.Role = strings.TrimSpace(req.Username), strings.TrimSpace(req.Role)
	req.Kind, req.Reason, req.By = strings.ToLower(strings.TrimSpace(req.Kind)), strings.TrimSpace(req.Reason), strings.TrimSpace(req.By)
	if req.Username == "" || req.By == "" || req.Reason == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username, reason and by are required"})
		return
	}
	if req.Role == "" {
		req.Role = lego.AccuracyChecker
	}
	if req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "amount must be positive"})
		return
	}

	var err error
	switch req.Kind {
	case "dock":
		err = s.legoDB.DockAccuracy(req.Username, req.Role, req.Amount, req.Reason, req.By)
	case "credit":
		err = s.legoDB.CreditAccuracy(req.Username, req.Role, req.Amount, req.Reason, req.By)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `kind must be "dock" or "credit"`})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACCURACY_"+strings.ToUpper(req.Kind), "SUCCESS", req.Username+" "+req.Role)
	writeJSON(w, http.StatusOK, map[string]bool{"applied": true})
}
