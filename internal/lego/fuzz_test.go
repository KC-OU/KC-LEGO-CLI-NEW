package lego

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

func FuzzFTSQuery(f *testing.F) {
	for _, s := range []string{"brick 2 x 4", `"`, `) OR (`, "NEAR(a b)", "2x4", "", "ünï", strings.Repeat("a ", 100)} {
		f.Add(s)
	}
	db, err := Open(filepath.Join(f.TempDir(), "lego.db"))
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { db.Close() })
	db.Exec(`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('3001','Brick 2 x 4',11)`)
	db.Exec(`INSERT INTO fts_parts(fts_parts) VALUES('rebuild')`)
	f.Fuzz(func(t *testing.T, in string) {
		q := ftsQuery(in)
		for _, r := range q {
			if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '"' || r == '*' || r == ' ') {
				t.Fatalf("unexpected character %q in query %q from %q", r, q, in)
			}
		}
		// the query must always be valid FTS5 syntax, whatever was typed
		if _, err := db.SearchCatalogParts(in, 5); err != nil {
			t.Fatalf("SearchCatalogParts(%q): %v (query %q)", in, err, q)
		}
	})
}

func FuzzParseRebrickableCSV(f *testing.F) {
	f.Add("Part,Color,Quantity\n3001,4,10\n")
	f.Add("\xef\xbb\xbfpart_num,color_id,quantity,is_spare\n3001,Red,1,True\n")
	f.Add("a,b\n\"unterminated,1\n")
	f.Add("")
	colors := testColors()
	f.Fuzz(func(t *testing.T, in string) {
		rows, _, _ := ParseRebrickableCSV(strings.NewReader(in), colors)
		for _, r := range rows {
			if r.PartNum == "" || r.Qty < 0 {
				t.Fatalf("a parsed row must have a part and a non-negative quantity: %+v", r)
			}
		}
	})
}

func FuzzParseBrickLinkXML(f *testing.F) {
	f.Add(`<INVENTORY><ITEM><ITEMTYPE>P</ITEMTYPE><ITEMID>3001</ITEMID><COLOR>5</COLOR><QTY>2</QTY></ITEM></INVENTORY>`)
	f.Add(`<INVENTORY><ITEM><ITEMID>&lt;x</ITEMID></ITEM>`)
	f.Add(`<!DOCTYPE x [<!ENTITY a "aaaa">]><INVENTORY><ITEM><ITEMID>&a;</ITEMID><QTY>1</QTY></ITEM></INVENTORY>`)
	f.Add("")
	colors := testColors()
	f.Fuzz(func(t *testing.T, in string) {
		rows, _, _ := ParseBrickLinkXML(strings.NewReader(in), colors)
		for _, r := range rows {
			if r.PartNum == "" || r.Qty < 0 {
				t.Fatalf("bad row %+v", r)
			}
		}
	})
}

func FuzzLoadCatalogFile(f *testing.F) {
	for _, body := range catalogCSV {
		f.Add(gz(body))
	}
	f.Add([]byte("not gzip"))
	f.Add([]byte{})
	db, err := Open(filepath.Join(f.TempDir(), "lego.db"))
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { db.Close() })
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(dir, "x.csv.gz")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, spec := range catalogSpecs {
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			_, _ = loadCatalogFile(tx, spec, path) // any error is fine; a panic or hang is not
			tx.Rollback()
		}
	})
}

func FuzzSetLookupNumbers(f *testing.F) {
	for _, s := range []string{"75192", "75192-1", "", "-", "../../x", "1'; DROP TABLE sets;--", "\x00"} {
		f.Add(s)
	}
	db, err := Open(filepath.Join(f.TempDir(), "lego.db"))
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { db.Close() })
	f.Fuzz(func(t *testing.T, in string) {
		if _, err := db.CatalogSet(in); err != nil {
			t.Fatalf("CatalogSet(%q): %v", in, err)
		}
		db.ElementPart(in)
		if _, err := db.SearchCatalogSets(in, 3); err != nil {
			t.Fatalf("SearchCatalogSets(%q): %v", in, err)
		}
	})
}
