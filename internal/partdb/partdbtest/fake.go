package partdbtest

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Fake is an httptest stand-in for Part-DB's REST API that behaves like the
// real one where it matters: bearer token required, JSON content type on
// POST and merge-patch on PATCH, category required on a part, tags a string,
// 422 with a violations list on duplicates, and lots that are NOT creatable
// inline with the part. It writes into the scratch database (real schema), so
// the code under test reads back exactly what Part-DB would have stored.
type Fake struct {
	Server   *httptest.Server
	Token    string
	mu       sync.Mutex
	requests []string
	comments []string
	fail     map[string]int
}

// Fail makes the next requests matching "METHOD /api/path" answer with status
// (a 500-style failure to test compensation), until Fail(..., 0) clears it.
func (f *Fake) Fail(methodAndPath string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail == nil {
		f.fail = map[string]int{}
	}
	if status == 0 {
		delete(f.fail, methodAndPath)
		return
	}
	f.fail[methodAndPath] = status
}

// URL is the base URL to give the client.
func (f *Fake) URL() string { return f.Server.URL }

// Requests returns "METHOD /path" for every request seen, in order.
func (f *Fake) Requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// Comments returns the ?_comment= values seen on write requests.
func (f *Fake) Comments() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.comments...)
}

func NewFake(t testing.TB, dbPath, token string) *Fake {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	f := &Fake{Token: token}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(db, w, r) }))
	t.Cleanup(func() { f.Server.Close(); db.Close() })
	return f
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/ld+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func violation(w http.ResponseWriter, path, msg string) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"title": "An error occurred", "detail": path + ": " + msg,
		"violations": []map[string]string{{"propertyPath": path, "message": msg}},
	})
}

func idFromIRI(iri, prefix string) (int, bool) {
	if !strings.HasPrefix(iri, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(iri, prefix))
	return n, err == nil
}

func (f *Fake) serve(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	if r.Method != http.MethodGet {
		f.comments = append(f.comments, r.URL.Query().Get("_comment"))
	}
	failStatus := f.fail[r.Method+" "+r.URL.Path]
	f.mu.Unlock()

	if failStatus != 0 {
		writeJSON(w, failStatus, map[string]string{"detail": "injected failure"})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+f.Token {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"title": "Unauthorized", "detail": "Invalid credentials."})
		return
	}
	ct := r.Header.Get("Content-Type")
	switch r.Method {
	case http.MethodPost:
		if !strings.HasPrefix(ct, "application/json") && !strings.HasPrefix(ct, "application/ld+json") {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"detail": "unsupported content type"})
			return
		}
	case http.MethodPatch:
		if !strings.HasPrefix(ct, "application/merge-patch+json") {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"detail": "PATCH needs application/merge-patch+json"})
			return
		}
	}
	var body map[string]any
	if r.Method == http.MethodPost || r.Method == http.MethodPatch {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid JSON"})
			return
		}
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	parts := strings.Split(path, "/")

	switch {
	case r.Method == http.MethodGet && parts[0] == "parts":
		writeJSON(w, 200, map[string]any{"hydra:member": []any{}, "hydra:totalItems": 0})

	case r.Method == http.MethodPost && path == "categories":
		f.postCategory(db, w, body)
	case r.Method == http.MethodPost && path == "parts":
		f.postPart(db, w, body)
	case r.Method == http.MethodPost && path == "storage_locations":
		f.postLocation(db, w, body)
	case r.Method == http.MethodPost && path == "part_lots":
		f.postLot(db, w, body)

	case r.Method == http.MethodPatch && len(parts) == 2 && parts[0] == "part_lots":
		id, _ := strconv.Atoi(parts[1])
		if amt, ok := body["amount"].(float64); ok {
			res, err := db.Exec("UPDATE part_lots SET amount = ? WHERE id = ?", amt, id)
			if n, _ := res.RowsAffected(); err != nil || n == 0 {
				writeJSON(w, 404, map[string]string{"detail": "Not Found"})
				return
			}
		}
		writeJSON(w, 200, map[string]any{"@id": "/api/part_lots/" + parts[1], "id": id})
	case r.Method == http.MethodPatch && len(parts) == 2 && parts[0] == "parts":
		id, _ := strconv.Atoi(parts[1])
		for _, col := range []string{"name", "tags", "description"} {
			if v, ok := body[col].(string); ok {
				_, _ = db.Exec("UPDATE parts SET "+col+" = ? WHERE id = ?", v, id)
			}
		}
		if v, ok := body["minamount"].(float64); ok {
			_, _ = db.Exec("UPDATE parts SET minamount = ? WHERE id = ?", v, id)
		}
		writeJSON(w, 200, map[string]any{"@id": "/api/parts/" + parts[1], "id": id})

	case r.Method == http.MethodDelete && len(parts) == 2 && parts[0] == "parts":
		id, _ := strconv.Atoi(parts[1])
		_, _ = db.Exec("DELETE FROM part_lots WHERE id_part = ?", id)
		res, _ := db.Exec("DELETE FROM parts WHERE id = ?", id)
		if n, _ := res.RowsAffected(); n == 0 {
			writeJSON(w, 404, map[string]string{"detail": "Not Found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodDelete && len(parts) == 2 && parts[0] == "part_lots":
		id, _ := strconv.Atoi(parts[1])
		_, _ = db.Exec("DELETE FROM part_lots WHERE id = ?", id)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "Not Found"})
	}
}

func (f *Fake) postCategory(db *sql.DB, w http.ResponseWriter, body map[string]any) {
	name, _ := body["name"].(string)
	if strings.TrimSpace(name) == "" {
		violation(w, "name", "This value should not be blank.")
		return
	}
	var parent any
	if iri, ok := body["parent"].(string); ok {
		if id, ok := idFromIRI(iri, "/api/categories/"); ok {
			parent = id
		}
	}
	var dup int
	if err := db.QueryRow("SELECT COUNT(*) FROM categories WHERE LOWER(name) = LOWER(?) AND ((? IS NULL AND parent_id IS NULL) OR parent_id = ?)", name, parent, parent).Scan(&dup); err == nil && dup > 0 {
		violation(w, "name", "This value is already used.")
		return
	}
	res, err := db.Exec(`INSERT INTO categories (name, parent_id, partname_hint, partname_regex, disable_footprints, disable_manufacturers,
		disable_autodatasheets, disable_properties, default_description, default_comment, comment, not_selectable)
		VALUES (?, ?, '', '', 0, 0, 0, 0, '', '', '', 0)`, name, parent)
	if err != nil {
		writeJSON(w, 500, map[string]string{"detail": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, 201, map[string]any{"@id": "/api/categories/" + strconv.FormatInt(id, 10), "id": id, "name": name})
}

func (f *Fake) postPart(db *sql.DB, w http.ResponseWriter, body map[string]any) {
	name, _ := body["name"].(string)
	if strings.TrimSpace(name) == "" {
		violation(w, "name", "This value should not be blank.")
		return
	}
	catIRI, _ := body["category"].(string)
	catID, ok := idFromIRI(catIRI, "/api/categories/")
	if !ok {
		violation(w, "category", "This value should not be null.")
		return
	}
	tags := ""
	if v, present := body["tags"]; present {
		s, isStr := v.(string)
		if !isStr {
			writeJSON(w, 400, map[string]string{"detail": "tags must be a string"})
			return
		}
		tags = s
	}
	desc, _ := body["description"].(string)
	var ipn any
	if s, _ := body["ipn"].(string); s != "" {
		ipn = s
		var dup int
		if err := db.QueryRow("SELECT COUNT(*) FROM parts WHERE ipn = ?", s).Scan(&dup); err == nil && dup > 0 {
			violation(w, "ipn", "This IPN is already used by another part.")
			return
		}
	}
	mpn, _ := body["manufacturer_product_number"].(string)
	minAmount, _ := body["minamount"].(float64)
	res, err := db.Exec(`INSERT INTO parts (id_category, datetime_added, name, last_modified, needs_review, tags, description, comment,
		visible, favorite, minamount, manufacturer_product_url, manufacturer_product_number, order_quantity, manual_order, ipn)
		VALUES (?, datetime('now'), ?, datetime('now'), 0, ?, ?, '', 1, 0, ?, '', ?, 0, 0, ?)`, catID, name, tags, desc, minAmount, mpn, ipn)
	if err != nil {
		writeJSON(w, 500, map[string]string{"detail": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, 201, map[string]any{"@id": "/api/parts/" + strconv.FormatInt(id, 10), "id": id, "name": name})
}

func (f *Fake) postLot(db *sql.DB, w http.ResponseWriter, body map[string]any) {
	partID, ok := idFromIRI(str(body["part"]), "/api/parts/")
	if !ok {
		violation(w, "part", "This value should not be null.")
		return
	}
	amount, ok := body["amount"].(float64)
	if !ok {
		violation(w, "amount", "This value should be a number.")
		return
	}
	var loc any
	if id, ok := idFromIRI(str(body["storage_location"]), "/api/storage_locations/"); ok {
		loc = id
	}
	desc, _ := body["description"].(string)
	res, err := db.Exec(`INSERT INTO part_lots (id_part, id_store_location, amount, needs_refill, vendor_barcode, datetime_added,
		description, comment, instock_unknown) VALUES (?, ?, ?, 0, '', datetime('now'), ?, '', 0)`, partID, loc, amount, desc)
	if err != nil {
		writeJSON(w, 500, map[string]string{"detail": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, 201, map[string]any{"@id": "/api/part_lots/" + strconv.FormatInt(id, 10), "id": id})
}

func (f *Fake) postLocation(db *sql.DB, w http.ResponseWriter, body map[string]any) {
	name, _ := body["name"].(string)
	if strings.TrimSpace(name) == "" {
		violation(w, "name", "This value should not be blank.")
		return
	}
	var parent any
	if id, ok := idFromIRI(str(body["parent"]), "/api/storage_locations/"); ok {
		parent = id
	}
	res, err := db.Exec(`INSERT INTO storelocations (name, parent_id, is_full, only_single_part, limit_to_existing_parts, comment, not_selectable)
		VALUES (?, ?, 0, 0, 0, '', 0)`, name, parent)
	if err != nil {
		writeJSON(w, 500, map[string]string{"detail": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, 201, map[string]any{"@id": "/api/storage_locations/" + strconv.FormatInt(id, 10), "id": id, "name": name})
}

func str(v any) string { s, _ := v.(string); return s }
