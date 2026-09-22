package lego

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// A minimal .xlsx writer (the Office Open XML parts Excel and LibreOffice need, and
// nothing more), so exports open as a real workbook without a spreadsheet library.
// Text is written as inline strings, which a spreadsheet never evaluates, so no
// formula-injection escaping is needed; the only formulas are the HYPERLINKs we build.

type xcell struct {
	s    string
	n    int
	num  bool
	link string // https URL: the cell is HYPERLINK(link, s)
}

func xs(s string) xcell { return xcell{s: s} }
func xn(n int) xcell    { return xcell{n: n, num: true} }

type xsheet struct {
	name string
	rows [][]xcell // the first row is the header (bold, frozen)
}

// XLSX is the export as an Excel workbook: a Parts sheet (with a Picture link per row),
// a Sets sheet when there are sets, and a Details sheet for a single part's or set's page.
func XLSX(d *ExportData) ([]byte, error) {
	var sheets []xsheet
	if len(d.Facts) > 0 {
		sh := xsheet{name: "Details", rows: [][]xcell{{xs("Field"), xs("Value")}}}
		for _, f := range d.Facts {
			sh.rows = append(sh.rows, []xcell{xs(f[0]), xs(f[1])})
		}
		if d.Picture != nil && strings.HasPrefix(d.Picture.URL, "https://") {
			sh.rows = append(sh.rows, []xcell{xs("Picture"), {s: d.Picture.URL, link: d.Picture.URL}})
		}
		sheets = append(sheets, sh)
	}
	if len(d.Build) > 0 {
		sh := xsheet{name: "Buildable", rows: [][]xcell{{xs("Set"), xs("Name"), xs("Theme"), xs("Year"), xs("Have %"), xs("Pieces"), xs("Have"), xs("Missing")}}}
		for _, r := range d.Build {
			sh.rows = append(sh.rows, []xcell{xs(r.SetNum), xs(r.Name), xs(r.Theme), xn(r.Year), xn(r.Percent), xn(r.Total), xn(r.Have), xn(r.Missing)})
		}
		sheets = append(sheets, sh)
	}
	if len(d.Rows) > 0 || (len(d.Sets) == 0 && len(d.Build) == 0 && len(d.Facts) == 0) {
		sh := xsheet{name: "Parts", rows: [][]xcell{{xs("Part"), xs("Name"), xs("Category"), xs("Colour"), xs("Colour id"), xs("Quantity"), xs("Minimum"), xs("Part-DB id"), xs("Picture")}}}
		for _, r := range d.Rows {
			id, pdb, pic := xs(""), xs(""), xs("")
			if r.ColorID >= 0 {
				id = xn(r.ColorID)
			}
			if r.PartDBID > 0 {
				pdb = xn(r.PartDBID)
			}
			if r.Image != nil && strings.HasPrefix(r.Image.URL, "https://") {
				pic = xcell{s: "view", link: r.Image.URL}
			}
			sh.rows = append(sh.rows, []xcell{xs(r.PartNum), xs(r.Name), xs(r.Category), xs(r.ColorName), id, xn(r.Qty), xn(r.MinQty), pdb, pic})
		}
		sheets = append(sheets, sh)
	}
	if len(d.Sets) > 0 {
		sh := xsheet{name: "Sets", rows: [][]xcell{{xs("Set"), xs("Name"), xs("Theme"), xs("Year"), xs("Copies"), xs("Pieces"), xs("Parted out")}}}
		for _, s := range d.Sets {
			sh.rows = append(sh.rows, []xcell{xs(s.SetNum), xs(s.Name), xs(s.Theme), xn(s.Year), xn(s.Qty), xn(s.PartsQty), xs(map[bool]string{true: "yes", false: "no"}[s.PartedOut])})
		}
		sheets = append(sheets, sh)
	}
	return writeXLSX(sheets)
}

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// colName is the spreadsheet column letter(s) for a 0-based index (0 → A, 26 → AA).
func colName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

func sheetXML(sh xsheet) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	// Width each column to its longest value (capped), since there is no autofit in the file format.
	if len(sh.rows) > 0 {
		b.WriteString("<cols>")
		for c := range sh.rows[0] {
			w := 8
			for _, r := range sh.rows {
				if c < len(r) {
					w = max(w, min(len([]rune(r[c].s))+2, 60))
				}
			}
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%d" customWidth="1"/>`, c+1, c+1, w)
		}
		b.WriteString("</cols>")
	}
	b.WriteString("<sheetData>")
	for ri, row := range sh.rows {
		fmt.Fprintf(&b, `<row r="%d">`, ri+1)
		for ci, c := range row {
			ref := colName(ci) + strconv.Itoa(ri+1)
			style := ""
			if ri == 0 {
				style = ` s="1"`
			}
			switch {
			case c.num:
				fmt.Fprintf(&b, `<c r="%s"%s><v>%d</v></c>`, ref, style, c.n)
			case c.link != "":
				f := fmt.Sprintf(`HYPERLINK("%s","%s")`, strings.ReplaceAll(c.link, `"`, `""`), strings.ReplaceAll(c.s, `"`, `""`))
				fmt.Fprintf(&b, `<c r="%s" s="2" t="str"><f>%s</f><v>%s</v></c>`, ref, xmlText(f), xmlText(c.s))
			case c.s != "":
				fmt.Fprintf(&b, `<c r="%s"%s t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, xmlText(c.s))
			}
		}
		b.WriteString("</row>")
	}
	b.WriteString("</sheetData></worksheet>")
	return b.String()
}

// Styles: 0 normal, 1 bold (headers), 2 blue underlined (links).
const xlsxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<fonts count="3"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font>` +
	`<font><u/><sz val="11"/><color rgb="FF0563C1"/><name val="Calibri"/></font></fonts>` +
	`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="3"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="0" fontId="2" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`</styleSheet>`

func writeXLSX(sheets []xsheet) ([]byte, error) {
	var ct, wbSheets, wbRels strings.Builder
	for i, sh := range sheets {
		n := i + 1
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&wbSheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlText(sh.name), n, n)
		fmt.Fprintf(&wbRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
	}
	files := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
			ct.String() + `</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets>` + wbSheets.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + wbRels.String() +
			fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, len(sheets)+1) +
			`</Relationships>`},
		{"xl/styles.xml", xlsxStyles},
	}
	for i, sh := range sheets {
		files = append(files, struct{ name, body string }{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sheetXML(sh)})
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
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
