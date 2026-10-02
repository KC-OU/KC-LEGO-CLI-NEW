package labels

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

func unzipLBX(t *testing.T, b []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		rc.Close()
		out[f.Name] = buf.String()
	}
	return out
}

func TestLBXIsAWellFormedZipWithBothFiles(t *testing.T) {
	s, _ := SizeByID("38x90")
	d := sample()
	d.QR = "https://rebrickable.com/sets/75192-1/"
	b, err := LBX([]Data{d}, s)
	if err != nil {
		t.Fatal(err)
	}
	files := unzipLBX(t, b)
	for _, name := range []string{"label.xml", "prop.xml"} {
		body, ok := files[name]
		if !ok {
			t.Fatalf("missing %s in the .lbx zip", name)
		}
		var v any
		if err := xml.Unmarshal([]byte(body), &v); err != nil {
			t.Errorf("%s is not well-formed XML: %v", name, err)
		}
	}
}

func TestLBXLabelXMLHasTheRightPaperSizeAndContent(t *testing.T) {
	s, _ := SizeByID("62x100")
	d := sample()
	d.QR = "https://rebrickable.com/sets/75192-1/"
	b, err := LBX([]Data{d}, s)
	if err != nil {
		t.Fatal(err)
	}
	xmlBody := unzipLBX(t, b)["label.xml"]

	wantW, wantH := mmToPt(62), mmToPt(100)
	if !strings.Contains(xmlBody, fmt.Sprintf(`width="%.1fpt"`, wantW)) {
		t.Errorf("expected paper width %.1fpt in:\n%s", wantW, xmlBody)
	}
	if !strings.Contains(xmlBody, fmt.Sprintf(`height="%.1fpt"`, wantH)) {
		t.Errorf("expected paper height %.1fpt in:\n%s", wantH, xmlBody)
	}
	if !strings.Contains(xmlBody, "75192-1") {
		t.Error("expected the set number in the text block")
	}
	if !strings.Contains(xmlBody, `protocol="QRCODE"`) || !strings.Contains(xmlBody, "rebrickable.com") {
		t.Error("expected a native QRCODE barcode object with the QR data")
	}
	if !strings.Contains(xmlBody, `protocol="CODE128"`) {
		t.Error("expected a native CODE128 barcode object")
	}
}

func TestLBXWithoutQRSkipsTheQRObject(t *testing.T) {
	s, _ := SizeByID("38x90")
	d := sample()
	d.QR = ""
	b, err := LBX([]Data{d}, s)
	if err != nil {
		t.Fatal(err)
	}
	xmlBody := unzipLBX(t, b)["label.xml"]
	if strings.Contains(xmlBody, `protocol="QRCODE"`) {
		t.Error("no QR data should mean no QRCODE object")
	}
}

func TestLBXRejectsMoreThanOneLabelOrSheetStock(t *testing.T) {
	s62, _ := SizeByID("62x29")
	if _, err := LBX([]Data{sample(), sample()}, s62); err == nil {
		t.Error("LBX with more than one item should be refused")
	}
	a4, _ := SizeByID("a4")
	if _, err := LBX([]Data{sample()}, a4); err == nil {
		t.Error("LBX on sheet stock should be refused")
	}
}
