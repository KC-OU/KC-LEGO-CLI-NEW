package lego

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAttachSetPhotoListAndRemove is the direct test for a set's own photo
// gallery -- the box, the built model -- independent of any check, any
// number of photos per set, reusing VerifyProofFile/StoreProof the same way
// check proofs do.
func TestAttachSetPhotoListAndRemove(t *testing.T) {
	d := buildDB(t)
	dir := t.TempDir()

	// Separate StoreProof destination dirs, not just different source names:
	// its stored filename is setNum + a one-second-resolution timestamp, so
	// two calls for the same set within the same second would otherwise
	// collide on O_EXCL.
	src1 := writeTestPNG(t, filepath.Join(dir, "box.png"))
	dst1, err := StoreProof(filepath.Join(dir, "attachments1"), "75192-1", "png", src1)
	if err != nil {
		t.Fatal(err)
	}
	p1, err := d.AttachSetPhoto("75192-1", "png", dst1, "the box", "kc")
	if err != nil {
		t.Fatal(err)
	}

	src2 := writeTestPNG(t, filepath.Join(dir, "built.png"))
	dst2, err := StoreProof(filepath.Join(dir, "attachments2"), "75192-1", "png", src2)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := d.AttachSetPhoto("75192-1", "png", dst2, "finished build", "kc")
	if err != nil {
		t.Fatal(err)
	}

	photos, err := d.SetPhotos("75192-1")
	if err != nil || len(photos) != 2 {
		t.Fatalf("SetPhotos = %+v, err = %v, want 2", photos, err)
	}
	if photos[0].ID != p1.ID || photos[0].Note != "the box" {
		t.Errorf("first photo = %+v, want p1 (oldest first)", photos[0])
	}
	if photos[1].ID != p2.ID || photos[1].Note != "finished build" {
		t.Errorf("second photo = %+v, want p2", photos[1])
	}

	// A different set's photos are untouched.
	if other, err := d.SetPhotos("10696-1"); err != nil || len(other) != 0 {
		t.Fatalf("SetPhotos for an unrelated set = %+v, err = %v, want empty", other, err)
	}

	if err := d.RemoveSetPhoto(p1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst1); err == nil {
		t.Error("removing a photo should also remove its stored file")
	}
	photos, err = d.SetPhotos("75192-1")
	if err != nil || len(photos) != 1 || photos[0].ID != p2.ID {
		t.Fatalf("SetPhotos after removing one = %+v, err = %v, want just p2", photos, err)
	}

	if err := d.RemoveSetPhoto(99999); err == nil {
		t.Error("expected an error removing an unknown photo ID")
	}
}
