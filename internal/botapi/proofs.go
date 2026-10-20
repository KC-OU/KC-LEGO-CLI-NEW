// Paper-copy verification over Discord: a checker attaches a photo/scan of
// the finished paper checklist directly to the slash command that reports
// it, instead of emailing it to an admin or using a separate upload tool.
// Mirrors `wms lego attach` exactly (internal/lego.VerifyProofFile/
// StoreProof/AttachProof, the same attachments directory) -- the one new
// piece is fetching the file from Discord's CDN first, since a Discord
// attachment option only ever gives the bot a URL to it, not the bytes.
package botapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// attachmentsDir mirrors cmd/wms/lego_paper.go's legoAttachmentsDir() --
// duplicated rather than exported across an unrelated package boundary for
// four lines of config lookup, same trade-off internal/dashboardapi's own
// admin.go already made for ticketLabel.
func attachmentsDir() string {
	if dir := config.Get(config.LegoAttachmentsDir); dir != "" {
		return dir
	}
	return filepath.Join(filepath.Dir(config.Get(config.LegoDBPath)), "attachments")
}

// normalizeSetNum mirrors cmd/wms/lego_sets.go's catalogNum -- a bare
// number typed without "-1" defaults to the first variant, the same
// convenience the CLI already gives.
func normalizeSetNum(n string) string {
	n = strings.TrimSpace(n)
	if n != "" && !strings.Contains(n, "-") {
		return n + "-1"
	}
	return n
}

type attachProofParams struct {
	SetNum        string `json:"set_num"`
	Note          string `json:"note"`
	AttachmentURL string `json:"attachment_url"`
}

// maxDownload matches lego.VerifyProofFile's own size ceiling -- no point
// reading further than it will ever accept.
const maxDownload = 25<<20 + 1

// attachProof downloads a Discord-attached photo/scan, verifies it's a
// genuine jpg/png/pdf, and records it against setNum's current (or most
// recent) check -- the exact same effect as `wms lego attach`.
func (s *Server) attachProof(actor string, raw json.RawMessage) (int, map[string]any) {
	var p attachProofParams
	if err := json.Unmarshal(raw, &p); err != nil || p.AttachmentURL == "" {
		return http.StatusBadRequest, map[string]any{"error": "set and photo are required"}
	}
	setNum := normalizeSetNum(p.SetNum)
	if setNum == "" {
		return http.StatusBadRequest, map[string]any{"error": "set is required"}
	}

	c, err := s.legoDB.CurrentOrLastCheck(setNum)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}
	if c == nil {
		return http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("set %s has no check yet -- mark it paper-done or start a check first", setNum)}
	}

	tmp, err := downloadToTemp(p.AttachmentURL)
	if err != nil {
		return http.StatusBadGateway, map[string]any{"error": "could not download the attachment: " + err.Error()}
	}
	defer os.Remove(tmp)

	kind, err := lego.VerifyProofFile(tmp)
	if err != nil {
		return http.StatusBadRequest, map[string]any{"error": err.Error()}
	}
	dst, err := lego.StoreProof(attachmentsDir(), setNum, kind, tmp)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}
	if _, err := s.legoDB.AttachProof(c.ID, setNum, kind, dst, p.Note, actor); err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}
	return http.StatusOK, map[string]any{"set": setNum, "check_id": c.ID, "kind": kind, "stored": dst}
}

// downloadToTemp fetches url into a new temp file, refusing anything past
// maxDownload bytes -- VerifyProofFile re-checks size precisely afterwards,
// this just stops an absurdly large response from being read at all.
func downloadToTemp(url string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("attachment fetch returned %s", resp.Status)
	}

	f, err := os.CreateTemp("", "kc-proof-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(resp.Body, maxDownload)); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
