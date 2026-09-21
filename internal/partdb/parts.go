package partdb

import (
	"database/sql"
	"fmt"
)

type PartRow struct {
	ID          int
	Name        string
	MfgPN       string
	Category    string
	StockQty    float64
	Location    string
	Description string
}

// SearchParts mirrors the PartDB Hub browse query: name/MPN/category/
// description LIKE match, joined to categories/lots/locations, newest first.
func (d *DB) SearchParts(term string) ([]PartRow, error) {
	like := "%" + term + "%"
	rows, err := d.Query(`
		SELECT p.id, p.name, p.manufacturer_product_number, COALESCE(c.name, ''),
		       COALESCE(SUM(pl.amount), 0), COALESCE(sl.name, ''), COALESCE(p.description, '')
		FROM parts p
		LEFT JOIN categories c ON p.id_category = c.id
		LEFT JOIN part_lots pl ON pl.id_part = p.id
		LEFT JOIN storelocations sl ON pl.id_store_location = sl.id
		WHERE (? = '' OR p.name LIKE ? OR p.manufacturer_product_number LIKE ? OR c.name LIKE ? OR p.description LIKE ?)
		GROUP BY p.id ORDER BY p.id DESC LIMIT 50`,
		term, like, like, like, like)
	if err != nil {
		return nil, fmt.Errorf("searching parts: %w", err)
	}
	defer rows.Close()

	var out []PartRow
	for rows.Next() {
		var p PartRow
		if err := rows.Scan(&p.ID, &p.Name, &p.MfgPN, &p.Category, &p.StockQty, &p.Location, &p.Description); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type PartDetail struct {
	ID          int
	Name        string
	Description string
	Comment     string
	MfgPN       string
	MinAmount   float64
	Category    string
	Location    string
	TotalStock  float64
}

func (d *DB) GetPart(id int) (*PartDetail, error) {
	row := d.QueryRow(`
		SELECT p.id, p.name, COALESCE(p.description,''), COALESCE(p.comment,''), p.manufacturer_product_number, p.minamount,
		       COALESCE(c.name, ''), COALESCE(sl.name, ''), COALESCE((SELECT SUM(amount) FROM part_lots WHERE id_part = p.id), 0)
		FROM parts p
		LEFT JOIN categories c ON p.id_category = c.id
		LEFT JOIN part_lots pl ON pl.id_part = p.id
		LEFT JOIN storelocations sl ON pl.id_store_location = sl.id
		WHERE p.id = ? LIMIT 1`, id)

	var pd PartDetail
	if err := row.Scan(&pd.ID, &pd.Name, &pd.Description, &pd.Comment, &pd.MfgPN, &pd.MinAmount, &pd.Category, &pd.Location, &pd.TotalStock); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("part %d not found", id)
		}
		return nil, err
	}
	return &pd, nil
}

// FindPartByIPN looks up a part by its ipn column (internal part number) —
// used by callers that have a stable external identifier (e.g. a
// Rebrickable part_num) to match against on repeated syncs, rather than a
// fuzzy name search. Returns 0, nil if no part has that ipn.
func (d *DB) FindPartByIPN(ipn string) (int, error) {
	if ipn == "" {
		return 0, nil
	}
	var id int
	err := d.QueryRow("SELECT id FROM parts WHERE ipn = ? LIMIT 1", ipn).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("looking up part by ipn %s: %w", ipn, err)
	}
	return id, nil
}
