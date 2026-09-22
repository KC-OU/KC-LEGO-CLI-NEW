package partdb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// fallbackCategoryID is Part-DB's built-in "(Other)" category, used when a
// part is created without one.
const fallbackCategoryID = 7

// Writer creates and changes parts: reads come from the SQLite file, writes go
// through the REST API. API is a func so the token is re-read on every use.
type Writer struct {
	DB  *DB
	API func() *API
}

func NewWriter(db *DB) *Writer { return &Writer{DB: db, API: NewAPI} }

// Ctx returns the bounded context every Writer call should run under; the TUI
// calls these synchronously, so a hung Part-DB must not freeze it for long.
func (w *Writer) Ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func (w *Writer) childCategory(parentID int, name string) (int, bool, error) {
	cats, err := w.DB.AllCategories()
	if err != nil {
		return 0, false, err
	}
	for _, c := range cats {
		sameParent := (parentID == 0 && !c.ParentID.Valid) || (c.ParentID.Valid && int(c.ParentID.Int64) == parentID)
		if sameParent && strings.EqualFold(strings.TrimSpace(c.Name), strings.TrimSpace(name)) {
			return c.ID, true, nil
		}
	}
	return 0, false, nil
}

// ResolveCategory walks path ("Lego", "Bricks") from the top of the category
// tree, creating any missing level through the API, and returns the leaf id.
// Matching is by name under a specific parent — never by name alone, since the
// live database has many categories that share a name under different parents.
// An empty path means the built-in "(Other)" category.
func (w *Writer) ResolveCategory(ctx context.Context, path []string) (int, error) {
	if len(path) == 0 {
		return fallbackCategoryID, nil
	}
	parent := 0
	for _, name := range path {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		id, found, err := w.childCategory(parent, name)
		if err != nil {
			return 0, err
		}
		if !found {
			api := w.API()
			id, err = api.CreateCategory(ctx, name, parent)
			if err != nil {
				// Two sessions can create the same category at once: a 422
				// duplicate means it exists now, so read it back.
				var ae *APIError
				if errors.As(err, &ae) && ae.Status == 422 {
					if id2, ok, _ := w.childCategory(parent, name); ok {
						parent = id2
						continue
					}
				}
				return 0, fmt.Errorf("creating category %q: %w", name, err)
			}
			if id == 0 { // response without an id: find it in the database
				if id2, ok, _ := w.childCategory(parent, name); ok {
					id = id2
				}
			}
		}
		parent = id
	}
	if parent == 0 {
		return fallbackCategoryID, nil
	}
	return parent, nil
}

type lot struct {
	ID     int
	Amount float64
}

func (w *Writer) lots(partID int) ([]lot, error) {
	rows, err := w.DB.Query("SELECT id, amount FROM part_lots WHERE id_part = ? ORDER BY id", partID)
	if err != nil {
		return nil, fmt.Errorf("reading stock lots: %w", err)
	}
	defer rows.Close()
	var out []lot
	for rows.Next() {
		var l lot
		if err := rows.Scan(&l.ID, &l.Amount); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// defaultLocation is the lowest-numbered storage location (0 if there is none),
// where new stock goes — the original code hardcoded id 1.
func (w *Writer) defaultLocation() int {
	var id int
	if err := w.DB.QueryRow("SELECT id FROM storelocations ORDER BY id LIMIT 1").Scan(&id); err != nil {
		return 0
	}
	return id
}

// Stock returns a part's total stock across all lots.
func (w *Writer) Stock(partID int) (float64, error) {
	ls, err := w.lots(partID)
	if err != nil {
		return 0, err
	}
	var total float64
	for _, l := range ls {
		total += l.Amount
	}
	return total, nil
}

// UpsertResult describes what UpsertPart did, with an undo for the TUI's F9.
type UpsertResult struct {
	PartID  int
	Created bool
	PrevQty float64
	NewQty  float64
	Undo    func() error
}

// UpsertPart makes the part with spec.IPN exist with exactly qty in stock: it
// creates the part (and a lot) when the IPN is new, or sets the stock of the
// existing part. A part with no IPN is always created. If the stock lot can't
// be created the part is deleted again, so a failure never leaves a half-made part.
func (w *Writer) UpsertPart(ctx context.Context, spec PartSpec, qty float64) (*UpsertResult, error) {
	api := w.API()
	if !api.Enabled() {
		return nil, ErrNoToken
	}
	if qty < 0 {
		qty = 0
	}
	existing, err := w.DB.FindPartByIPN(spec.IPN)
	if err != nil {
		return nil, err
	}

	if existing == 0 {
		partID, err := api.CreatePart(ctx, spec)
		if err != nil {
			return nil, fmt.Errorf("creating part: %w", err)
		}
		if partID == 0 { // response without an id: find it by IPN
			if partID, _ = w.DB.FindPartByIPN(spec.IPN); partID == 0 {
				return nil, errors.New("Part-DB created the part but did not say which one")
			}
		}
		if qty > 0 {
			if _, err := api.CreateLot(ctx, partID, qty, w.defaultLocation(), ""); err != nil {
				_ = api.DeletePart(ctx, partID) // compensate: no half-made part
				return nil, fmt.Errorf("creating stock: %w", err)
			}
		}
		return &UpsertResult{PartID: partID, Created: true, NewQty: qty, Undo: func() error {
			c, cancel := w.Ctx()
			defer cancel()
			return w.API().DeletePart(c, partID)
		}}, nil
	}

	if spec.MinAmount != nil { // only when it differs, so a re-sync costs no extra call
		var cur float64
		if err := w.DB.QueryRow("SELECT minamount FROM parts WHERE id = ?", existing).Scan(&cur); err == nil && cur != *spec.MinAmount {
			if err := api.UpdatePart(ctx, existing, map[string]any{"minamount": *spec.MinAmount}); err != nil {
				return nil, fmt.Errorf("setting the minimum amount: %w", err)
			}
		}
	}
	prev, undo, err := w.setStock(ctx, existing, func(cur float64) float64 { return qty })
	if err != nil {
		return nil, err
	}
	return &UpsertResult{PartID: existing, PrevQty: prev, NewQty: qty, Undo: undo}, nil
}

// setStock changes a part's stock to next(currentTotal) by adjusting its first
// lot (creating one if there is none) and returns the previous total plus an undo.
func (w *Writer) setStock(ctx context.Context, partID int, next func(cur float64) float64) (prev float64, undo func() error, err error) {
	api := w.API()
	if !api.Enabled() {
		return 0, nil, ErrNoToken
	}
	ls, err := w.looseLots(partID) // a set's own lots are not the loose stock this changes
	if err != nil {
		return 0, nil, err
	}
	for _, l := range ls {
		prev += l.Amount
	}
	target := next(prev)
	if target < 0 {
		target = 0
	}

	if len(ls) == 0 {
		if target == 0 {
			return prev, func() error { return nil }, nil
		}
		lotID, err := api.CreateLot(ctx, partID, target, w.defaultLocation(), "")
		if err != nil {
			return 0, nil, fmt.Errorf("creating stock: %w", err)
		}
		return prev, func() error {
			c, cancel := w.Ctx()
			defer cancel()
			return w.API().DeleteLot(c, lotID)
		}, nil
	}

	first := ls[0]
	newFirst := first.Amount + (target - prev)
	if newFirst < 0 {
		newFirst = 0
	}
	if newFirst == first.Amount { // already right: no API call, nothing to undo
		return prev, func() error { return nil }, nil
	}
	if err := api.SetLotAmount(ctx, first.ID, newFirst); err != nil {
		return 0, nil, fmt.Errorf("changing stock: %w", err)
	}
	old := first.Amount
	return prev, func() error {
		c, cancel := w.Ctx()
		defer cancel()
		return w.API().SetLotAmount(c, first.ID, old)
	}, nil
}

// AdjustStock changes a part's stock by delta (never below 0), like the
// TUI's "Receive / Adjust Stock" screen. The returned undo restores it.
func (w *Writer) AdjustStock(partID int, delta float64) (newAmount float64, undo func() error, err error) {
	ctx, cancel := w.Ctx()
	defer cancel()
	prev, undo, err := w.setStock(ctx, partID, func(cur float64) float64 { return cur + delta })
	if err != nil {
		return 0, nil, err
	}
	newAmount = prev + delta
	if newAmount < 0 {
		newAmount = 0
	}
	return newAmount, undo, nil
}
