// Package authbridge calls kc-parts-auth-node's internal provisioning
// endpoint to create a matching BetterAuth login for a Go-side
// Picker/Checker/Admin account that has opted into the "dashboard" access
// channel (see internal/access.User.Dashboard). Both the CLI
// (`wms access user dashboard`) and internal/dashboardapi call this, so
// there is exactly one implementation of the actual HTTP hand-off.
//
// This is a one-time credential hand-off, not a live bridge: it sets the
// new BetterAuth account's password to whatever plaintext temp
// password/PIN the Go side just generated, once. A later password change
// on either side does not propagate to the other.
package authbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// Result mirrors kc-parts-auth-node's POST /internal/provision-dashboard-user
// response on success (HTTP 200, whether or not a new account was actually
// created -- an already-provisioned username is reported via Reason, not
// an error).
type Result struct {
	Provisioned bool
	UserID      string
	Reason      string
}

type apiResponse struct {
	Provisioned bool   `json:"provisioned"`
	UserID      string `json:"userId,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Configured reports whether both AuthServiceURL and DashboardProvisionToken
// are set -- callers use this to decide whether to even offer the "also
// sign into the dashboard" option, rather than let Provision fail later.
func Configured() bool {
	return config.Get(config.AuthServiceURL) != "" && config.Get(config.DashboardProvisionToken) != ""
}

// Provision asks kc-parts-auth-node to create (or report already-exists
// for) a BetterAuth account for username/email/password. email may be ""
// -- kc-parts-auth-node synthesizes a placeholder the same way it already
// does for a Discord sign-in with no public email.
func Provision(username, email, password string) (Result, error) {
	base := config.Get(config.AuthServiceURL)
	token := config.Get(config.DashboardProvisionToken)
	if base == "" || token == "" {
		return Result{}, fmt.Errorf("dashboard login provisioning is not configured (set %s and %s)", config.AuthServiceURL, config.DashboardProvisionToken)
	}
	body, err := json.Marshal(map[string]string{"username": username, "email": email, "password": password})
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequest(http.MethodPost, base+"/internal/provision-dashboard-user", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Provision-Token", token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("could not reach the auth service: %w", err)
	}
	defer resp.Body.Close()

	var out apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("malformed response from the auth service: %w", err)
	}
	if resp.StatusCode >= 300 {
		if out.Error != "" {
			return Result{}, fmt.Errorf("%s", out.Error)
		}
		return Result{}, fmt.Errorf("provisioning failed (%d)", resp.StatusCode)
	}
	return Result{Provisioned: out.Provisioned, UserID: out.UserID, Reason: out.Reason}, nil
}
