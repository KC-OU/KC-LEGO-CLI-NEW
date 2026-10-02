package labels

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"strings"
	"time"
	"unicode/utf8"
)

// LBX renders exactly one label as a Brother .lbx file — a ZIP of Brother's
// own XML (label.xml + prop.xml), reverse-engineered by the P-touch Editor
// community since Brother never published a spec; structure confirmed
// against a real example
// (github.com/Alecto3-D/brother-p-touch-editor-format). Confidence here is
// lower than PNG/ZPL/PDF/HTML: there's no official schema to check against,
// only examples — this is a deliberately simpler first pass than the other
// renderers' precise multi-field layout: real paper dimensions (what
// actually caused the original print mismatch) and real, native QR/Code128
// barcode objects (not a rasterized image Brother has to guess the size
// of), with the rest of the label's text as one auto-shrinking text block —
// "shrink=true"/LONGTEXTFIXED lets Brother's own layout engine fit the
// lines, rather than this code trying to reproduce it. Expect to test-print
// on a real QL-600 and iterate; this is the one format in this package that
// can't be verified without one.
func LBX(items []Data, s Size) ([]byte, error) {
	if len(items) != 1 {
		return nil, fmt.Errorf(".lbx export is one label at a time (got %d) — use --format pdf or html for a batch", len(items))
	}
	if s.Sheet() {
		return nil, fmt.Errorf(".lbx export doesn't support sheet stock (%s) — use --format pdf or html", s.ID)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct{ name, body string }{
		{"label.xml", lbxLabelXML(items[0], s)},
		{"prop.xml", lbxPropXML()},
	} {
		w, err := zw.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// mmToPt matches render.go's PDF() conversion (1mm = 72/25.4pt) — Brother's
// XML measures everything in points too.
func mmToPt(mm float64) float64 { return mm * 72 / 25.4 }

func lbxLabelXML(d Data, s Size) string {
	W, H := mmToPt(s.W), mmToPt(s.H)
	pad := W * 0.04

	bc := d.Barcode
	if bc == "" {
		bc = d.SetNum
	}
	checked := "not checked yet"
	if d.Checked {
		checked = "Checked " + d.CheckedAt.Local().Format("2 Jan 2006") + " by " + d.CheckedBy
	}
	lines := []string{d.SetNum, d.Name, fmt.Sprintf("%d  -  %d pcs", d.Year, d.Pieces), d.Status()}
	if d.Location != "" {
		lines = append(lines, "Location: "+d.Location)
	}
	lines = append(lines, checked)
	text := strings.Join(lines, "\n")

	barcodeH := min(H*0.14, mmToPt(10))
	qrSide, qrCellSize := 0.0, 0.0
	if d.QR != "" {
		qrSide = min(W-2*pad, H-barcodeH-2*pad) * 0.4
		// Brother renders a QR at moduleCount × cellSize, not at
		// objectStyle's width/height (that's only the editor's selection
		// frame) — a zero/unset cellSize renders literally nothing. There's
		// no way to predict moduleCount (Brother's own encoder picks the QR
		// version from the data at version="auto") without reimplementing
		// its encoder, so this is a rough fit assuming a typical short
		// payload (~version 2-3, ~29 modules) rather than an exact one.
		qrCellSize = max(1, qrSide/29)
	}
	textH := H - barcodeH - qrSide - 3*pad

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<pt:document xmlns:pt="http://schemas.brother.info/ptouch/2007/lbx/main" xmlns:style="http://schemas.brother.info/ptouch/2007/lbx/style" xmlns:text="http://schemas.brother.info/ptouch/2007/lbx/text" xmlns:draw="http://schemas.brother.info/ptouch/2007/lbx/draw" xmlns:image="http://schemas.brother.info/ptouch/2007/lbx/image" xmlns:barcode="http://schemas.brother.info/ptouch/2007/lbx/barcode" xmlns:database="http://schemas.brother.info/ptouch/2007/lbx/database" xmlns:table="http://schemas.brother.info/ptouch/2007/lbx/table" version="1.1" generator="wms-go">` + "\n")
	b.WriteString(`<pt:body currentSheet="Sheet 1">` + "\n")
	fmt.Fprintf(&b, `<style:sheet name="Sheet 1"><style:paper media="0" width="%.1fpt" height="%.1fpt" marginLeft="0pt" marginTop="0pt" marginRight="0pt" marginBottom="0pt" orientation="portrait" autoLength="false" monochromeDisplay="true" paperColor="#FFFFFF" paperInk="#000000" split="1" format="" backgroundTheme="0"/>`+"\n", W, H)
	fmt.Fprintf(&b, `<style:backGround x="0pt" y="0pt" width="%.1fpt" height="%.1fpt" brushStyle="NULL" brushId="0" color="#000000" backColor="#FFFFFF"/>`+"\n", W, H)
	b.WriteString(`<pt:objects>` + "\n")

	// All the label's text as one auto-shrinking block. text:stringItem (one
	// run, since this is uniformly styled) must come after pt:data with a
	// charLen matching the text's rune count exactly, or P-touch Editor
	// renders the whole object as blank — confirmed against a real P-touch
	// Editor export (github.com/Alecto3-D/brother-p-touch-editor-format) and
	// github.com/jdlien/lbx-utils's own format notes, element order (data
	// before stringItem) matters too.
	fmt.Fprintf(&b, `<text:text><pt:objectStyle x="%.1fpt" y="%.1fpt" width="%.1fpt" height="%.1fpt" backColor="#FFFFFF" ropMode="COPYPEN" angle="0" anchor="TOPLEFT" flip="NONE"><pt:pen style="NULL" widthX="0.5pt" widthY="0.5pt" color="#000000"/><pt:brush style="NULL" color="#000000" id="0"/><pt:expanded objectName="Text1" ID="0" lock="0" templateMergeTarget="LABELLIST" templateMergeType="NONE" templateMergeID="0" dbMergeFieldStyleName="" linkStatus="NONE" linkID="0"/></pt:objectStyle><text:ptFontInfo><text:logFont name="Helsinki" width="0pt" italic="false" weight="400" charSet="0" pitchAndFamily="0"/><text:fontExt effect="NOEFFECT" underline="0" strikeout="0" size="10pt" orgSize="10pt" textColor="#000000"/></text:ptFontInfo><text:textControl control="LONGTEXTFIXED" clipFrame="false" aspectNormal="true" shrink="true" autoLF="true" avoidImage="false"/><text:textAlign horizontalAlignment="LEFT" verticalAlignment="TOP" inLineAlignment="BASELINE"/><text:textStyle vertical="false" nullBlock="false" charSpace="0" lineSpace="0" orgPoint="10pt"/><pt:data>%s</pt:data><text:stringItem charLen="%d"><text:ptFontInfo><text:logFont name="Helsinki" width="0pt" italic="false" weight="400" charSet="0" pitchAndFamily="0"/><text:fontExt effect="NOEFFECT" underline="0" strikeout="0" size="10pt" orgSize="10pt" textColor="#000000"/></text:ptFontInfo></text:stringItem></text:text>`+"\n",
		pad, pad, W-2*pad, textH, html.EscapeString(text), utf8.RuneCountInString(text))

	y := pad + textH + pad
	if qrSide > 0 {
		fmt.Fprintf(&b, `<barcode:barcode><pt:objectStyle x="%.1fpt" y="%.1fpt" width="%.1fpt" height="%.1fpt" backColor="#FFFFFF" ropMode="COPYPEN" angle="0" anchor="TOPLEFT" flip="NONE"><pt:pen style="NULL" widthX="0.5pt" widthY="0.5pt" color="#000000"/><pt:brush style="NULL" color="#000000" id="0"/><pt:expanded objectName="QR1" ID="0" lock="0" templateMergeTarget="LABELLIST" templateMergeType="NONE" templateMergeID="0" dbMergeFieldStyleName="" linkStatus="NONE" linkID="0"/></pt:objectStyle><barcode:barcodeStyle protocol="QRCODE" lengths="0" zeroFill="false" barWidth="1pt" barRatio="1:3" humanReadable="false" humanReadableAlignment="LEFT" checkDigit="false" autoLengths="true" margin="false" sameLengthBar="false" bearerBar="false"/><pt:data>%s</pt:data><barcode:qrcodeStyle model="2" eccLevel="15%%" cellSize="%.1fpt" mbcs="932" removeCharKind="0" removeCharString="" joint="1" jointSpace="8" jointVertically="false" version="auto" changeVersionDrag="false"/></barcode:barcode>`+"\n",
			(W-qrSide)/2, y, qrSide, qrSide, html.EscapeString(d.QR), qrCellSize)
		y += qrSide + pad
	}

	fmt.Fprintf(&b, `<barcode:barcode><pt:objectStyle x="%.1fpt" y="%.1fpt" width="%.1fpt" height="%.1fpt" backColor="#FFFFFF" ropMode="COPYPEN" angle="0" anchor="TOPLEFT" flip="NONE"><pt:pen style="NULL" widthX="0.5pt" widthY="0.5pt" color="#000000"/><pt:brush style="NULL" color="#000000" id="0"/><pt:expanded objectName="Barcode1" ID="0" lock="0" templateMergeTarget="LABELLIST" templateMergeType="NONE" templateMergeID="0" dbMergeFieldStyleName="" linkStatus="NONE" linkID="0"/></pt:objectStyle><barcode:barcodeStyle protocol="CODE128" lengths="0" zeroFill="false" barWidth="1pt" barRatio="1:3" humanReadable="true" humanReadableAlignment="CENTER" checkDigit="false" autoLengths="true" margin="false" sameLengthBar="false" bearerBar="false"/><pt:data>%s</pt:data></barcode:barcode>`+"\n",
		pad, y, W-2*pad, max(1, H-y-pad), html.EscapeString(bc))

	b.WriteString(`</pt:objects></style:sheet></pt:body></pt:document>` + "\n")
	return b.String()
}

func lbxPropXML() string {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	return `<?xml version="1.0" encoding="UTF-8"?><meta:properties xmlns:meta="http://schemas.brother.info/ptouch/2007/lbx/meta" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/"><meta:appName>wms-go</meta:appName><dc:title></dc:title><dc:subject></dc:subject><dc:creator>wms-go</dc:creator><meta:keyword></meta:keyword><dc:description></dc:description><meta:template></meta:template><dcterms:created>` + now + `</dcterms:created><dcterms:modified>` + now + `</dcterms:modified><meta:lastPrinted>` + now + `</meta:lastPrinted><meta:modifiedBy>wms-go</meta:modifiedBy><meta:revision>1</meta:revision><meta:editTime>0</meta:editTime><meta:numPages>1</meta:numPages><meta:numWords>0</meta:numWords><meta:numChars>0</meta:numChars><meta:security>0</meta:security></meta:properties>`
}
