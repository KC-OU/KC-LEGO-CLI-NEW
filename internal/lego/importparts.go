package lego

import (
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Bulk import of a parts list you already have somewhere else: a Rebrickable
// parts-list CSV, or a BrickLink inventory / wanted-list XML. Reading a file
// and applying it are separate steps so --dry-run can show exactly what would
// change first.

// ImportRow is one line of an input file.
type ImportRow struct {
	Line      int
	PartNum   string
	ColorID   int // Rebrickable colour id, NoColor when unknown or free text
	ColorName string
	Qty       int
}

// ColorTable finds colours by Rebrickable id and by name.
type ColorTable struct {
	ByID   map[int]Color
	ByName map[string]Color
	ByBL   map[int]Color // BrickLink colour id -> Rebrickable colour
}

// NewColorTable indexes colours (the offline catalog's, for names and ids).
func NewColorTable(cols []Color) *ColorTable {
	t := &ColorTable{ByID: map[int]Color{}, ByName: map[string]Color{}, ByBL: map[int]Color{}}
	for _, c := range cols {
		t.ByID[c.ID] = c
		t.ByName[strings.ToLower(c.Name)] = c
	}
	return t
}

// AddBrickLink adds the BrickLink-number mapping from Rebrickable's live colour table.
func (t *ColorTable) AddBrickLink(all []ColorInfo) {
	for _, ci := range all {
		if bl := ci.BrickLinkID(); bl > 0 {
			if _, taken := t.ByBL[bl]; !taken {
				t.ByBL[bl] = Color{ID: ci.ID, Name: ci.Name, RGB: ci.RGB, Trans: ci.IsTrans}
			}
		}
	}
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "t":
		return true
	}
	return false
}

func columnIndex(header []string, names ...string) int {
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
		for _, n := range names {
			if h == n {
				return i
			}
		}
	}
	return -1
}

// ParseRebrickableCSV reads a Rebrickable parts-list CSV (columns Part / part_num,
// Color / color_id, Quantity; spare rows are skipped). A colour that is a number
// is a Rebrickable colour id; a name is looked up, and kept as typed text if unknown.
func ParseRebrickableCSV(r io.Reader, colors *ColorTable) (rows []ImportRow, problems []string, err error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	header, err := cr.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("reading the header line: %w", err)
	}
	pi := columnIndex(header, "part", "part_num", "part number", "partnum")
	ci := columnIndex(header, "color", "color_id", "colour", "colour_id")
	qi := columnIndex(header, "quantity", "qty")
	si := columnIndex(header, "is_spare", "is spare", "spare")
	if pi < 0 || qi < 0 {
		return nil, nil, errors.New("this does not look like a Rebrickable parts list: it needs Part (or part_num) and Quantity columns")
	}
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		field := func(i int) string {
			if i < 0 || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		if si >= 0 && truthy(field(si)) {
			continue
		}
		qty, qerr := strconv.Atoi(field(qi))
		part := field(pi)
		if perr := CheckPartNum(part); part != "" && perr != nil {
			problems = append(problems, fmt.Sprintf("line %d: %v", line, perr))
			continue
		}
		if part == "" || qerr != nil || qty < 0 {
			problems = append(problems, fmt.Sprintf("line %d: needs a part number and a whole quantity (got %q, %q)", line, part, field(qi)))
			continue
		}
		row := ImportRow{Line: line, PartNum: part, ColorID: NoColor, Qty: qty}
		if c := field(ci); c != "" {
			if id, err := strconv.Atoi(c); err == nil {
				if known, ok := colors.ByID[id]; ok {
					row.ColorID, row.ColorName = known.ID, known.Name
				} else {
					problems = append(problems, fmt.Sprintf("line %d: unknown Rebrickable colour id %d — kept as typed text", line, id))
					row.ColorName = "colour " + c
				}
			} else if known, ok := colors.ByName[strings.ToLower(c)]; ok {
				row.ColorID, row.ColorName = known.ID, known.Name
			} else {
				row.ColorName = c
			}
		}
		rows = append(rows, row)
	}
	return rows, problems, nil
}

type blItem struct {
	ItemType string `xml:"ITEMTYPE"`
	ItemID   string `xml:"ITEMID"`
	Color    string `xml:"COLOR"`
	Qty      string `xml:"QTY"`
	MinQty   string `xml:"MINQTY"`
}

// ParseBrickLinkXML reads a BrickLink inventory or wanted-list file (<INVENTORY>
// of <ITEM>s). Only parts (ITEMTYPE P) are imported. BrickLink colour numbers
// are turned into Rebrickable colours through colors.ByBL, which needs the live
// colour table; an unmapped colour is reported and kept as typed text.
func ParseBrickLinkXML(r io.Reader, colors *ColorTable) (rows []ImportRow, problems []string, err error) {
	var inv struct {
		Items []blItem `xml:"ITEM"`
	}
	if err := xml.NewDecoder(r).Decode(&inv); err != nil {
		return nil, nil, fmt.Errorf("this is not a BrickLink inventory XML file: %w", err)
	}
	if len(inv.Items) == 0 {
		return nil, nil, errors.New("the file has no <ITEM> entries")
	}
	for i, it := range inv.Items {
		line := i + 1
		if t := strings.ToUpper(strings.TrimSpace(it.ItemType)); t != "" && t != "P" {
			problems = append(problems, fmt.Sprintf("item %d (%s %s): only parts are imported, skipped", line, t, it.ItemID))
			continue
		}
		q := strings.TrimSpace(it.Qty)
		if q == "" {
			q = strings.TrimSpace(it.MinQty)
		}
		qty, qerr := strconv.Atoi(q)
		id := strings.TrimSpace(it.ItemID)
		if id == "" || qerr != nil || qty < 0 {
			problems = append(problems, fmt.Sprintf("item %d: needs an ITEMID and a whole QTY (got %q, %q)", line, id, q))
			continue
		}
		row := ImportRow{Line: line, PartNum: id, ColorID: NoColor, Qty: qty}
		if c := strings.TrimSpace(it.Color); c != "" && c != "0" {
			bl, _ := strconv.Atoi(c)
			if known, ok := colors.ByBL[bl]; ok {
				row.ColorID, row.ColorName = known.ID, known.Name
			} else {
				problems = append(problems, fmt.Sprintf("item %d: BrickLink colour %s has no Rebrickable match — kept as typed text", line, c))
				row.ColorName = "BrickLink colour " + c
			}
		}
		rows = append(rows, row)
	}
	return rows, problems, nil
}

// PlanItem is one part+colour and what importing it would do.
type PlanItem struct {
	ImportRow
	Name     string
	Category string
	Known    bool // the part is in the offline catalog
	Was      int  // quantity held now (0 when new)
	Exists   bool
	Becomes  int
}

// ImportPlan is everything an import would do, computed without changing anything.
type ImportPlan struct {
	Mode      string // "add" (add to what you hold) or "set" (replace it)
	Items     []PlanItem
	New       int
	Changed   int
	Unchanged int
	Unknown   int // parts the offline catalog does not know
}

// PlanImport merges duplicate lines, names each part from the offline catalog and
// works out the resulting quantities. mode is "add" or "set".
func (d *DB) PlanImport(rows []ImportRow, mode string) (*ImportPlan, error) {
	if mode != "add" && mode != "set" {
		return nil, fmt.Errorf("mode must be add or set, not %q", mode)
	}
	type key struct {
		part  string
		color int
		name  string
	}
	merged := map[key]*PlanItem{}
	var order []key
	for _, r := range rows {
		name := ""
		if r.ColorID < 0 {
			name = r.ColorName
		}
		k := key{strings.ToLower(r.PartNum), r.ColorID, name}
		if it, ok := merged[k]; ok {
			it.Qty += r.Qty
			continue
		}
		merged[k] = &PlanItem{ImportRow: r}
		order = append(order, k)
	}
	plan := &ImportPlan{Mode: mode}
	for _, k := range order {
		it := merged[k]
		if cp, err := d.CatalogPart(it.PartNum); err == nil && cp != nil {
			it.PartNum, it.Name, it.Category, it.Known = cp.Num, cp.Name, cp.Category, true
		} else {
			it.Name = it.PartNum
			plan.Unknown++
		}
		if cur, err := d.GetOwnedPart(it.PartNum, it.ColorID, it.ColorName); err != nil {
			return nil, err
		} else if cur != nil {
			it.Exists, it.Was = true, cur.Qty
			if it.Name == it.PartNum && cur.Name != "" {
				it.Name, it.Category = cur.Name, cur.Category
			}
		}
		it.Becomes = it.Qty
		if mode == "add" {
			it.Becomes = it.Was + it.Qty
		}
		switch {
		case !it.Exists:
			plan.New++
		case it.Becomes != it.Was:
			plan.Changed++
		default:
			plan.Unchanged++
		}
		plan.Items = append(plan.Items, *it)
	}
	return plan, nil
}

// ApplyImport writes the plan in one transaction: either every line lands or none does.
func (d *DB) ApplyImport(plan *ImportPlan) error {
	d.ensureDailySnapshot()
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, it := range plan.Items {
		if it.Exists && it.Becomes == it.Was {
			continue
		}
		if err := addOwned(tx, OwnedPart{PartNum: it.PartNum, Name: it.Name, Category: it.Category, ColorID: it.ColorID, ColorName: it.ColorName, Qty: it.Becomes}); err != nil {
			return err
		}
		d.journal(tx, "import", "part", it.PartNum, it.ColorID, it.ColorName, it.Was, it.Becomes, "bulk import ("+plan.Mode+")")
	}
	return tx.Commit()
}
