// CrowdSec view for the dashboard's Security page: active bans, recent alerts and bouncer
// health, plus ban/unban. It talks to CrowdSec only through `cscli` inside its container
// (no shell, arguments validated), so no extra credential is needed or stored here.
package dashboardapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

func crowdsecContainer() string {
	if v := os.Getenv("CROWDSEC_CONTAINER"); v != "" {
		return v
	}
	return "crowdsec"
}

// cscli runs one cscli command inside the CrowdSec container.
func cscli(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", append([]string{"exec", crowdsecContainer(), "cscli"}, args...)...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("cscli: %v: %s", err, strings.TrimSpace(string(out[:min(len(out), 300)])))
	}
	return out, nil
}

type csDecision struct {
	ID       int64  `json:"id"`
	Value    string `json:"value"`
	Type     string `json:"type"`
	Scenario string `json:"scenario"`
	Origin   string `json:"origin"`
	Duration string `json:"duration"`
}

type csAlert struct {
	ID          int64  `json:"id"`
	CreatedAt   string `json:"created_at"`
	Scenario    string `json:"scenario"`
	EventsCount int    `json:"events_count"`
	Source      struct {
		IP     string `json:"ip"`
		CN     string `json:"cn"`
		ASName string `json:"as_name"`
	} `json:"source"`
}

type csBouncer struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Version  string `json:"version"`
	LastPull string `json:"last_pull"`
	Revoked  bool   `json:"revoked"`
}

// handleCrowdsec is GET /dashboard/security/crowdsec.
func (s *Server) handleCrowdsec(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"decisions": []csDecision{}, "alerts": []csAlert{}, "bouncers": []csBouncer{}}
	var errs []string
	fill := func(key string, dst any, args ...string) {
		out, err := cscli(r.Context(), args...)
		if err != nil {
			errs = append(errs, err.Error())
			return
		}
		if err := json.Unmarshal(out, dst); err != nil {
			errs = append(errs, key+": unreadable output")
			return
		}
		resp[key] = dst
	}
	var d []csDecision
	var a []csAlert
	var b []csBouncer
	fill("decisions", &d, "decisions", "list", "-o", "json")
	fill("alerts", &a, "alerts", "list", "-o", "json", "--limit", "50")
	fill("bouncers", &b, "bouncers", "list", "-o", "json")
	if len(errs) > 0 {
		resp["error"] = strings.Join(errs, "; ")
	}
	writeJSON(w, http.StatusOK, resp)
}

var banDuration = regexp.MustCompile(`^[0-9]{1,4}[mhd]$`)

type crowdsecActionRequest struct {
	IP       string `json:"ip"`
	Duration string `json:"duration"`
	By       string `json:"by"`
}

// handleCrowdsecAction serves POST /dashboard/security/crowdsec/{ban,unban}.
func (s *Server) handleCrowdsecAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req crowdsecActionRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
			return
		}
		ip := net.ParseIP(strings.TrimSpace(req.IP))
		if ip == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not a valid IP address"})
			return
		}
		var args []string
		if action == "unban" {
			args = []string{"decisions", "delete", "--ip", ip.String()}
		} else {
			if !banDuration.MatchString(req.Duration) {
				req.Duration = "4h"
			}
			args = []string{"decisions", "add", "--ip", ip.String(), "--duration", req.Duration, "--reason", "manual ban from dashboard"}
		}
		out, err := cscli(r.Context(), args...)
		s.logAudit(req.By, "DASHBOARD_CROWDSEC_"+strings.ToUpper(action), map[bool]string{true: "SUCCESS", false: "FAILURE"}[err == nil], ip.String())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"result": strings.TrimSpace(string(out))})
	}
}
