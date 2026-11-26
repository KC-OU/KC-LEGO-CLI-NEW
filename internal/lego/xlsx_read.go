package lego

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A minimal .xlsx reader (zip + XML from the standard library): first worksheet
// only, values only -- no formulas evaluated, no styles. Enough to take a
// retirement list saved from Excel; anything fancier should be exported to CSV.

type xlsxSST struct {
	Items []struct {
		T string `xml:"t"`
		R []struct {
			T string `xml:"t"`
		} `xml:"r"`
	} `xml:"si"`
}

type xlsxSheet struct {
	Rows []struct {
		Cells []struct {
			Ref string `xml:"r,attr"`
			T   string `xml:"t,attr"`
			V   string `xml:"v"`
			IS  struct {
				T string `xml:"t"`
			} `xml:"is"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
}

func colIndex(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A') + 1
	}
	return n - 1
}

// ReadXLSX returns the first sheet as rows of text.
func ReadXLSX(data []byte) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("not a valid .xlsx file (save it as .xlsx or export to CSV)")
	}
	read := func(f *zip.File) ([]byte, error) {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, 32<<20))
	}
	var shared []string
	var sheets []*zip.File
	for _, f := range zr.File {
		switch {
		case f.Name == "xl/sharedStrings.xml":
			b, err := read(f)
			if err != nil {
				return nil, err
			}
			var sst xlsxSST
			if err := xml.Unmarshal(b, &sst); err != nil {
				return nil, err
			}
			for _, it := range sst.Items {
				s := it.T
				for _, r := range it.R {
					s += r.T
				}
				shared = append(shared, s)
			}
		case strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml"):
			sheets = append(sheets, f)
		}
	}
	if len(sheets) == 0 {
		return nil, errors.New("no worksheet found in the .xlsx file")
	}
	sort.Slice(sheets, func(i, j int) bool { return sheets[i].Name < sheets[j].Name })
	b, err := read(sheets[0])
	if err != nil {
		return nil, err
	}
	var sh xlsxSheet
	if err := xml.Unmarshal(b, &sh); err != nil {
		return nil, err
	}
	var out [][]string
	for _, row := range sh.Rows {
		var cells []string
		for _, c := range row.Cells {
			i := colIndex(c.Ref)
			for len(cells) <= i {
				cells = append(cells, "")
			}
			switch c.T {
			case "s":
				if n, err := strconv.Atoi(c.V); err == nil && n >= 0 && n < len(shared) {
					cells[i] = shared[n]
				}
			case "inlineStr":
				cells[i] = c.IS.T
			default:
				cells[i] = c.V
			}
		}
		out = append(out, cells)
	}
	return out, nil
}

// excelDate turns an Excel serial day number (e.g. 46387) into 2006-01-02; "" if v is not one.
func excelDate(v string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f < 20000 || f > 80000 {
		return ""
	}
	return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(f)).Format("2006-01-02")
}

// rowsToCSV re-encodes sheet rows as CSV for ParseRetirementCSV, converting
// Excel serial numbers in any column whose header mentions "date".
func rowsToCSV(rows [][]string) []byte {
	var dateCols = map[int]bool{}
	if len(rows) > 0 {
		for i, h := range rows[0] {
			if strings.Contains(strings.ToLower(h), "date") {
				dateCols[i] = true
			}
		}
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	for ri, r := range rows {
		if ri > 0 {
			for i := range r {
				if dateCols[i] {
					if d := excelDate(r[i]); d != "" {
						r[i] = d
					}
				}
			}
		}
		_ = w.Write(r)
	}
	w.Flush()
	return buf.Bytes()
}

// ImportRetirementFile imports an uploaded retirement list: .xlsx (Excel) or CSV,
// matched by header name like the community sheet. Returns how many sets it read.
func (d *DB) ImportRetirementFile(name string, data []byte) (int, error) {
	if len(data) == 0 {
		return 0, errors.New("the file is empty")
	}
	if strings.HasSuffix(strings.ToLower(name), ".xlsx") {
		rows, err := ReadXLSX(data)
		if err != nil {
			return 0, err
		}
		data = rowsToCSV(rows)
	}
	infos, err := ParseRetirementCSV(bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("could not read %s: %w", name, err)
	}
	return d.ImportRetirements(infos)
}
