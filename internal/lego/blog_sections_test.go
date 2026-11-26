package lego

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

func TestLintPostMirrorsTheBlogLintRules(t *testing.T) {
	errs, warns := LintPost("Hello [[wiki]] TODO\n> [!note] x\n![a](https://x.com/a.png) 50% %%c%% [l](javascript:alert(1)) [m](other.md)\n| a | b |\nmail me@x.org")
	joined := strings.Join(errs, "|")
	for _, want := range []string{"TODO", "wikilink", "callout", "our own", "%%comment%%", "unsafe link", ".md file"} {
		if !strings.Contains(joined, want) {
			t.Errorf("lint errors miss %q: %v", want, errs)
		}
	}
	if len(warns) < 2 {
		t.Errorf("a table and an email should warn, got %v", warns)
	}
	if e, w := LintPost("Plain text with a [link](https://example.com) and **bold**."); len(e) != 0 || len(w) != 0 {
		t.Errorf("clean text flagged: %v %v", e, w)
	}
}

func TestBagSlotsNeedEveryBagAndTheFinishedSet(t *testing.T) {
	db := newTestDB(t)
	for _, n := range []string{"blog:Bag 1", "blog:bag 2", "blog:Finished set", "private note"} {
		if _, err := db.AttachSetPhoto("21027-1", "jpeg", "x.jpg", n, "t"); err != nil {
			t.Fatal(err)
		}
	}
	have, missing := db.BagSlots("21027-1", 3)
	if strings.Join(have, ",") != "Bag 1,Bag 2,Finished set" || strings.Join(missing, ",") != "Bag 3" {
		t.Errorf("have %v missing %v", have, missing)
	}
}

func exifJPEG(taken string) []byte {
	tiff := make([]byte, 0, 80)
	tiff = append(tiff, 'I', 'I', 42, 0, 8, 0, 0, 0) // header, IFD0 at 8
	tiff = binary.LittleEndian.AppendUint16(tiff, 1) // one IFD0 entry: ExifIFD pointer
	tiff = binary.LittleEndian.AppendUint16(tiff, 0x8769)
	tiff = binary.LittleEndian.AppendUint16(tiff, 4)
	tiff = binary.LittleEndian.AppendUint32(tiff, 1)
	tiff = binary.LittleEndian.AppendUint32(tiff, 26) // sub IFD at 26
	tiff = binary.LittleEndian.AppendUint32(tiff, 0)  // next IFD
	tiff = binary.LittleEndian.AppendUint16(tiff, 1)  // sub IFD: DateTimeOriginal
	tiff = binary.LittleEndian.AppendUint16(tiff, 0x9003)
	tiff = binary.LittleEndian.AppendUint16(tiff, 2)
	tiff = binary.LittleEndian.AppendUint32(tiff, 20)
	tiff = binary.LittleEndian.AppendUint32(tiff, 44) // the string at 44
	tiff = binary.LittleEndian.AppendUint32(tiff, 0)
	tiff = append(tiff, []byte(taken+"\x00")...)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(seg)+2))
	out = append(out, seg...)
	return append(out, 0xFF, 0xDA, 0, 2)
}

func TestJPEGTakenAtReadsTheCaptureTime(t *testing.T) {
	at, ok := jpegTakenAt(exifJPEG("2026:11:24 10:30:05"))
	if !ok || at.Format("2006-01-02 15:04:05") != "2026-11-24 10:30:05" {
		t.Fatalf("taken = %v ok=%v", at, ok)
	}
	if _, ok := jpegTakenAt([]byte{0xFF, 0xD8, 0xFF, 0xDA, 0, 2}); ok {
		t.Error("a JPEG with no EXIF has no capture time")
	}
	if _, ok := jpegTakenAt([]byte("not a jpeg")); ok {
		t.Error("not a JPEG")
	}
}

func TestBlogSectionsFlowReadinessPullAndNotes(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.SavePost(Post{Slug: "21027-1", SetNum: "21027-1", Title: "Berlin", Body: "Fine text."}, "4821", "web", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.SetBlogWriter("21027-1", "4821"); err != nil {
		t.Fatal(err)
	}
	if db.PostReady("21027-1") {
		t.Fatal("a new post is not ready")
	}
	if _, err := db.PullCheck("21027-1"); err == nil {
		t.Error("pulling before the check is finished must fail")
	}
	finishedCheck(t, db, "21027-1", 4, 0)
	snap, err := db.PullCheck("21027-1")
	if err != nil || snap.Pieces != 4 || snap.Missing != 0 {
		t.Fatalf("snapshot %+v err %v", snap, err)
	}
	if err := db.SaveFacts("21027-1", BlogFacts{Name: "Other name"}, true); err == nil {
		t.Error("a corrected fact needs a reason")
	}
	if err := db.SaveFacts("21027-1", BlogFacts{Name: "Other name", Note: "catalog is wrong"}, true); err != nil {
		t.Fatal(err)
	}
	_ = db.SetBuild("21027-1", 1, true)
	_ = db.AddSectionFile("21027-1", "sheet", BlogFile{Name: "s.html", Kind: "sheet"})
	_ = db.AddSectionFile("21027-1", "instr", BlogFile{Name: "i.pdf", Kind: "pdf"})
	if db.PostReady("21027-1") {
		t.Fatal("still no photos")
	}
	for _, c := range []string{"blog:Bag 1", "blog:Finished set"} {
		_, _ = db.AttachSetPhoto("21027-1", "jpeg", "x.jpg", c, "4821")
	}
	if !db.PostReady("21027-1") {
		t.Fatalf("everything is in place: %+v", db.ReadyChecks("21027-1", ""))
	}
	time.Sleep(1100 * time.Millisecond)
	finishedCheck(t, db, "21027-1", 3, 0) // checked again
	if !db.NewerCheck("21027-1") {
		t.Error("a newer check should be reported")
	}
	if err := db.AddBlogNote("21027-1", "boss", "changes", " "); err == nil {
		t.Error("an empty note is refused")
	}
	_ = db.AddBlogNote("21027-1", "boss", "changes", "Add a clearer photo of bag 1")
	if n := db.BlogNotes("21027-1"); len(n) != 1 || n[0].Kind != "changes" {
		t.Errorf("notes %+v", n)
	}
}

func TestChangesRequestedStateAndGitSyncWaitsForTheSections(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.SavePost(Post{Slug: "21027-1", SetNum: "21027-1", Title: "Berlin", Body: "x"}, "4821", "web", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPostState("21027-1", PostReview, "4821"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPostState("21027-1", PostChanges, "boss"); err != nil {
		t.Fatalf("review -> changes: %v", err)
	}
	if err := db.SetPostState("21027-1", PostPublished, "boss"); err == nil {
		t.Error("changes cannot jump to published")
	}
	if err := db.SetPostState("21027-1", PostReview, "4821"); err != nil {
		t.Fatalf("changes -> review: %v", err)
	}
	// a git push for a post that has sections but is not ready waits as a draft
	_ = db.EnsureSections("21027-1", "4821")
	p, err := db.SavePost(Post{Slug: "21027-1", SetNum: "21027-1", Title: "Berlin", Body: "pushed text"}, "git", "git", "h1")
	if err != nil || p.State != PostDraft {
		t.Fatalf("pushed post state = %v err %v, want draft until the sections are done", p, err)
	}
	// a post without sections still goes straight to review, as before
	p2, _ := db.SavePost(Post{Slug: "plain", Title: "Plain", Body: "x"}, "git", "git", "h2")
	if p2.State != PostReview {
		t.Errorf("a post without sections should still queue for review, got %s", p2.State)
	}
}
