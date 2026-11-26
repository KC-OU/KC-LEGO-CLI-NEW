package lego

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncPostsDirGoesToReviewAndSkipsUnchanged(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "21027-1.md"), []byte("---\ntitle: Berlin\ntheme: Architecture\nset: 21027-1\n---\nHello"), 0o644)
	os.WriteFile(filepath.Join(dir, "bad.md"), []byte("no title here"), 0o644)
	os.MkdirAll(filepath.Join(dir, "audio"), 0o755)
	os.WriteFile(filepath.Join(dir, "audio", "a.mp3"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "audio", "evil.sh"), []byte("x"), 0o644)
	audio := filepath.Join(t.TempDir(), "aud")
	out, _ := d.SyncPostsDir(dir, audio, "git")
	p, _ := d.GetPost("21027-1")
	if p == nil || p.State != PostReview || p.Body != "Hello" || len(out) != 2 {
		t.Fatalf("post=%+v out=%v", p, out)
	}
	if _, err := os.Stat(filepath.Join(audio, "a.mp3")); err != nil {
		t.Fatal("audio not copied")
	}
	if _, err := os.Stat(filepath.Join(audio, "evil.sh")); err == nil {
		t.Fatal("only audio files may be copied")
	}
	d.SetPostState("21027-1", PostPublished, "a")
	if out, _ := d.SyncPostsDir(dir, "", "git"); len(out) != 1 { // unchanged file skipped; bad.md reported again
		t.Fatalf("unchanged file should be skipped: %v", out)
	}
	if p, _ := d.GetPost("21027-1"); p.State != PostPublished {
		t.Fatal("an unchanged sync must not unpublish")
	}
	os.WriteFile(filepath.Join(dir, "21027-1.md"), []byte("---\ntitle: Berlin\n---\nChanged"), 0o644)
	d.SyncPostsDir(dir, "", "git")
	if p, _ := d.GetPost("21027-1"); p.State != PostReview {
		t.Fatalf("a changed file must go back to review, got %s", p.State)
	}
}

func TestHideAndShowKeepsPublishDate(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.SavePost(Post{Slug: "a-1", Title: "A"}, "kc", "web", ""); err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{PostReview, PostPublished} {
		if err := d.SetPostState("a-1", to, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := d.GetPost("a-1")
	if err := d.SetPostState("a-1", PostHidden, "admin"); err != nil {
		t.Fatal(err)
	}
	if p, _ := d.GetPost("a-1"); p.State != PostHidden || p.PublishedAt != first.PublishedAt {
		t.Fatalf("hidden should keep the date: %+v", p)
	}
	if err := d.SetPostState("a-1", PostPublished, "admin"); err != nil {
		t.Fatal(err)
	}
	if p, _ := d.GetPost("a-1"); p.State != PostPublished || p.PublishedAt != first.PublishedAt {
		t.Fatalf("show should restore it as it was: %+v", p)
	}
	if err := d.SetPostState("a-1", PostHidden, "admin"); err != nil {
		t.Fatal(err)
	}
	d.SavePost(Post{Slug: "a-1", Title: "A2"}, "kc", "web", "")
	if p, _ := d.GetPost("a-1"); p.State != PostDraft {
		t.Fatalf("editing a hidden post needs approval again: %s", p.State)
	}
}
