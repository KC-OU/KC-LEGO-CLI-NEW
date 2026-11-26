package lego

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestXLSXWorkbook(t *testing.T) {
	d := &ExportData{Title: "t", When: time.Now(),
		Facts:   [][2]string{{"Name", "Brick <2x4> & \"co\""}},
		Picture: &Image{URL: "https://cdn.rebrickable.com/x.png"},
		Rows: []ExportRow{
			{PartNum: "3001", Name: "=HYPERLINK(\"evil\")", ColorID: 4, ColorName: "Red", Qty: 7, Image: &Image{URL: "https://cdn.rebrickable.com/media/parts/ldraw/4/3001.png"}},
			{PartNum: "3002", ColorID: -1, Qty: 1, Image: &Image{URL: "javascript:alert(1)"}},
		}}
	b, err := XLSX(d)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		body, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(body)
	}
	for _, want := range []string{"[Content_Types].xml", "xl/workbook.xml", "xl/styles.xml", "xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml"} {
		if _, ok := files[want]; !ok {
			t.Fatalf("missing %s", want)
		}
	}
	details, parts := files["xl/worksheets/sheet1.xml"], files["xl/worksheets/sheet2.xml"]
	if !strings.Contains(details, "Brick &lt;2x4&gt; &amp; &#34;co&#34;") {
		t.Errorf("text not escaped: %s", details)
	}
	if !strings.Contains(parts, `HYPERLINK(&#34;https://cdn.rebrickable.com/media/parts/ldraw/4/3001.png&#34;,&#34;view&#34;)`) {
		t.Errorf("picture link missing: %s", parts)
	}
	if strings.Contains(parts, "javascript:") {
		t.Error("a non-https picture URL became a link")
	}
	// User text is an inline string, never a formula.
	if strings.Contains(parts, `<f>=HYPERLINK`) || !strings.Contains(parts, `t="inlineStr"><is><t xml:space="preserve">=HYPERLINK`) {
		t.Errorf("user text must stay an inline string: %s", parts)
	}
	if colName(0) != "A" || colName(25) != "Z" || colName(26) != "AA" {
		t.Error("colName")
	}
}

func TestJSONEmbedsPictures(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nrest")
	d := &ExportData{When: time.Now(), Rows: []ExportRow{{PartNum: "3001", ColorID: 4, Qty: 1}}}
	d.AddImages(func(ExportRow) string { return "https://cdn.rebrickable.com/p.png" }, func(string) []byte { return png })
	d.Picture = NewImage("https://cdn.rebrickable.com/s.jpg", nil)
	b, err := JSON(d)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Picture *Image
		Parts   []struct{ Image *Image }
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	im := out.Parts[0].Image
	got, _ := base64.StdEncoding.DecodeString(im.Base64)
	if im.MIME != "image/png" || !bytes.Equal(got, png) || im.URL == "" {
		t.Errorf("row image = %+v", im)
	}
	if out.Picture == nil || out.Picture.Base64 != "" || out.Picture.URL == "" {
		t.Errorf("picture without bytes should carry only its URL: %+v", out.Picture)
	}
	h, _ := HTML(d)
	if !strings.Contains(string(h), "data:image/png;base64,") {
		t.Error("HTML should embed the cached picture")
	}
}

func TestSortingSheetGroupsByColour(t *testing.T) {
	d := &ExportData{Title: "Set 1-1", When: time.Now(), Rows: []ExportRow{
		{PartNum: "3023", ColorName: "Blue", Category: "Plates", Qty: 4},
		{PartNum: "3001", ColorName: "Red", Category: "Bricks", Qty: 2},
		{PartNum: "3001", ColorName: "Blue", Category: "Bricks", Qty: 6},
	}}
	b, err := SortingHTML(d)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	blue, red := strings.Index(s, "Blue — 10 piece(s)"), strings.Index(s, "Red — 2 piece(s)")
	if blue < 0 || red < blue || strings.Index(s, ">3001<") > strings.Index(s, ">3023<") {
		t.Fatalf("groups/order wrong:\n%s", s)
	}
}

func TestSortingSheetShowsElementSwatchAndEmbeddedPicture(t *testing.T) {
	im := NewImage("https://cdn.rebrickable.com/media/parts/elements/6143318.jpg", []byte("\x89PNG\r\n\x1a\n"))
	d := &ExportData{Title: "Set 1-1", When: time.Now(), Rows: []ExportRow{
		{PartNum: "23443", ColorName: "Black", ColorRGB: "05131D", Element: "6143318", Category: "Bars", Qty: 1, Image: im},
		{PartNum: "1", ColorName: "Odd", ColorRGB: "red;}</style>", Qty: 1},
	}}
	b, err := SortingHTML(d)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"element 6143318", "background:#05131D", "data:image/png;base64,", "page "} {
		if !strings.Contains(s, want) {
			t.Errorf("sheet is missing %q", want)
		}
	}
	if strings.Contains(s, "</style>\"") || strings.Contains(s, "red;}") {
		t.Error("an invalid colour value reached the CSS")
	}
	if strings.Contains(s, `loading="lazy"`) {
		t.Error("pictures must not be lazy: a saved or printed copy would miss the ones not scrolled to")
	}
}

func TestThumbnailShrinksBigPicturesAndLeavesSmallOnes(t *testing.T) {
	big := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for i := range big.Pix {
		big.Pix[i] = byte(i * 7)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, big); err != nil {
		t.Fatal(err)
	}
	out := thumbnail(buf.Bytes())
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != 120 || cfg.Height != 90 {
		t.Fatalf("want 120x90, got %v %v", cfg, err)
	}
	small := image.NewRGBA(image.Rect(0, 0, 40, 30))
	var sb bytes.Buffer
	_ = png.Encode(&sb, small)
	if !bytes.Equal(thumbnail(sb.Bytes()), sb.Bytes()) {
		t.Error("a small picture must come back unchanged")
	}
	if !bytes.Equal(thumbnail([]byte("not a picture")), []byte("not a picture")) {
		t.Error("undecodable bytes must come back unchanged")
	}
}

func TestSortingSheetSignOffAndBorrowedBlock(t *testing.T) {
	d := &ExportData{Title: "Set 21027-1", When: time.Now(), SetNum: "21027-1", CheckerID: "1234 Bob<script>",
		Rows:     []ExportRow{{PartNum: "3001", ColorName: "Red", Qty: 4}},
		Borrowed: []BorrowedPart{{NeedPartNum: "3001", ColorName: "Red", Qty: 2, FromSetNum: "10696-1", Status: "to order", Note: "was short"}}}
	b, err := SortingHTML(d)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`value="1234"`, "1 big, 2 small", "Borrowed parts — taken from another set", ">10696-1<", "was short", `id="ack"`, "straight away", "Tap again to clear"} {
		if !strings.Contains(s, want) {
			t.Errorf("sheet is missing %q", want)
		}
	}
	if strings.Contains(s, "confirm(") {
		t.Error("Clear marks must not use a pop-up: phone and file viewers block it and the button looks dead")
	}
	if strings.Contains(s, "Bob") || strings.Contains(s, "<script>Bob") {
		t.Error("the sign-off IDs must be digits only, never a name")
	}
	if got := strings.Count(s, `aria-label="Part needed"`); got != 8 {
		t.Errorf("blank borrowed rows = %d, want 8", got)
	}
}

func TestBorrowedPartAccounting(t *testing.T) {
	db := newTestDB(t)
	mk := func(set string, have int) *SetCheck {
		c := &SetCheck{SetNum: set, Kind: CheckIntake, Status: StatusDraft, CheckedBy: "t", StartedAt: time.Now(),
			Lines: []CheckLine{{PartNum: "3001", PartName: "Brick", ColorID: 4, ColorName: "Red", Need: 4, Have: have}}}
		if _, err := db.FinishCheck(c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	mk("10696-1", 4)          // the donor: complete
	build := mk("21027-1", 2) // short 2 of 3001 red
	if _, _, m, _, _ := build.Totals(); m != 2 {
		t.Fatalf("missing before = %d, want 2", m)
	}
	id, err := db.AddBorrowedPart(BorrowedPart{BuildSetNum: "21027-1", NeedPartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 2, FromSetNum: "10696-1", CreatedBy: "t"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetCheck(build.ID)
	if l := got.Lines[0]; l.Borrowed != 2 || l.BorrowedFrom != "10696-1" || l.Missing() != 0 {
		t.Errorf("borrower line = %+v (missing %d), want 2 borrowed from 10696-1 and nothing missing", l, l.Missing())
	}
	if st := db.GetSetState("21027-1"); st.MissingQty != 0 {
		t.Errorf("borrower missing_qty = %d, want 0", st.MissingQty)
	}
	// the donor now has 2 on loan: a recount that finds only 2 must not report them missing
	rc, err := db.NewCheck(context.Background(), nil, "10696-1", CheckRecount, "t")
	if err != nil {
		t.Fatal(err)
	}
	if rc.Lines[0].Lent != 2 {
		t.Fatalf("donor lent = %d, want 2", rc.Lines[0].Lent)
	}
	rc.Lines[0].Have = 2
	if m := rc.Lines[0].Missing(); m != 0 {
		t.Errorf("donor missing while lent = %d, want 0", m)
	}
	// replacing the part brings the donor back to expecting it
	for i := 0; i < 2; i++ {
		if _, err := db.AdvanceBorrowedPart(id); err != nil {
			t.Fatal(err)
		}
	}
	dn, _ := db.GetCheck(db.GetSetState("10696-1").LastCheck.ID)
	if dn.Lines[0].Lent != 0 {
		t.Errorf("donor lent after replaced = %d, want 0", dn.Lines[0].Lent)
	}
}

func TestFinishCheckRefusesUnrecordedBorrowed(t *testing.T) {
	db := newTestDB(t)
	c := &SetCheck{SetNum: "21027-1", Kind: CheckIntake, Status: StatusDraft, CheckedBy: "t", StartedAt: time.Now(),
		Lines: []CheckLine{{PartNum: "3001", ColorID: 4, ColorName: "Red", Need: 4, Have: 2, Borrowed: 2}}}
	if _, err := db.FinishCheck(c); err == nil || !strings.Contains(err.Error(), "not recorded") {
		t.Fatalf("finishing with an unrecorded borrowed part: err = %v, want a refusal", err)
	}
	if _, err := db.AddBorrowedForCheck(&SetCheck{SetNum: "21027-1", Lines: []CheckLine{{PartNum: "3001", ColorID: 4, ColorName: "Red", Need: 4, Have: 2}}},
		BorrowedPart{NeedPartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 2, FromSetNum: "10696-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishCheck(c); err != nil {
		t.Fatalf("finishing once it is recorded: %v", err)
	}
}

func TestShortNameDropsBracketedRemarks(t *testing.T) {
	for in, want := range map[string]string{
		"Plate, Modified 1 x 2 with Bar Handle on End (Closed Ends) [Complete Assembly]": "Plate, Modified 1 x 2 with Bar Handle on End",
		"Hinge Plate 1 x 4 Swivel Top / Base - Solid Clip [Complete Assembly]":           "Hinge Plate 1 x 4 Swivel Top / Base - Solid Clip",
		"Brick 2 x 4": "Brick 2 x 4",
		"(Odd)":       "(Odd)",
	} {
		if got := shortName(in); got != want {
			t.Errorf("shortName(%q) = %q, want %q", in, got, want)
		}
	}
}
