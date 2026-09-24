package lego

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveReportRoundTrips(t *testing.T) {
	d := openScratchDB(t)
	dir := t.TempDir()
	id, err := d.ArchiveReport(dir, "missing-report", "Missing parts", "kc", "html", []byte("<html>hi</html>"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := d.GetArchiveEntry(id)
	if err != nil || e == nil {
		t.Fatalf("GetArchiveEntry: %v %v", e, err)
	}
	if e.Kind != "missing-report" || e.Title != "Missing parts" || e.CreatedBy != "kc" {
		t.Errorf("entry = %+v", e)
	}
	body, err := os.ReadFile(filepath.Join(dir, e.File))
	if err != nil || string(body) != "<html>hi</html>" {
		t.Fatalf("archived file: %v %q", err, body)
	}
}

func TestListArchiveFiltersByCreatorUnlessAll(t *testing.T) {
	d := openScratchDB(t)
	dir := t.TempDir()
	if _, err := d.ArchiveReport(dir, "missing-report", "A", "alex", "html", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ArchiveReport(dir, "missing-report", "B", "sam", "html", []byte("b")); err != nil {
		t.Fatal(err)
	}
	mine, err := d.ListArchive("alex", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 || mine[0].CreatedBy != "alex" {
		t.Fatalf("mine = %+v", mine)
	}
	all, err := d.ListArchive("", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("all = %+v", all)
	}
	// newest first
	if all[0].Title != "B" {
		t.Errorf("expected newest (B) first, got %+v", all)
	}
}

func TestCleanupArchiveDeletesOldEntriesAndFiles(t *testing.T) {
	d := openScratchDB(t)
	dir := t.TempDir()
	id, err := d.ArchiveReport(dir, "missing-report", "Old", "alex", "html", []byte("old"))
	if err != nil {
		t.Fatal(err)
	}
	// backdate it past the 90-day cutoff
	old := time.Now().Add(-100 * 24 * time.Hour).Format(timeLayout)
	if _, err := d.Exec(`UPDATE report_archive SET created_at = ? WHERE id = ?`, old, id); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ArchiveReport(dir, "missing-report", "Recent", "alex", "html", []byte("recent")); err != nil {
		t.Fatal(err)
	}
	n, err := d.CleanupArchive(dir, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cleaned %d, want 1", n)
	}
	if e, _ := d.GetArchiveEntry(id); e != nil {
		t.Error("the old entry's DB row should be gone")
	}
	remaining, err := d.ListArchive("alex", false)
	if err != nil || len(remaining) != 1 || remaining[0].Title != "Recent" {
		t.Fatalf("remaining = %+v, %v", remaining, err)
	}
}
