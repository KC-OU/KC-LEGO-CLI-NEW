package partdb

import "database/sql"

type Category struct {
	ID       int
	ParentID sql.NullInt64
	Name     string
}

func (d *DB) AllCategories() ([]Category, error) {
	rows, err := d.Query("SELECT id, parent_id, name FROM categories")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.ParentID, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type PartLot struct {
	ID              int
	PartID          int
	StoreLocationID sql.NullInt64
	Amount          float64
}

func (d *DB) AllPartLots() ([]PartLot, error) {
	rows, err := d.Query("SELECT id, id_part, id_store_location, amount FROM part_lots")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PartLot
	for rows.Next() {
		var l PartLot
		if err := rows.Scan(&l.ID, &l.PartID, &l.StoreLocationID, &l.Amount); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

type StoreLocation struct {
	ID   int
	Name string
}

func (d *DB) AllStoreLocations() ([]StoreLocation, error) {
	rows, err := d.Query("SELECT id, name FROM storelocations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StoreLocation
	for rows.Next() {
		var s StoreLocation
		if err := rows.Scan(&s.ID, &s.Name); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// FullPart is the shape internal/sync needs from PartDB: a part plus its
// category id and total lot quantity, without the join-heavy display
// formatting SearchParts/GetPart do for the TUI.
type FullPart struct {
	ID            int
	Name          string
	Description   string
	Comment       string
	IPN           string
	GTIN          string
	MfgPN         string
	CategoryID    int
	Mass          sql.NullFloat64
	DatetimeAdded string
	LastModified  string
}

func (d *DB) AllParts() ([]FullPart, error) {
	rows, err := d.Query(`SELECT id, name, COALESCE(description,''), COALESCE(comment,''), COALESCE(ipn,''), COALESCE(gtin,''),
		manufacturer_product_number, id_category, mass, COALESCE(datetime_added,''), COALESCE(last_modified,'') FROM parts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FullPart
	for rows.Next() {
		var p FullPart
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Comment, &p.IPN, &p.GTIN,
			&p.MfgPN, &p.CategoryID, &p.Mass, &p.DatetimeAdded, &p.LastModified); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
