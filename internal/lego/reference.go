package lego

import (
	"database/sql"
	"fmt"
	"strings"
)

// SearchRefSets is the local fallback set lookup — a parameterized LIKE
// query over the imported legolookup.json snapshot, replacing the legacy
// tool's jq `test($input; "i")` filter (which built its filter string by
// concatenating raw user input — unsanitized, even if jq's sandboxing meant
// it was never exploitable).
func (d *DB) SearchRefSets(term string) ([]RefSet, error) {
	like := "%" + term + "%"
	rows, err := d.Query(`SELECT set_num, name, year, theme, total_pieces FROM ref_sets
		WHERE (? = '' OR name LIKE ? OR set_num LIKE ?) ORDER BY name LIMIT 200`, term, like, like)
	if err != nil {
		return nil, fmt.Errorf("searching reference sets: %w", err)
	}
	defer rows.Close()

	var out []RefSet
	for rows.Next() {
		var r RefSet
		if err := rows.Scan(&r.SetNum, &r.Name, &r.Year, &r.Theme, &r.TotalPieces); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SearchRefParts is the local fallback part lookup over the imported
// legolookup-part.json snapshot.
func (d *DB) SearchRefParts(term string) ([]RefPart, error) {
	like := "%" + term + "%"
	rows, err := d.Query(`SELECT part_num, name, category FROM ref_parts
		WHERE (? = '' OR name LIKE ? OR part_num LIKE ?) ORDER BY name LIMIT 200`, term, like, like)
	if err != nil {
		return nil, fmt.Errorf("searching reference parts: %w", err)
	}
	defer rows.Close()

	var out []RefPart
	for rows.Next() {
		var r RefPart
		if err := rows.Scan(&r.PartNum, &r.Name, &r.Category); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRefSet finds one set in the imported catalog by exact number, with or
// without Rebrickable's "-1" suffix.
func (d *DB) GetRefSet(setNum string) (*RefSet, error) {
	var r RefSet
	err := d.QueryRow(`SELECT set_num, name, year, theme, total_pieces FROM ref_sets
		WHERE set_num = ? OR set_num = ? OR set_num = ? LIMIT 1`, setNum, setNum+"-1", strings.TrimSuffix(setNum, "-1")).
		Scan(&r.SetNum, &r.Name, &r.Year, &r.Theme, &r.TotalPieces)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("looking up reference set %s: %w", setNum, err)
	}
	return &r, nil
}

// GetRefPart finds one part in the imported catalog by exact number.
func (d *DB) GetRefPart(partNum string) (*RefPart, error) {
	var r RefPart
	err := d.QueryRow(`SELECT part_num, name, category FROM ref_parts WHERE part_num = ? LIMIT 1`, partNum).
		Scan(&r.PartNum, &r.Name, &r.Category)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("looking up reference part %s: %w", partNum, err)
	}
	return &r, nil
}
