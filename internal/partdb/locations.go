package partdb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Storage locations, and stock kept per location. A LEGO set's parts live in their
// own location, "LEGO / Sets / <num> <name>", so a stock check can count a set on
// its own, and the loose-part sync (which sets a part's loose stock) must leave
// those lots alone — see looseLots.

// SetsLocationPath is where each set's own location is created.
var SetsLocationPath = []string{"LEGO", "Sets"}

func (a *API) CreateStorageLocation(ctx context.Context, name string, parentID int) (int, error) {
	body := map[string]any{"name": name}
	if parentID > 0 {
		body["parent"] = fmt.Sprintf("/api/storage_locations/%d", parentID)
	}
	var c created
	if err := a.do(ctx, http.MethodPost, "/api/storage_locations", body, &c); err != nil {
		return 0, err
	}
	return c.id(), nil
}

type location struct {
	ID, Parent int
	Name       string
}

func (w *Writer) locations() ([]location, error) {
	rows, err := w.DB.Query("SELECT id, COALESCE(parent_id, 0), name FROM storelocations")
	if err != nil {
		return nil, fmt.Errorf("reading storage locations: %w", err)
	}
	defer rows.Close()
	var out []location
	for rows.Next() {
		var l location
		if err := rows.Scan(&l.ID, &l.Parent, &l.Name); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (w *Writer) childLocation(parent int, name string) (int, bool, error) {
	ls, err := w.locations()
	if err != nil {
		return 0, false, err
	}
	for _, l := range ls {
		if l.Parent == parent && strings.EqualFold(strings.TrimSpace(l.Name), strings.TrimSpace(name)) {
			return l.ID, true, nil
		}
	}
	return 0, false, nil
}

// ResolveLocation walks path from the top of the location tree, creating any
// missing level, and returns the leaf id (the same rules as ResolveCategory).
func (w *Writer) ResolveLocation(ctx context.Context, path []string) (int, error) {
	parent := 0
	for _, name := range path {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		id, found, err := w.childLocation(parent, name)
		if err != nil {
			return 0, err
		}
		if !found {
			id, err = w.API().CreateStorageLocation(ctx, name, parent)
			if err != nil {
				var ae *APIError
				if errors.As(err, &ae) && ae.Status == 422 {
					if id2, ok, _ := w.childLocation(parent, name); ok {
						parent = id2
						continue
					}
				}
				return 0, fmt.Errorf("creating storage location %q: %w", name, err)
			}
			if id == 0 {
				if id2, ok, _ := w.childLocation(parent, name); ok {
					id = id2
				}
			}
		}
		parent = id
	}
	return parent, nil
}

// setLocationIDs are the locations under LEGO / Sets (each set's own place).
func (w *Writer) setLocationIDs() map[int]bool {
	out := map[int]bool{}
	ls, err := w.locations()
	if err != nil {
		return out
	}
	byID := map[int]location{}
	for _, l := range ls {
		byID[l.ID] = l
	}
	// root = the LEGO/Sets node, if it exists
	root := 0
	for _, l := range ls {
		if strings.EqualFold(l.Name, SetsLocationPath[1]) && l.Parent != 0 && strings.EqualFold(byID[l.Parent].Name, SetsLocationPath[0]) && byID[l.Parent].Parent == 0 {
			root = l.ID
		}
	}
	if root == 0 {
		return out
	}
	for _, l := range ls {
		for p, hops := l.Parent, 0; p != 0 && hops < 32; p, hops = byID[p].Parent, hops+1 {
			if p == root {
				out[l.ID] = true
				break
			}
		}
	}
	return out
}

// looseLots are a part's lots outside the set locations: the stock the loose-part
// sync owns.
func (w *Writer) looseLots(partID int) ([]lot, error) {
	rows, err := w.DB.Query("SELECT id, amount, COALESCE(id_store_location, 0) FROM part_lots WHERE id_part = ? ORDER BY id", partID)
	if err != nil {
		return nil, fmt.Errorf("reading stock lots: %w", err)
	}
	defer rows.Close()
	sets := w.setLocationIDs()
	var out []lot
	for rows.Next() {
		var l lot
		var loc int
		if err := rows.Scan(&l.ID, &l.Amount, &loc); err != nil {
			return nil, err
		}
		if !sets[loc] {
			out = append(out, l)
		}
	}
	return out, rows.Err()
}

// EnsurePart returns the part with spec.IPN, creating it (with no stock) if needed.
func (w *Writer) EnsurePart(ctx context.Context, spec PartSpec) (id int, created bool, err error) {
	api := w.API()
	if !api.Enabled() {
		return 0, false, ErrNoToken
	}
	if id, err = w.DB.FindPartByIPN(spec.IPN); err != nil || id != 0 {
		return id, false, err
	}
	if id, err = api.CreatePart(ctx, spec); err != nil {
		return 0, false, fmt.Errorf("creating part: %w", err)
	}
	if id == 0 {
		if id, _ = w.DB.FindPartByIPN(spec.IPN); id == 0 {
			return 0, false, errors.New("Part-DB created the part but did not say which one")
		}
	}
	return id, true, nil
}

// SetLotAt makes the part hold exactly amount at the location (one lot there,
// created when needed) and returns what it held before.
func (w *Writer) SetLotAt(ctx context.Context, partID, locationID int, amount float64) (prev float64, err error) {
	var lotID int
	err = w.DB.QueryRow("SELECT id, amount FROM part_lots WHERE id_part = ? AND id_store_location = ? ORDER BY id LIMIT 1", partID, locationID).Scan(&lotID, &prev)
	switch {
	case err == nil:
		if prev == amount {
			return prev, nil
		}
		return prev, w.API().SetLotAmount(ctx, lotID, amount)
	case amount <= 0:
		return 0, nil
	}
	_, err = w.API().CreateLot(ctx, partID, amount, locationID, "LEGO set contents")
	return 0, err
}
