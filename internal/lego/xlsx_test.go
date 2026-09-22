package lego

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

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
