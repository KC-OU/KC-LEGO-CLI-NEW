// A time-limited, single-use upload link for someone with no Teedy
// account of their own -- offered by the dashboard's onboarding popup
// the moment a normal Teedy upload attempt fails for that reason (see
// the user's own hand-drawn design, page 2: "you will be supplied a
// temporary upload link"). The token is self-verifying (HMAC + expiry,
// no server round-trip needed to check it's well-formed and not
// expired); only "already used" needs server-side state, tracked in the
// same onboarding.json store as everything else in this feature.
package dashboardapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

const guestLinkTTL = 24 * time.Hour

type guestLinkPayload struct {
	SetNum string `json:"set_num"`
	Nonce  string `json:"nonce"`
	Exp    int64  `json:"exp"`
}

func guestLinkSecret() ([]byte, bool) {
	s := config.Get(config.OnboardingGuestLinkSecret)
	return []byte(s), s != ""
}

// signGuestLinkToken builds "<base64url(payload json)>.<hex hmac>".
func signGuestLinkToken(setNum string) (string, error) {
	secret, ok := guestLinkSecret()
	if !ok {
		return "", fmt.Errorf("guest upload links are not configured (set %s)", config.OnboardingGuestLinkSecret)
	}
	nonceBytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nonceBytes); err != nil {
		return "", err
	}
	payload := guestLinkPayload{
		SetNum: setNum,
		Nonce:  hex.EncodeToString(nonceBytes),
		Exp:    time.Now().Add(guestLinkTTL).Unix(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encoded))
	sig := hex.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

// verifyGuestLinkToken checks the signature and expiry, and reports the
// nonce the caller must then check/mark against the "used" list -- this
// function alone does not consume the token.
func verifyGuestLinkToken(token string) (guestLinkPayload, error) {
	secret, ok := guestLinkSecret()
	if !ok {
		return guestLinkPayload{}, fmt.Errorf("guest upload links are not configured")
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return guestLinkPayload{}, fmt.Errorf("malformed token")
	}
	encoded, sig := parts[0], parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encoded))
	want := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(want)) != 1 {
		return guestLinkPayload{}, fmt.Errorf("invalid token")
	}
	body, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return guestLinkPayload{}, fmt.Errorf("malformed token")
	}
	var payload guestLinkPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return guestLinkPayload{}, fmt.Errorf("malformed token")
	}
	if time.Now().Unix() > payload.Exp {
		return guestLinkPayload{}, fmt.Errorf("this link has expired")
	}
	return payload, nil
}

type guestLinkRequest struct {
	SetNum string `json:"set_num"`
	By     string `json:"by"`
}

// handleGenerateGuestUploadLink is POST
// /dashboard/onboarding/guest-link -- any signed-in user, called by the
// onboarding popup the moment a Teedy upload check comes back "not
// found" and the user says they can't upload themselves.
func (s *Server) handleGenerateGuestUploadLink(w http.ResponseWriter, r *http.Request) {
	var req guestLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.SetNum = strings.TrimSpace(req.SetNum)
	req.By = strings.TrimSpace(req.By)
	if req.SetNum == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set_num and by are required"})
		return
	}
	token, err := signGuestLinkToken(req.SetNum)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ONBOARDING_GUEST_LINK_ISSUED", "SUCCESS", req.SetNum)
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_in_seconds": int(guestLinkTTL.Seconds())})
}

func (s *onboardingStore) tokenUsed(nonce string) (bool, error) {
	d, err := s.load()
	if err != nil {
		return false, err
	}
	for _, used := range d.UsedGuestTokens {
		if used == nonce {
			return true, nil
		}
	}
	return false, nil
}

func (s *onboardingStore) markTokenUsed(nonce string) error {
	d, err := s.load()
	if err != nil {
		return err
	}
	d.UsedGuestTokens = append(d.UsedGuestTokens, nonce)
	return s.save(d)
}

// handleGuestUpload is POST /dashboard/onboarding/guest-upload -- still
// behind withToken like every other dashboardapi route (this Go API is
// only ever called by the Rust dashboard server, never directly by a
// browser). The actual anonymous guest-facing form lives on the Rust
// side; the guest-link token in this request's form body is a SEPARATE
// authorization, single-use and time-limited, checked on top of the
// normal server-to-server bearer token. Proxies straight into Teedy
// using this server's own service-account credentials -- a guest never
// sees or needs a real Teedy login.
func (s *Server) handleGuestUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed upload"})
		return
	}
	token := strings.TrimSpace(r.FormValue("token"))
	fullName := strings.TrimSpace(r.FormValue("full_name"))
	userID := strings.TrimSpace(r.FormValue("user_id"))
	date := strings.TrimSpace(r.FormValue("date"))
	if token == "" || fullName == "" || userID == "" || date == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token, full_name, user_id and date are all required"})
		return
	}
	payload, err := verifyGuestLinkToken(token)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	used, err := s.onboarding.tokenUsed(payload.Nonce)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if used {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "this link has already been used"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a file is required"})
		return
	}
	defer file.Close()
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read the uploaded file"})
		return
	}
	// The upload goes into the same verify queue as a signed-in checker's, filed under the ID they typed (never
	// the name: the name is only checked to be present). Teedy is no longer needed for it.
	uid := lego.WorkerID(userID)
	if uid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id must be your ID number"})
		return
	}
	flow, err := s.legoDB.StartFlow(payload.SetNum, uid)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if code, err := s.storeFlowPaper(flow, uid, header.Filename, fileBytes); err != nil {
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	if err := s.onboarding.markTokenUsed(payload.Nonce); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(uid, "DASHBOARD_ONBOARDING_GUEST_UPLOAD", "SUCCESS", payload.SetNum)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
