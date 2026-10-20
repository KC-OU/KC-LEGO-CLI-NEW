package dashboardapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// seedCatalog mirrors internal/botapi/proofs_test.go's own helper of the
// same name -- NewCheck (which MarkPaperDone calls) needs a parts list to
// build a check against, and there's no live Rebrickable client in these
// tests, so the offline catalog is the only source for one.
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

func withAttachmentsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.LegoAttachmentsDir, dir)
	return dir
}

func TestProofsForSetReturnsTheCheckAndItsAttachedProofs(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	dir := withAttachmentsDir(t)

	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Falcon", Qty: 1, PartsQty: 10}); err != nil {
		t.Fatal(err)
	}
	seedCatalog(t, db, "75192-1")
	c, err := db.MarkPaperDone(context.Background(), nil, "75192-1", lego.CheckIntake, "dave", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src.png")
	if err := os.WriteFile(src, []byte("not-really-a-png-but-fine-for-this-test"), 0o644); err != nil {
		t.Fatal(err)
	}
	stored, err := lego.StoreProof(dir, "75192-1", "png", src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AttachProof(c.ID, "75192-1", "png", stored, "looked clean", "dave"); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/admin/proofs/75192-1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var body struct {
		CheckID  int64      `json:"check_id"`
		Disputed bool       `json:"disputed"`
		Proofs   []proofRow `json:"proofs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.CheckID != c.ID || body.Disputed {
		t.Fatalf("unexpected check info: %+v", body)
	}
	if len(body.Proofs) != 1 || body.Proofs[0].Note != "looked clean" {
		t.Fatalf("expected one recorded proof, got %+v", body.Proofs)
	}
}

func TestProofsForSetWithoutAnExistingCheckIs404(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	withAttachmentsDir(t)

	if err := db.UpsertSet(lego.Set{SetNum: "10312-1", Name: "Jazz Club", Qty: 1, PartsQty: 10}); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/admin/proofs/10312-1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestProofFileServesTheAttachedBytes(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	dir := withAttachmentsDir(t)

	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Falcon", Qty: 1, PartsQty: 10}); err != nil {
		t.Fatal(err)
	}
	seedCatalog(t, db, "75192-1")
	c, err := db.MarkPaperDone(context.Background(), nil, "75192-1", lego.CheckIntake, "dave", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src.png")
	if err := os.WriteFile(src, []byte("the-actual-photo-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	stored, err := lego.StoreProof(dir, "75192-1", "png", src)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.AttachProof(c.ID, "75192-1", "png", stored, "", "dave")
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/admin/proofs/75192-1/"+strconv.FormatInt(p.ID, 10)+"/file", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if string(buf[:n]) != "the-actual-photo-bytes" {
		t.Fatalf("unexpected body: %q", buf[:n])
	}
}

func TestProofFileForAnUnknownProofIdIs404(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	withAttachmentsDir(t)

	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Falcon", Qty: 1, PartsQty: 10}); err != nil {
		t.Fatal(err)
	}
	seedCatalog(t, db, "75192-1")
	if _, err := db.MarkPaperDone(context.Background(), nil, "75192-1", lego.CheckIntake, "dave", time.Now()); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/admin/proofs/75192-1/999/file", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestFlagDisputeMarksTheCheckDisputed(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	withAttachmentsDir(t)

	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Falcon", Qty: 1, PartsQty: 10}); err != nil {
		t.Fatal(err)
	}
	seedCatalog(t, db, "75192-1")
	c, err := db.MarkPaperDone(context.Background(), nil, "75192-1", lego.CheckIntake, "dave", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	body := `{"set_num":"75192-1","note":"photo doesn't match the count","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/proofs/flag", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	check, err := db.CurrentOrLastCheck("75192-1")
	if err != nil {
		t.Fatal(err)
	}
	if check == nil || check.ID != c.ID || !check.Disputed || check.DisputeNote != "photo doesn't match the count" {
		t.Fatalf("expected the check to be disputed with the given note, got %+v", check)
	}
}

func TestFlagDisputeRequiresSetNumAndBy(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"note":"missing proof"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/proofs/flag", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestFlagDisputeOnASetWithNoCheckIs404(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"set_num":"00000-1","note":"x","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/proofs/flag", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestProofRoutesRequireTheSharedToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/dashboard/admin/proofs/75192-1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	resp, err = http.Post(srv.URL+"/dashboard/admin/proofs/flag", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
