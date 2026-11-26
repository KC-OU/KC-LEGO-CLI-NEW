package publicblog

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func setup(t *testing.T) (*lego.DB, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "lego.db")
	d, err := lego.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ro.Close(); d.Close() })
	att := filepath.Join(dir, "att")
	os.MkdirAll(att, 0o755)
	return d, Handler(Config{DB: ro, AttachmentsDir: att, AudioDir: filepath.Join(dir, "audio"), BaseURL: "https://sets.example", PerMinute: 1000}), att
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func TestOnlyPublishedPostsAreVisible(t *testing.T) {
	d, h, _ := setup(t)
	p, err := d.SavePost(lego.Post{Slug: "21027-1", SetNum: "21027-1", Theme: "Architecture", Title: "Berlin", Summary: "A skyline", Body: "Hello <script>alert(1)</script> [x](javascript:alert(1)) **bold**"}, "kc", "web", "")
	if err != nil {
		t.Fatal(err)
	}
	if get(h, "/set/21027-1").Code != 404 {
		t.Fatal("a draft must not be public")
	}
	if strings.Contains(get(h, "/").Body.String(), "Berlin") {
		t.Fatal("a draft must not be on the index")
	}
	pv := get(h, "/preview/"+p.PreviewToken)
	if pv.Code != 200 || !strings.Contains(pv.Body.String(), "Preview") || pv.Header().Get("X-Robots-Tag") == "" {
		t.Fatalf("preview should work and be noindex: %d", pv.Code)
	}
	if get(h, "/preview/short").Code != 404 {
		t.Fatal("bad token")
	}
	for _, step := range []string{lego.PostReview, lego.PostPublished} {
		if err := d.SetPostState("21027-1", step, "admin"); err != nil {
			t.Fatal(err)
		}
	}
	w := get(h, "/set/21027-1")
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Berlin") || !strings.Contains(body, "<strong>bold</strong>") {
		t.Fatalf("published post should render: %d", w.Code)
	}
	if strings.Contains(body, "<script>alert") || strings.Contains(body, "javascript:") {
		t.Fatal("raw HTML and javascript: links must not survive")
	}
	if !strings.Contains(get(h, "/").Body.String(), "Berlin") || !strings.Contains(get(h, "/feed.xml").Body.String(), "/set/21027-1") ||
		!strings.Contains(get(h, "/search?q=berl").Body.String(), "Berlin") {
		t.Fatal("published post should be in index, feed and search")
	}
	// Editing a published post pulls it back to draft until re-approved.
	if _, err := d.SavePost(lego.Post{Slug: "21027-1", SetNum: "21027-1", Title: "Berlin v2"}, "kc", "web", ""); err != nil {
		t.Fatal(err)
	}
	if get(h, "/set/21027-1").Code != 404 {
		t.Fatal("an edited post must leave the public site until approved")
	}
}

func TestPublicPhotosAndCheckSummaryOnly(t *testing.T) {
	d, h, att := setup(t)
	d.SavePost(lego.Post{Slug: "10001-1", SetNum: "10001-1", Title: "Test set"}, "kc", "web", "")
	d.SetPostState("10001-1", lego.PostReview, "a")
	d.SetPostState("10001-1", lego.PostPublished, "a")
	os.WriteFile(filepath.Join(att, "pub.jpg"), jpegWithGPS(t), 0o644)
	os.WriteFile(filepath.Join(att, "priv.jpg"), jpegWithGPS(t), 0o644)
	pub, _ := d.AttachSetPhoto("10001-1", "jpg", filepath.Join(att, "pub.jpg"), "blog:Bag 1", "kc")
	priv, _ := d.AttachSetPhoto("10001-1", "jpg", filepath.Join(att, "priv.jpg"), "damaged corner", "kc")
	if get(h, "/media/"+itoa(pub.ID)).Code != 200 {
		t.Fatal("a blog: photo of a published set should be served")
	}
	if b := get(h, "/media/"+itoa(pub.ID)).Body.Bytes(); bytes.Contains(b, []byte("Exif")) || bytes.Contains(b, []byte("SECRETGPS")) {
		t.Fatal("camera metadata must be stripped from public photos")
	}
	if get(h, "/media/"+itoa(priv.ID)).Code != 404 {
		t.Fatal("a photo not marked blog: must stay private")
	}
	if !strings.Contains(get(h, "/set/10001-1").Body.String(), "Bag 1") {
		t.Fatal("caption should show")
	}
	if strings.Contains(get(h, "/set/10001-1").Body.String(), "damaged corner") {
		t.Fatal("private notes must not appear")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestMarkdownEscapesHTML(t *testing.T) {
	out := string(Markdown("# T\n\n<img src=x onerror=1> ![a](data:x) - not list\n\n- a\n- b\n"))
	if strings.Contains(out, "<img src=x") || strings.Contains(out, "data:x") || !strings.Contains(out, "<ul><li>a</li><li>b</li></ul>") {
		t.Fatal(out)
	}
}

// jpegWithGPS is a tiny real JPEG with an EXIF segment (orientation plus a marker string) spliced in.
func jpegWithGPS(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 8, 6)), nil); err != nil {
		t.Fatal(err)
	}
	exif := append([]byte("Exif\x00\x00MM\x00\x2A\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x06\x00\x00\x00\x00\x00\x00"), "SECRETGPS"...)
	seg := append([]byte{0xFF, 0xE1, byte((len(exif) + 2) >> 8), byte(len(exif) + 2)}, exif...)
	raw := b.Bytes()
	return append(append(append([]byte{}, raw[:2]...), seg...), raw[2:]...)
}

func TestPrivacyRules(t *testing.T) {
	d, h, _ := setup(t)
	d.SavePost(lego.Post{Slug: "a-1", SetNum: "1-1", Title: "A", Audio: "https://youtu.be/abcdefghijk", Body: "![x](https://evil.example/t.png)"}, "kc", "web", "")
	d.SetPostState("a-1", lego.PostReview, "a")
	d.SetPostState("a-1", lego.PostPublished, "a")
	page := get(h, "/set/a-1").Body.String()
	if strings.Contains(page, "evil.example") || strings.Contains(page, "youtube-nocookie") {
		t.Fatal("no third party may load until the visitor chooses")
	}
	if !strings.Contains(get(h, "/set/a-1?video=1").Body.String(), "youtube-nocookie.com/embed/abcdefghijk") {
		t.Fatal("video should load after the visitor clicks")
	}
	if get(h, "/privacy").Code != 200 {
		t.Fatal("privacy page missing")
	}
}

func TestAboutPageAndStaffLink(t *testing.T) {
	_, h, _ := setup(t)
	b := get(h, "/about").Body.String()
	if !strings.Contains(b, "Nothing goes live until an admin approves it") {
		t.Fatal("about page should explain the review step")
	}
	if strings.Contains(b, "<form") && strings.Contains(b, "password") {
		t.Fatal("the public site must not have a login form")
	}
}

func TestPostShowsCheckTotalsBorrowedSheetAndNothingPrivate(t *testing.T) {
	d, h, att := setup(t)
	p, err := d.SavePost(lego.Post{Slug: "21027-1", SetNum: "21027-1", Title: "Berlin", Body: "Fine text."}, "4821", "web", "")
	if err != nil {
		t.Fatal(err)
	}
	c := &lego.SetCheck{SetNum: "21027-1", Kind: lego.CheckIntake, Status: lego.StatusDraft, CheckedBy: "4821", StartedAt: time.Now(),
		Lines: []lego.CheckLine{{PartNum: "3001", PartName: "Brick 2 x 4", ColorID: 4, ColorName: "Red", Need: 4, Have: 2}}}
	if _, err := d.AddBorrowedForCheck(c, lego.BorrowedPart{NeedPartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 2, FromSetNum: "10696-1", FromSetName: "Medium Brick Box", Note: "internal note", CreatedBy: "4821"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	if err := d.EnsureSections("21027-1", "4821"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.PullCheck("21027-1"); err != nil {
		t.Fatal(err)
	}
	// a paper sheet photo and a digital sheet with IDs and dates on it
	if err := os.WriteFile(filepath.Join(att, "sheet.png"), tinyPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = d.AddSectionFile("21027-1", "sheet", lego.BlogFile{Name: "sheet.png", Kind: "png"})
	_ = d.AddSectionFile("21027-1", "instr", lego.BlogFile{Name: "book.pdf", Kind: "pdf"})
	_ = d.SetSheetPublic("21027-1", d.MarksFromSheet("21027-1", `{"set":"21027-1","lines":[{"p":"3001","c":4,"s":2,"m":"2","x":"","i":"4821","k":"9999","d":"24/11/26"}]}`))
	_ = d.SetBuild("21027-1", 3, true)
	_ = d.SetPostState("21027-1", lego.PostReview, "4821")
	_ = d.SetPostState("21027-1", lego.PostPublished, "boss")

	body := get(h, "/set/21027-1").Body.String()
	for _, want := range []string{"Parts checked", "<td>4</td>", "Borrowed parts", "2 x 3001", "from set 10696-1", "Medium Brick Box", "Our paper check", "/sheet/21027-1/0", "line by line", "3 numbered bags", "buildinginstructions/21027"} {
		if !strings.Contains(body, want) {
			t.Errorf("the post page is missing %q", want)
		}
	}
	for _, never := range []string{"internal note", "4821", "9999", "24/11/26", "book.pdf", "blog-21027"} {
		if strings.Contains(body, never) {
			t.Errorf("the public page must never show %q", never)
		}
	}
	if w := get(h, "/sheet/21027-1/0"); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/png") {
		t.Errorf("sheet image = %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if get(h, "/sheet/21027-1/1").Code != 404 {
		t.Error("only the sheet's own images are served")
	}
	_ = d.SetPostState("21027-1", lego.PostHidden, "boss")
	if get(h, "/sheet/21027-1/0").Code != 404 {
		t.Error("a hidden post's sheet must not be served")
	}
	if w := get(h, "/sheet/21027-1/0?t="+p.PreviewToken); w.Code != 200 {
		t.Errorf("the preview token should show the sheet, got %d", w.Code)
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
