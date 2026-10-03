// Package reports generates small, one-off PDF records — distinct from
// internal/labels, which is purpose-built for fixed-size label layouts
// (mm dimensions, barcode ops). This writes raw PDF bytes the same
// low-level way labels.PDF does (no external library, see that file's own
// doc comment for why), just for an ordinary A4 text page instead.
package reports

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// AccuracyReportData is everything a filed accuracy report shows — see
// lego.AccuracyReport, lego.FileAccuracyReport.
type AccuracyReportData struct {
	Username, Role, ReviewedBy string
	Target                     string // the set number or order ref that triggered it
	Missing                    int
	Summary, ActionTaken       string
	TalkRequested              bool
	CreatedAt                  time.Time
}

const (
	a4W, a4H = 595.28, 841.89 // A4 at 72pt/inch — 210mm x 297mm
	margin   = 50.0
	lineH    = 14.0
)

// AccuracyReportPDF renders one filed report as a single A4 page.
func AccuracyReportPDF(r AccuracyReportData) []byte {
	var c strings.Builder
	y := a4H - margin
	line := func(font string, size float64, text string) {
		fmt.Fprintf(&c, "BT 0 g %s %.1f Tf %.1f %.1f Td (%s) Tj ET\n", font, size, margin, y, reportEscape(text))
		y -= lineH
	}
	gap := func() { y -= lineH / 2 }

	line("/F2", 16, "Accuracy Report")
	gap()
	line("/F1", 10, fmt.Sprintf("Employee: %s (%s)", r.Username, r.Role))
	line("/F1", 10, "Reviewed by: "+r.ReviewedBy)
	line("/F1", 10, "Date: "+r.CreatedAt.Format("2 Jan 2006 15:04"))
	line("/F1", 10, fmt.Sprintf("Triggering check: %s - %d part(s) missing", r.Target, r.Missing))
	gap()
	line("/F2", 11, "Summary")
	for _, l := range wrapText(r.Summary, 95) {
		line("/F1", 10, l)
	}
	gap()
	line("/F2", 11, "Action Taken")
	for _, l := range wrapText(r.ActionTaken, 95) {
		line("/F1", 10, l)
	}
	gap()
	talk := "No"
	if r.TalkRequested {
		talk = "Yes"
	}
	line("/F1", 10, "Follow-up conversation requested: "+talk)

	return onePagePDF(c.String())
}

// wrapText is a plain word-wrap at roughly maxChars per line — Helvetica
// isn't monospace, so this is a rough fit (same honestly-labelled
// approximation as labels.go's QR cellSize estimate), good enough for a
// single-column internal report, not a typeset document.
func wrapText(s string, maxChars int) []string {
	if s == "" {
		return []string{""}
	}
	var lines []string
	var cur strings.Builder
	for _, word := range strings.Fields(s) {
		if cur.Len() > 0 && cur.Len()+1+len(word) > maxChars {
			lines = append(lines, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(word)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

func reportEscape(s string) string {
	r := strings.NewReplacer("—", "-", "–", "-", "‘", "'", "’", "'", "“", "\"", "”", "\"", "…", "...")
	s = r.Replace(s)
	var b strings.Builder
	for _, ch := range s {
		if ch >= 32 && ch < 127 {
			b.WriteRune(ch)
		}
	}
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(b.String())
}

// onePagePDF wraps one page's already-built content stream in the minimal
// PDF object structure labels.PDF also hand-writes: catalog, pages, two
// fonts, one page, one content stream.
func onePagePDF(content string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [5 0 R] /Count 1 >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents 6 0 R >>", a4W, a4H),
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}
