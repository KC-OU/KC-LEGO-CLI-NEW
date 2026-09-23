package lego

import (
	"fmt"
	"path/filepath"
	"testing"
)

func benchDB(b *testing.B) *DB {
	b.Helper()
	d, err := Open(filepath.Join(b.TempDir(), "lego.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { d.Close() })
	return d
}

// bigExportData synthesises n rows without touching the database — Encode's
// writers only ever look at an *ExportData, so there's no need to seed a
// catalog and collection just to measure them.
func bigExportData(n int) *ExportData {
	d := &ExportData{Title: "Bench", Facts: [][2]string{{"Rows", fmt.Sprint(n)}}}
	for i := 0; i < n; i++ {
		d.Rows = append(d.Rows, ExportRow{
			PartNum: fmt.Sprintf("p%d", i%4000), Name: fmt.Sprintf("Part %d", i%4000), Category: "Bricks",
			ColorID: i % 40, ColorName: fmt.Sprintf("Colour %d", i%40), Qty: i%50 + 1, BLColor: i % 40,
		})
	}
	return d
}

func BenchmarkCanBuild(b *testing.B) {
	d := benchDB(b)
	tx, _ := d.Begin()
	for s := 1; s <= 3000; s++ {
		tx.Exec(`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts) VALUES (?,?,?,0,60)`, fmt.Sprintf("%d-1", s), "Set", 2000+s%25)
		tx.Exec(`INSERT INTO cat_inventories (id, version, set_num) VALUES (?,1,?)`, s, fmt.Sprintf("%d-1", s))
		for p := 0; p < 60; p++ {
			tx.Exec(`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (?,?,?,1)`, s, fmt.Sprintf("p%d", (s*7+p)%400), p%12)
		}
	}
	tx.Commit()
	for p := 0; p < 400; p++ {
		if err := d.AddOwnedPart(OwnedPart{PartNum: fmt.Sprintf("p%d", p), Name: "Part", Category: "Bricks", ColorID: p % 12, ColorName: "c", Qty: 5}); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.CanBuild(BuildOptions{MinPercent: 20}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSpreadsheetCSV(b *testing.B) {
	d := bigExportData(20000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SpreadsheetCSV(d)
	}
}

func BenchmarkXLSX(b *testing.B) {
	d := bigExportData(20000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := XLSX(d); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSortingHTML(b *testing.B) {
	d := bigExportData(20000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := SortingHTML(d); err != nil {
			b.Fatal(err)
		}
	}
}
