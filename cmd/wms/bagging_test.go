package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegoBagSizeShowAndSet(t *testing.T) {
	legoEnv(t)

	stdout, code := run(t, "lego", "bag-size", "3024")
	if code != 0 || !strings.Contains(stdout, "currently in the main bag") {
		t.Fatalf("default: code=%d out=%q", code, stdout)
	}

	stdout, code = run(t, "lego", "bag-size", "3024", "small")
	if code != 0 || !strings.Contains(stdout, "now in the small bag") {
		t.Fatalf("set small: code=%d out=%q", code, stdout)
	}
	stdout, _ = run(t, "lego", "bag-size", "3024")
	if !strings.Contains(stdout, "currently in the small bag") {
		t.Errorf("expected the override to stick, got %q", stdout)
	}

	if _, code := run(t, "lego", "bag-size", "3024", "huge"); code != exitUsage {
		t.Errorf("an invalid size = %d, want %d", code, exitUsage)
	}
}

func TestLegoSmallBagLabelWritesAFileAndEncodesTheBagCode(t *testing.T) {
	db := legoEnv(t)
	seedCatalog(t, db)
	out := filepath.Join(t.TempDir(), "bag.pdf")

	stdout, code := run(t, "lego", "small-bag-label", "1-1", "BAG-0042", "-o", out)
	if code != 0 || !strings.Contains(stdout, "Wrote the small-bag label") {
		t.Fatalf("small-bag-label: code=%d out=%q", code, stdout)
	}
	if info, err := os.Stat(out); err != nil || info.Size() == 0 {
		t.Fatalf("expected a non-empty PDF at %s: %v", out, err)
	}

	// --format html so the encoded barcode content is visible as plain text to check.
	stdout, code = run(t, "lego", "small-bag-label", "1-1", "BAG-0042", "--format", "html")
	if code != 0 || !strings.Contains(stdout, "SMALL PARTS") {
		t.Fatalf("expected the label to say SMALL PARTS, code=%d out=%q", code, stdout)
	}
}
