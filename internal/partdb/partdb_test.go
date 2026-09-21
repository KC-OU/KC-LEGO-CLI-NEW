package partdb

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb/partdbtest"
)

const testToken = "tcp_test_token"

type fixture struct {
	DB   *DB
	W    *Writer
	Fake *partdbtest.Fake
}

// newFixture is a real-schema scratch Part-DB plus a fake REST API in front of
// it, and a Writer wired to both. Nothing here can touch the live database.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	path := partdbtest.New(t)
	partdbtest.Guard(t, path)
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	fake := partdbtest.NewFake(t, path, testToken)
	w := &Writer{DB: db, API: func() *API {
		return &API{BaseURL: fake.URL(), Token: testToken, HTTP: fake.Server.Client(), Comment: "wms-go"}
	}}
	return &fixture{DB: db, W: w, Fake: fake}
}

func bg() context.Context { return context.Background() }

func TestUpsertCreatesNestedCategoryPartAndStock(t *testing.T) {
	f := newFixture(t)
	catID, err := f.W.ResolveCategory(bg(), []string{"Lego", "Bricks"})
	if err != nil {
		t.Fatalf("ResolveCategory: %v", err)
	}
	res, err := f.W.UpsertPart(bg(), PartSpec{Name: "Brick 2 x 4 - Red", IPN: "3001-4", Tags: "lego,Red", CategoryID: catID}, 10)
	if err != nil {
		t.Fatalf("UpsertPart: %v", err)
	}
	if !res.Created || res.NewQty != 10 {
		t.Fatalf("unexpected result %+v", res)
	}

	rows, err := f.DB.SearchParts("Brick")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Brick 2 x 4 - Red" || rows[0].StockQty != 10 || rows[0].Category != "Bricks" {
		t.Fatalf("search results: %+v", rows)
	}
	// Lego > Bricks is a real tree, not one category named "Lego/Bricks".
	cats, _ := f.DB.AllCategories()
	var lego, bricks *Category
	for i := range cats {
		switch cats[i].Name {
		case "Lego":
			lego = &cats[i]
		case "Bricks":
			bricks = &cats[i]
		}
	}
	if lego == nil || bricks == nil || lego.ParentID.Valid || !bricks.ParentID.Valid || int(bricks.ParentID.Int64) != lego.ID {
		t.Fatalf("expected Lego > Bricks, got %+v / %+v", lego, bricks)
	}
	for _, c := range f.Fake.Comments() {
		if c != "wms-go" {
			t.Errorf("every write should carry ?_comment=wms-go, got %q", c)
		}
	}

	if err := res.Undo(); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if rows, _ := f.DB.SearchParts("Brick"); len(rows) != 0 {
		t.Fatalf("undo should delete the part, got %+v", rows)
	}
}

func TestUpsertExistingPartOnlyChangesStock(t *testing.T) {
	f := newFixture(t)
	spec := PartSpec{Name: "Plate 1 x 2 - Blue", IPN: "3023-1", CategoryID: fallbackCategoryID}
	first, err := f.W.UpsertPart(bg(), spec, 5)
	if err != nil {
		t.Fatal(err)
	}
	before := len(f.Fake.Requests())

	res, err := f.W.UpsertPart(bg(), spec, 8)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || res.PartID != first.PartID || res.PrevQty != 5 || res.NewQty != 8 {
		t.Fatalf("expected an update of the same part: %+v", res)
	}
	for _, r := range f.Fake.Requests()[before:] {
		if strings.HasPrefix(r, "POST /api/parts") {
			t.Fatalf("an existing IPN must not create a second part: %v", f.Fake.Requests()[before:])
		}
	}
	if got, _ := f.W.Stock(res.PartID); got != 8 {
		t.Errorf("stock = %v, want 8", got)
	}
	if err := res.Undo(); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.W.Stock(res.PartID); got != 5 {
		t.Errorf("undo should restore 5, got %v", got)
	}
}

func TestUpsertRollsBackThePartWhenTheLotFails(t *testing.T) {
	f := newFixture(t)
	f.Fake.Fail("POST /api/part_lots", 500)
	_, err := f.W.UpsertPart(bg(), PartSpec{Name: "Half made", IPN: "X-1", CategoryID: fallbackCategoryID}, 3)
	if err == nil {
		t.Fatal("expected an error when the stock lot can't be created")
	}
	if rows, _ := f.DB.SearchParts("Half made"); len(rows) != 0 {
		t.Fatalf("a failed stock write must not leave a part behind: %+v", rows)
	}
}

func TestDuplicateIPNSurfacesTheValidationMessage(t *testing.T) {
	f := newFixture(t)
	a := f.W.API()
	if _, err := a.CreatePart(bg(), PartSpec{Name: "One", IPN: "DUP", CategoryID: fallbackCategoryID}); err != nil {
		t.Fatal(err)
	}
	_, err := a.CreatePart(bg(), PartSpec{Name: "Two", IPN: "DUP", CategoryID: fallbackCategoryID})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 422 || !strings.Contains(ae.Detail, "ipn") {
		t.Fatalf("expected a 422 naming the ipn, got %v", err)
	}
}

func TestNoTokenAndBadTokenAreClearErrors(t *testing.T) {
	f := newFixture(t)
	f.W.API = func() *API { return &API{BaseURL: f.Fake.URL(), Token: "", HTTP: f.Fake.Server.Client()} }
	if _, err := f.W.UpsertPart(bg(), PartSpec{Name: "x", CategoryID: 7}, 1); !errors.Is(err, ErrNoToken) {
		t.Fatalf("no token: got %v", err)
	}
	if _, _, err := f.W.AdjustStock(1, 1); !errors.Is(err, ErrNoToken) {
		t.Fatalf("no token on adjust: got %v", err)
	}

	f.W.API = func() *API { return &API{BaseURL: f.Fake.URL(), Token: "wrong", HTTP: f.Fake.Server.Client()} }
	err := f.W.API().Ping(bg())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 401 || !strings.Contains(ae.Error(), "rejected the API token") {
		t.Fatalf("bad token: got %v", err)
	}
	if err := f.W.API().Ping(bg()); err == nil {
		t.Fatal("ping with a wrong token must fail")
	}
}

func TestResolveCategoryMatchesUnderTheRightParent(t *testing.T) {
	f := newFixture(t)
	// Two categories called "Bricks" under different parents, as the live data has.
	a, _ := f.W.ResolveCategory(bg(), []string{"Tools", "Bricks"})
	b, _ := f.W.ResolveCategory(bg(), []string{"Lego", "Bricks"})
	if a == 0 || b == 0 || a == b {
		t.Fatalf("expected two distinct Bricks categories, got %d and %d", a, b)
	}
	again, _ := f.W.ResolveCategory(bg(), []string{"lego", "BRICKS"}) // case-insensitive, idempotent
	if again != b {
		t.Errorf("resolving Lego > Bricks again must find the same category (%d), got %d", b, again)
	}
	before := len(f.Fake.Requests())
	_, _ = f.W.ResolveCategory(bg(), []string{"Lego", "Bricks"})
	if len(f.Fake.Requests()) != before {
		t.Error("an existing category path must not call the API at all")
	}
	if id, _ := f.W.ResolveCategory(bg(), nil); id != fallbackCategoryID {
		t.Errorf("empty path should be the built-in (Other) category, got %d", id)
	}
}

func TestAdjustStock(t *testing.T) {
	f := newFixture(t)
	res, err := f.W.UpsertPart(bg(), PartSpec{Name: "Bolt", IPN: "B-1", CategoryID: fallbackCategoryID}, 5)
	if err != nil {
		t.Fatal(err)
	}
	newAmt, undo, err := f.W.AdjustStock(res.PartID, 3)
	if err != nil || newAmt != 8 {
		t.Fatalf("AdjustStock +3: %v %v", newAmt, err)
	}
	newAmt, _, err = f.W.AdjustStock(res.PartID, -100)
	if err != nil || newAmt != 0 {
		t.Fatalf("expected clamp to 0, got %v %v", newAmt, err)
	}
	if err := undo(); err != nil { // the first adjustment's undo restores the value before it
		t.Fatal(err)
	}
	if got, _ := f.W.Stock(res.PartID); got != 5 {
		t.Errorf("undo of the first adjustment should restore 5, got %v", got)
	}

	// A part with no lot yet gets one.
	bare, err := f.W.UpsertPart(bg(), PartSpec{Name: "No stock yet", IPN: "N-1", CategoryID: fallbackCategoryID}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if newAmt, _, err = f.W.AdjustStock(bare.PartID, 4); err != nil || newAmt != 4 {
		t.Fatalf("adjusting a part with no lot: %v %v", newAmt, err)
	}
	if got, _ := f.W.Stock(bare.PartID); got != 4 {
		t.Errorf("stock = %v, want 4", got)
	}
}

func insertTestUser(t *testing.T, db *DB, id int, name string, group int) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO users (id, name, email, password, group_id, disabled, need_pw_change, trusted_device_cookie_version,
		backup_codes, settings, saml_user, permissions_data, config_instock_comment_a, config_instock_comment_w, about_me)
		VALUES (?, ?, ?, 'hash', ?, 0, 0, 1, '[]', '[]', 0, '[]', '', '', '')`, id, name, name+"@example.com", group)
	if err != nil {
		t.Fatalf("seeding user: %v", err)
	}
}

func TestPartDBUserCRUD(t *testing.T) {
	f := newFixture(t)
	db := f.DB
	insertTestUser(t, db, 1, "kcollins", 1)
	insertTestUser(t, db, 2, "seconduser", 1)

	if _, err := db.CreateUser("picker1", "Picker", "picker1@example.com", "bcrypthash", true); err != nil {
		t.Fatalf("CreateUser on the real schema: %v", err)
	}
	users, err := db.ListUsers()
	if err != nil || len(users) != 3 {
		t.Fatalf("ListUsers: %d users, %v", len(users), err)
	}
	if _, err := db.CreateUser("picker1", "Picker", "x@example.com", "h", true); err == nil {
		t.Error("expected duplicate create to fail")
	}
	role := "Admin"
	if err := db.ModifyUser("picker1", &role, nil, nil); err != nil {
		t.Fatalf("ModifyUser: %v", err)
	}
	if u, _ := db.GetUserByName("picker1"); u.GroupID != 1 {
		t.Errorf("expected group_id 1 after promoting to admin, got %d", u.GroupID)
	}
	if err := db.ToggleActive("picker1", false); err != nil {
		t.Fatal(err)
	}
	if u, _ := db.GetUserByName("picker1"); !u.Disabled {
		t.Error("expected user to be disabled")
	}
	if err := db.DeleteUser("kcollins"); err == nil {
		t.Error("expected delete of protected account kcollins to fail")
	}
	if err := db.DeleteUser("seconduser"); err == nil {
		t.Error("expected delete of protected low-id account to fail")
	}
	if err := db.DeleteUser("picker1"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if users, _ = db.ListUsers(); len(users) != 2 {
		t.Fatalf("expected 2 users after delete, got %d", len(users))
	}
}

var sqlLiteralRE = regexp.MustCompile("\\.(?:Query|QueryRow|Exec)\\(\\s*(`[^`]*`|\"(?:[^\"\\\\]|\\\\.)*\")")

// TestSQLMatchesPartDBSchema compiles every SQL statement in this package
// against the real Part-DB structure (partdbtest/schema.sql). EXPLAIN catches a
// missing table or column; the writes that remain here (users) are also
// executed in TestPartDBUserCRUD, because EXPLAIN cannot see a NOT NULL column
// with no default — the failure that broke part, category and lot inserts.
func TestSQLMatchesPartDBSchema(t *testing.T) {
	db, err := Open(partdbtest.New(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	files, _ := filepath.Glob("*.go")
	checked := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(path)
		for _, m := range sqlLiteralRE.FindAllStringSubmatch(string(src), -1) {
			stmt := strings.Trim(m[1], "`\"")
			stmt = strings.ReplaceAll(stmt, `\"`, `"`)
			args := make([]any, strings.Count(stmt, "?"))
			rows, err := db.Query("EXPLAIN "+stmt, args...)
			if err != nil {
				t.Errorf("%s: %v\n   %s", path, err, strings.Join(strings.Fields(stmt), " "))
				continue
			}
			rows.Close()
			checked++
		}
	}
	if checked < 15 {
		t.Fatalf("only %d statements were extracted; the extraction pattern needs updating", checked)
	}
	t.Logf("%d statements compile against the Part-DB schema", checked)
}

func TestMinAmountIsCreatedAndUpdatedOnlyWhenItDiffers(t *testing.T) {
	f := newFixture(t)
	five, seven := 5.0, 7.0
	spec := PartSpec{Name: "Brick 2 x 4 - Red", IPN: "3001-4", CategoryID: fallbackCategoryID, MinAmount: &five}
	res, err := f.W.UpsertPart(bg(), spec, 10)
	if err != nil {
		t.Fatal(err)
	}
	minOf := func() float64 {
		var m float64
		if err := f.DB.QueryRow("SELECT minamount FROM parts WHERE id = ?", res.PartID).Scan(&m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if minOf() != 5 {
		t.Fatalf("a new part is created with the minimum, got %v", minOf())
	}
	patches := func() int {
		n := 0
		for _, r := range f.Fake.Requests() {
			if strings.HasPrefix(r, "PATCH /api/parts/") {
				n++
			}
		}
		return n
	}
	if _, err := f.W.UpsertPart(bg(), spec, 10); err != nil {
		t.Fatal(err)
	}
	if patches() != 0 {
		t.Errorf("an unchanged minimum must cost no PATCH, saw %d", patches())
	}
	spec.MinAmount = &seven
	if _, err := f.W.UpsertPart(bg(), spec, 10); err != nil {
		t.Fatal(err)
	}
	if minOf() != 7 || patches() != 1 {
		t.Errorf("a changed minimum is patched once: min=%v patches=%d", minOf(), patches())
	}
	spec.MinAmount = nil // a part managed without a minimum keeps whatever Part-DB has
	if _, err := f.W.UpsertPart(bg(), spec, 10); err != nil {
		t.Fatal(err)
	}
	if minOf() != 7 || patches() != 1 {
		t.Errorf("nil leaves the minimum alone: min=%v patches=%d", minOf(), patches())
	}
}

func TestOpenWaitsForPartDBsOwnWriteLockInsteadOfFailing(t *testing.T) {
	path := partdbtest.New(t)
	holder, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	conn, err := holder.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(700 * time.Millisecond)
		_, _ = conn.ExecContext(context.Background(), "COMMIT")
	}()
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Part-DB holds its lock only briefly; opening must wait, not fail: %v", err)
	}
	db.Close()
}
