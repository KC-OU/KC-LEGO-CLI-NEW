package botapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// testPNGServer stands in for Discord's CDN: a plain HTTP server handing
// back a small, genuinely valid PNG, the same shape internal/lego's own
// paper_test.go generates for VerifyProofFile's tests.
func testPNGServer(t *testing.T) *httptest.Server {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 50, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv
}

func withAttachmentsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.LegoAttachmentsDir, dir)
	return dir
}

// seedCatalog gives setNum a minimal offline-catalog entry, mirroring
// internal/lego's own buildDB test helper -- NewCheck needs a parts list to
// build a check against, and with no live Rebrickable client in these
// tests, the offline catalog is the only source for one.
func seedCatalog(t *testing.T, db *lego.DB, setNum string) {
	t.Helper()
	if _, err := db.Exec(`INSERT OR IGNORE INTO cat_themes (id, name, parent_id) VALUES (1, 'Star Wars', 0)`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES (?, 'Falcon', 2017, 1, 1, '')`,
		`INSERT INTO cat_inventories (id, version, set_num) VALUES ((SELECT COALESCE(MAX(id),0)+1 FROM cat_inventories), 1, ?)`,
	} {
		if _, err := db.Exec(q, setNum); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}
	var invID int
	if err := db.QueryRow(`SELECT id FROM cat_inventories WHERE set_num = ?`, setNum).Scan(&invID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (?, '3001', 4, 1)`, invID); err != nil {
		t.Fatal(err)
	}
}

func TestAttachProofStoresTheDownloadedFileAndRecordsIt(t *testing.T) {
	db, auditLog, _ := testEnv(t)
	withAttachmentsDir(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()
	png := testPNGServer(t)

	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Falcon", Qty: 1, PartsQty: 10}); err != nil {
		t.Fatal(err)
	}
	seedCatalog(t, db, "75192-1")
	c, err := db.MarkPaperDone(context.Background(), nil, "75192-1", lego.CheckIntake, "dave", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	params, _ := json.Marshal(attachProofParams{SetNum: "75192-1", Note: "looked clean", AttachmentURL: png.URL + "/proof.png"})
	resp, body := post(t, srv, "/bot/action", map[string]any{
		"platform": "discord", "id": "111222333", "pin": "1234", "action": "attach_proof", "params": json.RawMessage(params),
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %+v", resp.StatusCode, body)
	}
	if body["set"] != "75192-1" {
		t.Fatalf("unexpected response: %+v", body)
	}

	proofs, err := db.ProofsFor(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(proofs) != 1 || proofs[0].Kind != "png" || proofs[0].Note != "looked clean" {
		t.Fatalf("expected one recorded png proof, got %+v", proofs)
	}
	if _, err := os.Stat(proofs[0].FilePath); err != nil {
		t.Fatalf("stored proof file missing on disk: %v", err)
	}
}

func TestAttachProofWithoutAnExistingCheckIsRejected(t *testing.T) {
	db, auditLog, _ := testEnv(t)
	withAttachmentsDir(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()
	png := testPNGServer(t)

	if err := db.UpsertSet(lego.Set{SetNum: "10312-1", Name: "Jazz Club", Qty: 1, PartsQty: 10}); err != nil {
		t.Fatal(err)
	}

	params, _ := json.Marshal(attachProofParams{SetNum: "10312-1", AttachmentURL: png.URL + "/proof.png"})
	resp, _ := post(t, srv, "/bot/action", map[string]any{
		"platform": "discord", "id": "111222333", "pin": "1234", "action": "attach_proof", "params": json.RawMessage(params),
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (no check yet)", resp.StatusCode)
	}
}

func TestAttachProofRejectsSomethingThatIsntARealImageOrPDF(t *testing.T) {
	db, auditLog, _ := testEnv(t)
	withAttachmentsDir(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()

	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("not a real image")) }))
	defer garbage.Close()

	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Falcon", Qty: 1, PartsQty: 10}); err != nil {
		t.Fatal(err)
	}
	seedCatalog(t, db, "75192-1")
	if _, err := db.MarkPaperDone(context.Background(), nil, "75192-1", lego.CheckIntake, "dave", time.Now()); err != nil {
		t.Fatal(err)
	}

	params, _ := json.Marshal(attachProofParams{SetNum: "75192-1", AttachmentURL: garbage.URL + "/fake.png"})
	resp, body := post(t, srv, "/bot/action", map[string]any{
		"platform": "discord", "id": "111222333", "pin": "1234", "action": "attach_proof", "params": json.RawMessage(params),
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %+v, want 400", resp.StatusCode, body)
	}
}

func TestAttachProofRequiresTheAttachmentURL(t *testing.T) {
	db, auditLog, _ := testEnv(t)
	withAttachmentsDir(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()

	params, _ := json.Marshal(attachProofParams{SetNum: "75192-1"})
	resp, _ := post(t, srv, "/bot/action", map[string]any{
		"platform": "discord", "id": "111222333", "pin": "1234", "action": "attach_proof", "params": json.RawMessage(params),
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestNormalizeSetNumDefaultsTheVariantSuffix(t *testing.T) {
	if got := normalizeSetNum("75192"); got != "75192-1" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeSetNum("75192-2"); got != "75192-2" {
		t.Fatalf("got %q", got)
	}
}

// TestDiscordProofSubcommandResolvesTheAttachment exercises the real
// Discord envelope end to end: an ATTACHMENT-type option's value is just a
// snowflake ID, with the actual file metadata living in the interaction's
// top-level data.resolved.attachments -- this is the one part of the proof
// flow that's Discord-specific rather than shared with /bot/action, so it's
// the one worth testing against the real request shape, not just the
// decoded attachProofParams the other tests in this file use.
func TestDiscordProofSubcommandResolvesTheAttachment(t *testing.T) {
	srv, _, pub, priv, db := newDiscordTestServer(t)
	defer srv.Close()
	withAttachmentsDir(t)
	png := testPNGServer(t)

	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Falcon", Qty: 1, PartsQty: 10}); err != nil {
		t.Fatal(err)
	}
	seedCatalog(t, db, "75192-1")
	if _, err := db.MarkPaperDone(context.Background(), nil, "75192-1", lego.CheckIntake, "dave", time.Now()); err != nil {
		t.Fatal(err)
	}

	const attachmentID = "999888777"
	payload := map[string]any{
		"type":   discordTypeApplicationCommand,
		"member": map[string]any{"user": map[string]any{"id": "111222333"}},
		"data": map[string]any{
			"name": "wms",
			"options": []map[string]any{{
				"name": "proof",
				"options": []map[string]any{
					{"name": "pin", "value": "1234"},
					{"name": "set", "value": "75192-1"},
					{"name": "photo", "value": attachmentID},
					{"name": "note", "value": "looked clean"},
				},
			}},
			"resolved": map[string]any{
				"attachments": map[string]any{
					attachmentID: map[string]any{"id": attachmentID, "filename": "proof.png", "url": png.URL + "/proof.png"},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	req := signedDiscordRequest(t, srv.URL+"/discord/interactions", pub, priv, body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	data, _ := out["data"].(map[string]any)
	content, _ := data["content"].(string)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(content, "✅ proof attached") {
		t.Fatalf("status = %d, content = %q, want a success reply", resp.StatusCode, content)
	}
}
