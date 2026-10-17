package lego

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMarkPaperDoneIsResumedByTheSameCheckSession(t *testing.T) {
	d := buildDB(t)
	_ = d.UpsertSet(Set{SetNum: "1", Name: "Fire Station", Qty: 1, PartsQty: 100})
	paperDone, err := d.MarkPaperDone(context.Background(), nil, "1-1", CheckIntake, "kc", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if paperDone.PaperCheckedAt.IsZero() || paperDone.Status != StatusDraft {
		t.Fatalf("paper-done check = %+v", paperDone)
	}
	pending, err := d.PendingPaperChecks()
	if err != nil || len(pending) != 1 || pending[0].SetNum != "1-1" {
		t.Fatalf("pending = %+v, err = %v", pending, err)
	}

	// The CLI's own NewCheck (what `wms lego check` calls) must resume this exact
	// draft, not start a fresh one — paper-done and the eventual CLI entry are one
	// check session throughout.
	resumed, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ID != paperDone.ID || resumed.PaperCheckedAt.IsZero() {
		t.Fatalf("resumed = %+v, want same ID as %+v", resumed, paperDone)
	}

	if _, err := d.FinishCheck(resumed); err != nil {
		t.Fatal(err)
	}
	pending, err = d.PendingPaperChecks()
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after finishing = %+v, err = %v", pending, err)
	}
}

func TestVerifyProofFileAcceptsRealImagesAndPDFsRejectsGarbage(t *testing.T) {
	dir := t.TempDir()

	pngFile := writeTestPNG(t, filepath.Join(dir, "proof.png"))
	if kind, err := VerifyProofFile(pngFile); err != nil || kind != "png" {
		t.Errorf("png: kind=%q err=%v", kind, err)
	}

	pdf := filepath.Join(dir, "proof.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4\n...fake but has the right header...\n%%EOF"), 0o644); err != nil {
		t.Fatal(err)
	}
	if kind, err := VerifyProofFile(pdf); err != nil || kind != "pdf" {
		t.Errorf("pdf: kind=%q err=%v", kind, err)
	}

	renamed := filepath.Join(dir, "not-really-a-photo.jpg")
	if err := os.WriteFile(renamed, []byte("this is just text, not a jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyProofFile(renamed); err == nil {
		t.Error("a renamed text file must not verify as a photo")
	}

	empty := filepath.Join(dir, "empty.png")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyProofFile(empty); err == nil {
		t.Error("an empty file must not verify")
	}

	if _, err := VerifyProofFile(filepath.Join(dir, "missing.png")); err == nil {
		t.Error("a missing file must error")
	}
}

func TestAttachProofAndDisputeAgainstTheCurrentCheck(t *testing.T) {
	d := buildDB(t)
	_ = d.UpsertSet(Set{SetNum: "1", Name: "Fire Station", Qty: 1, PartsQty: 100})
	c, err := d.MarkPaperDone(context.Background(), nil, "1-1", CheckIntake, "kc", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	cur, err := d.CurrentOrLastCheck("1-1")
	if err != nil || cur == nil || cur.ID != c.ID {
		t.Fatalf("CurrentOrLastCheck = %+v, err = %v", cur, err)
	}

	dir := t.TempDir()
	src := writeTestPNG(t, filepath.Join(dir, "scan.png"))
	dst, err := StoreProof(filepath.Join(dir, "attachments"), "1-1", "png", src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("stored proof missing: %v", err)
	}
	if _, err := d.AttachProof(c.ID, "1-1", "png", dst, "looks complete", "kc"); err != nil {
		t.Fatal(err)
	}
	proofs, err := d.ProofsFor(c.ID)
	if err != nil || len(proofs) != 1 || proofs[0].FilePath != dst {
		t.Fatalf("proofs = %+v, err = %v", proofs, err)
	}

	if err := d.FlagDispute(c.ID, "CLI shows 3 missing, paper shows 0"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := d.GetCheck(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Disputed || reloaded.DisputeNote == "" {
		t.Fatalf("reloaded = %+v", reloaded)
	}
}

func TestCurrentOrLastCheckFallsBackToTheMostRecentFinishedCheck(t *testing.T) {
	d := buildDB(t)
	_ = d.UpsertSet(Set{SetNum: "1", Name: "Fire Station", Qty: 1, PartsQty: 100})
	if _, err := d.CurrentOrLastCheck("1-1"); err != nil {
		t.Fatal(err)
	}
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	cur, err := d.CurrentOrLastCheck("1-1")
	if err != nil || cur == nil || cur.ID != c.ID || cur.Status != StatusDone {
		t.Fatalf("cur = %+v, err = %v", cur, err)
	}
}

func writeTestPNG(t *testing.T, path string) string {
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
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
