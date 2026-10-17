package lego

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The live bag-scan gate: before marking a part, the checker scans the
// physical bag it's actually in; a scan that doesn't match gets a warning,
// not a hard block (see set_check.go's activeBag and mobileapi's
// confirmRequest.BagCode). This file holds the shared pieces both surfaces
// use: the bag_kinds registry, per-check bag instances (check_bags), and the
// code format ("<set>-<instance letter>") that lets a fresh label be
// resolved without a database round trip before it's ever been scanned in.

// BagKind is one row of the global bag_kinds registry (small/main/extra are
// built in — see BagSmall/BagMain/BagExtra; an admin can add more).
type BagKind struct {
	ID        int64
	Key, Name string
}

// defaultBagLabel is each built-in kind's fixed instance letter — small and
// main read naturally as A/B (the original two-bag split), extra is always
// E regardless of how many custom kinds exist between them, so it never
// looks like it was just the next one in a boring sequence.
var defaultBagLabel = map[string]string{BagSmall: "A", BagMain: "B", BagExtra: "E"}

// BagKinds lists every registered bag kind.
func (d *DB) BagKinds() ([]BagKind, error) {
	rows, err := d.Query(`SELECT id, key, name FROM bag_kinds ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BagKind
	for rows.Next() {
		var k BagKind
		if err := rows.Scan(&k.ID, &k.Key, &k.Name); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// BagKindByKey looks up one registered kind, nil if key isn't registered.
func (d *DB) BagKindByKey(key string) (*BagKind, error) {
	var k BagKind
	err := d.QueryRow(`SELECT id, key, name FROM bag_kinds WHERE key = ?`, key).Scan(&k.ID, &k.Key, &k.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &k, err
}

// AddBagKind registers a new bag kind (e.g. "wheels") beyond the three built
// in. Caller enforces admin-only — same permission check every other admin
// action here uses (checkAdmin/requireAdmin), not a fresh login.
func (d *DB) AddBagKind(key, name, by string) (*BagKind, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	name = strings.TrimSpace(name)
	if key == "" || name == "" {
		return nil, errors.New("a key and a name are required")
	}
	if _, err := d.Exec(`INSERT INTO bag_kinds (key, name, created_by, created_at) VALUES (?,?,?,?)`,
		key, name, by, time.Now().UTC().Format(timeLayout)); err != nil {
		return nil, fmt.Errorf("adding bag kind %q: %w", key, err)
	}
	return d.BagKindByKey(key)
}

// RequiredBagKind is which bag kind a line's part needs for the given action.
// "extra" always means the dedicated extra bag (a spare doesn't belong
// wherever the part's own normal bag is); every other action (have_all,
// missing, have, damaged) means the part's own small/main/custom placement.
func (d *DB) RequiredBagKind(l CheckLine, action string) (*BagKind, error) {
	if action == "extra" {
		return d.BagKindByKey(BagExtra)
	}
	size, err := d.PartBagSize(l.PartNum)
	if err != nil {
		return nil, err
	}
	return d.BagKindByKey(size)
}

// RequiredBagKinds is every distinct kind c's lines actually need — the
// small/main (or custom) kinds its parts are placed in, plus the extra kind
// too but only if at least one line has already been marked Extra. This is
// what the finish-gate requires scanned, not a blanket "every kind that
// exists" — a set with nothing flagged small never needs a small bag.
func (d *DB) RequiredBagKinds(c *SetCheck) ([]BagKind, error) {
	seen := map[int64]BagKind{}
	needsExtra := false
	for _, l := range c.Lines {
		k, err := d.RequiredBagKind(l, "have")
		if err != nil {
			return nil, err
		}
		if k != nil {
			seen[k.ID] = *k
		}
		if l.Extra > 0 {
			needsExtra = true
		}
	}
	if needsExtra {
		if k, _ := d.BagKindByKey(BagExtra); k != nil {
			seen[k.ID] = *k
		}
	}
	out := make([]BagKind, 0, len(seen))
	for _, k := range seen {
		out = append(out, k)
	}
	return out, nil
}

// CheckBag is one physical bag instance registered against a check.
type CheckBag struct {
	ID                   int64
	CheckID              int64
	BagKindID            int64
	BagKindKey           string
	InstanceLabel, Code  string
	ScannedAt            time.Time // zero = registered/printed but not yet scanned in this session
	CreatedBy, CreatedAt string
}

func scanCheckBag(row interface{ Scan(...any) error }) (*CheckBag, error) {
	var b CheckBag
	var scanned string
	if err := row.Scan(&b.ID, &b.CheckID, &b.BagKindID, &b.BagKindKey, &b.InstanceLabel, &b.Code, &scanned, &b.CreatedBy, &b.CreatedAt); err != nil {
		return nil, err
	}
	b.ScannedAt, _ = time.Parse(timeLayout, scanned)
	return &b, nil
}

const checkBagCols = `cb.id, cb.check_id, cb.bag_kind_id, bk.key, cb.instance_label, cb.code, cb.scanned_at, cb.created_by, cb.created_at`

// BagCode is the deterministic code a fresh (non-pool) bag label carries —
// "<set>-<instance letter>", e.g. "75192-1-A". Computed from the set number
// and kind alone, with no database read: printing a label and actually
// starting a check are separate moments (see EnsureCheckBag), but the code
// on the label is decided the instant it's printed.
func BagCode(setNum, kindKey string) string {
	return setNum + "-" + instanceLabelFor(kindKey, 1)
}

func instanceLabelFor(kindKey string, n int) string {
	base, ok := defaultBagLabel[kindKey]
	if !ok {
		base = strings.ToUpper(kindKey[:1])
	}
	if n <= 1 {
		return base
	}
	return fmt.Sprintf("%s%d", base, n)
}

// EnsureCheckBag returns checkID's instance of kindKey, creating it (as not-
// yet-scanned) the first time it's needed — printing that kind's label, or
// (for extra) the first line in the session marked Extra. Idempotent: a
// second call for the same check+kind returns the same row.
func (d *DB) EnsureCheckBag(checkID int64, setNum, kindKey, by string) (*CheckBag, error) {
	k, err := d.BagKindByKey(kindKey)
	if err != nil {
		return nil, err
	}
	if k == nil {
		return nil, fmt.Errorf("%q isn't a registered bag kind", kindKey)
	}
	if existing, err := scanCheckBag(d.QueryRow(`SELECT `+checkBagCols+` FROM check_bags cb JOIN bag_kinds bk ON bk.id = cb.bag_kind_id
		WHERE cb.check_id = ? AND cb.bag_kind_id = ? ORDER BY cb.id LIMIT 1`, checkID, k.ID)); err == nil {
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	label := instanceLabelFor(kindKey, 1)
	code := setNum + "-" + label
	now := time.Now().UTC().Format(timeLayout)
	res, err := d.Exec(`INSERT INTO check_bags (check_id, bag_kind_id, instance_label, code, created_by, created_at) VALUES (?,?,?,?,?,?)`,
		checkID, k.ID, label, code, by, now)
	if err != nil {
		return nil, fmt.Errorf("registering the %s bag: %w", kindKey, err)
	}
	id, _ := res.LastInsertId()
	return &CheckBag{ID: id, CheckID: checkID, BagKindID: k.ID, BagKindKey: kindKey, InstanceLabel: label, Code: code, CreatedBy: by, CreatedAt: now}, nil
}

// ErrBagCapReached is AddBagInstance's sentinel for "this set's max_bags is
// already in use" — the caller turns it into "ask an admin to raise the
// limit" (see docs/guides/set-checks.md), not a generic error.
var ErrBagCapReached = errors.New("this set's bag limit has been reached")

// AddBagInstance creates a SECOND (or further) instance of kindKey on check
// — a big set needing two small-parts bags, say — enforcing setNum's
// max_bags cap (default 2) across the check's instances in total. Returns
// ErrBagCapReached, not a new instance, once that cap is hit.
func (d *DB) AddBagInstance(checkID int64, setNum, kindKey, by string) (*CheckBag, error) {
	k, err := d.BagKindByKey(kindKey)
	if err != nil {
		return nil, err
	}
	if k == nil {
		return nil, fmt.Errorf("%q isn't a registered bag kind", kindKey)
	}
	existing, err := d.CheckBags(checkID)
	if err != nil {
		return nil, err
	}
	maxBags := d.GetSetState(setNum).MaxBags
	if len(existing) >= maxBags {
		return nil, ErrBagCapReached
	}
	n := 1
	for _, b := range existing {
		if b.BagKindID == k.ID {
			n++
		}
	}
	label := instanceLabelFor(kindKey, n)
	code := setNum + "-" + label
	now := time.Now().UTC().Format(timeLayout)
	res, err := d.Exec(`INSERT INTO check_bags (check_id, bag_kind_id, instance_label, code, created_by, created_at) VALUES (?,?,?,?,?,?)`,
		checkID, k.ID, label, code, by, now)
	if err != nil {
		return nil, fmt.Errorf("registering another %s bag: %w", kindKey, err)
	}
	id, _ := res.LastInsertId()
	return &CheckBag{ID: id, CheckID: checkID, BagKindID: k.ID, BagKindKey: kindKey, InstanceLabel: label, Code: code, CreatedBy: by, CreatedAt: now}, nil
}

// CheckBags lists every bag instance registered on a check.
func (d *DB) CheckBags(checkID int64) ([]CheckBag, error) {
	rows, err := d.Query(`SELECT `+checkBagCols+` FROM check_bags cb JOIN bag_kinds bk ON bk.id = cb.bag_kind_id
		WHERE cb.check_id = ? ORDER BY cb.id`, checkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CheckBag
	for rows.Next() {
		b, err := scanCheckBag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// ResolveBagCode finds any bag instance (on any check, any set) with this
// exact code — the newest one, if a messy pre-registry record happens to
// collide. nil, nil when nothing matches.
func (d *DB) ResolveBagCode(code string) (*CheckBag, error) {
	b, err := scanCheckBag(d.QueryRow(`SELECT `+checkBagCols+` FROM check_bags cb JOIN bag_kinds bk ON bk.id = cb.bag_kind_id
		WHERE cb.code = ? ORDER BY cb.id DESC LIMIT 1`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, err
}

// ScanResult is ScanBag's outcome — exactly one of these is true/non-empty.
type ScanResult struct {
	Bag     *CheckBag // set on a successful scan
	Unknown bool      // the code doesn't match any registered bag
	// OtherSet is set when the code is for a different set entirely (parsed
	// from a fresh code's own "<set>-<label>" shape, or an existing row for
	// a different check) — the specific, helpful case over a generic error.
	OtherSet string
}

// ScanBag resolves a scanned/typed code against checkID (whose set is
// setNum): finds or creates the matching check_bags row and marks it
// scanned, or explains why it couldn't (unknown code, or a code that belongs
// to a different set). Called both to "scan in" a bag before marking parts
// and for the scan-to-open-a-check entry point (checkID=0 there — see
// OpenBySetBag).
func (d *DB) ScanBag(checkID int64, setNum, code string) (ScanResult, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return ScanResult{Unknown: true}, nil
	}
	// A fresh code is self-describing: "<set>-<label>". Check that before
	// touching the database, so a code for a different set is recognised
	// even if it was printed but never actually scanned into any check yet.
	if i := strings.LastIndex(code, "-"); i > 0 {
		apparentSet, label := code[:i], code[i+1:]
		if apparentSet != setNum && looksLikeInstanceLabel(label) {
			return ScanResult{OtherSet: apparentSet}, nil
		}
	}
	existing, err := d.ResolveBagCode(code)
	if err != nil {
		return ScanResult{}, err
	}
	if existing != nil && existing.CheckID != checkID {
		var otherSet string
		_ = d.QueryRow(`SELECT set_num FROM set_checks WHERE id = ?`, existing.CheckID).Scan(&otherSet)
		if otherSet == "" {
			otherSet = "another check"
		}
		return ScanResult{OtherSet: otherSet}, nil
	}
	if existing != nil {
		now := time.Now().UTC().Format(timeLayout)
		if _, err := d.Exec(`UPDATE check_bags SET scanned_at = ? WHERE id = ?`, now, existing.ID); err != nil {
			return ScanResult{}, err
		}
		existing.ScannedAt, _ = time.Parse(timeLayout, now)
		return ScanResult{Bag: existing}, nil
	}
	// Not registered yet anywhere, but it does look like a fresh code for
	// THIS set — register and scan it in in one step (covers printing a
	// label before the check session that uses it has even started).
	i := strings.LastIndex(code, "-")
	if i <= 0 {
		return ScanResult{Unknown: true}, nil
	}
	label := code[i+1:]
	if !looksLikeInstanceLabel(label) {
		return ScanResult{Unknown: true}, nil
	}
	kindKey := kindForLabel(label)
	if kindKey == "" {
		return ScanResult{Unknown: true}, nil
	}
	by := ""
	bag, err := d.EnsureCheckBag(checkID, setNum, kindKey, by)
	if err != nil {
		return ScanResult{}, err
	}
	now := time.Now().UTC().Format(timeLayout)
	if _, err := d.Exec(`UPDATE check_bags SET scanned_at = ? WHERE id = ?`, now, bag.ID); err != nil {
		return ScanResult{}, err
	}
	bag.ScannedAt, _ = time.Parse(timeLayout, now)
	return ScanResult{Bag: bag}, nil
}

func looksLikeInstanceLabel(label string) bool {
	if label == "" || len(label) > 4 {
		return false
	}
	for i, r := range label {
		if i == 0 && !(r >= 'A' && r <= 'Z') {
			return false
		}
		if i > 0 && !(r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// kindForLabel reverses defaultBagLabel's base letter (A/B/E, ignoring a
// trailing instance number) back to a kind key — "" if it's not one of the
// built-in three (a custom kind's letter can collide with another kind's
// first letter, so an unrecognised label is treated as unknown rather than
// guessed at).
func kindForLabel(label string) string {
	base := label[:1]
	for k, b := range defaultBagLabel {
		if b == base {
			return k
		}
	}
	return ""
}

// BagScannedForKind reports whether checkID already has a scanned instance of
// kindID — mobile's stateless equivalent of the TUI's in-memory activeBag:
// "already scanned earlier this request cycle" lives in check_bags.scanned_at,
// not in any server-held session.
func (d *DB) BagScannedForKind(checkID, kindID int64) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM check_bags WHERE check_id = ? AND bag_kind_id = ? AND scanned_at != ''`, checkID, kindID).Scan(&n)
	return n > 0, err
}

// UnscannedRequiredBags is which of c's required bag kinds (see
// RequiredBagKinds) have no scanned instance yet on this check — what the
// finish-gate blocks on, and what it names in its error.
func (d *DB) UnscannedRequiredBags(c *SetCheck) ([]BagKind, error) {
	required, err := d.RequiredBagKinds(c)
	if err != nil || len(required) == 0 {
		return nil, err
	}
	existing, err := d.CheckBags(c.ID)
	if err != nil {
		return nil, err
	}
	scanned := map[int64]bool{}
	for _, b := range existing {
		if !b.ScannedAt.IsZero() {
			scanned[b.BagKindID] = true
		}
	}
	var out []BagKind
	for _, k := range required {
		if !scanned[k.ID] {
			out = append(out, k)
		}
	}
	return out, nil
}

// ---- the reusable bag pool ----
//
// A fresh bag_pool.
// A pool bag is a real, permanent container (a labeled plastic bin, say)
// with its own stable code, assigned to whichever check currently needs it
// instead of generating a fresh one-off "<set>-A" code. Assigning one
// creates its check_bags row with that exact code, so it scans and gates
// exactly like any other bag -- ScanBag/RequiredBagKinds/UnscannedRequiredBags
// need no changes at all to support it. Finishing the check frees it back to
// available (see releasePoolBagsForCheck, called from FinishCheck).

// PoolBag is one row of the reusable bag pool.
type PoolBag struct {
	Code                 string
	BagKindID            int64
	BagKindKey           string
	Status               string // "available" or "assigned"
	CurrentCheckID       int64
	CreatedBy, CreatedAt string
}

func scanPoolBag(row interface{ Scan(...any) error }) (*PoolBag, error) {
	var p PoolBag
	if err := row.Scan(&p.Code, &p.BagKindID, &p.BagKindKey, &p.Status, &p.CurrentCheckID, &p.CreatedBy, &p.CreatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

const poolBagCols = `bp.code, bp.bag_kind_id, bk.key, bp.status, bp.current_check_id, bp.created_by, bp.created_at`

// AddPoolBag registers a new reusable physical bag under its own stable
// code (e.g. "BIN-07") -- admin-only, same permission check as AddBagKind.
func (d *DB) AddPoolBag(code, kindKey, by string) (*PoolBag, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("a bag code is required")
	}
	k, err := d.BagKindByKey(kindKey)
	if err != nil {
		return nil, err
	}
	if k == nil {
		return nil, fmt.Errorf("%q isn't a registered bag kind", kindKey)
	}
	if _, err := d.Exec(`INSERT INTO bag_pool (code, bag_kind_id, status, created_by, created_at) VALUES (?,?,?,?,?)`,
		code, k.ID, "available", by, time.Now().UTC().Format(timeLayout)); err != nil {
		return nil, fmt.Errorf("registering pool bag %q: %w", code, err)
	}
	return d.PoolBagByCode(code)
}

// PoolBagByCode looks up one pool bag, nil if that code isn't registered.
func (d *DB) PoolBagByCode(code string) (*PoolBag, error) {
	p, err := scanPoolBag(d.QueryRow(`SELECT `+poolBagCols+` FROM bag_pool bp JOIN bag_kinds bk ON bk.id = bp.bag_kind_id WHERE bp.code = ?`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// PoolBags lists every registered pool bag.
func (d *DB) PoolBags() ([]PoolBag, error) {
	rows, err := d.Query(`SELECT ` + poolBagCols + ` FROM bag_pool bp JOIN bag_kinds bk ON bk.id = bp.bag_kind_id ORDER BY bp.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PoolBag
	for rows.Next() {
		p, err := scanPoolBag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// ErrPoolBagUnavailable is AssignPoolBag's sentinel for "already assigned to
// a different check".
var ErrPoolBagUnavailable = errors.New("that pool bag is already assigned to a different check")

// AssignPoolBag hands checkID a reusable pool bag by its code instead of a
// fresh auto-generated one -- admin-only (raising a set's bag count is
// already admin-gated; handing out a specific physical bag is the same kind
// of decision). Idempotent for the same check; ErrPoolBagUnavailable if
// another check currently has it.
func (d *DB) AssignPoolBag(checkID int64, by, code string) (*CheckBag, error) {
	pb, err := d.PoolBagByCode(code)
	if err != nil {
		return nil, err
	}
	if pb == nil {
		return nil, fmt.Errorf("no pool bag registered with code %q", code)
	}
	if pb.Status == "assigned" && pb.CurrentCheckID != checkID {
		return nil, ErrPoolBagUnavailable
	}
	if existing, err := scanCheckBag(d.QueryRow(`SELECT `+checkBagCols+` FROM check_bags cb JOIN bag_kinds bk ON bk.id = cb.bag_kind_id
		WHERE cb.check_id = ? AND cb.code = ?`, checkID, code)); err == nil {
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if _, err := d.Exec(`UPDATE bag_pool SET status = 'assigned', current_check_id = ? WHERE code = ?`, checkID, code); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(timeLayout)
	res, err := d.Exec(`INSERT INTO check_bags (check_id, bag_kind_id, instance_label, code, created_by, created_at) VALUES (?,?,?,?,?,?)`,
		checkID, pb.BagKindID, "POOL:"+code, code, by, now)
	if err != nil {
		return nil, fmt.Errorf("assigning pool bag %q: %w", code, err)
	}
	id, _ := res.LastInsertId()
	return &CheckBag{ID: id, CheckID: checkID, BagKindID: pb.BagKindID, BagKindKey: pb.BagKindKey, InstanceLabel: "POOL:" + code, Code: code, CreatedBy: by, CreatedAt: now}, nil
}

// ReleasePoolBag frees a pool bag back to available by hand -- e.g. a check
// was abandoned rather than finished (see releasePoolBagsForCheck for the
// normal finish-time path). A no-op if it's already available or unknown.
func (d *DB) ReleasePoolBag(code string) error {
	_, err := d.Exec(`UPDATE bag_pool SET status = 'available', current_check_id = 0 WHERE code = ?`, code)
	return err
}

// releasePoolBagsForCheck frees every pool bag assigned to checkID, in the
// same transaction as finishing it (see FinishCheck) -- a reusable bag is
// only ever "out" for the lifetime of the check that borrowed it.
func releasePoolBagsForCheck(tx *sql.Tx, checkID int64) error {
	_, err := tx.Exec(`UPDATE bag_pool SET status = 'available', current_check_id = 0 WHERE current_check_id = ?`, checkID)
	return err
}

// ---- per-bag breakdown (completion dashboard, reports) ----

// BagMissing is one bag kind's share of a check's still-missing pieces.
type BagMissing struct {
	BagKind
	Missing int
}

// MissingByBag breaks c's missing total down by the bag kind each short
// line's part actually belongs in (its ordinary small/main/custom
// placement -- never extra, which isn't about what's missing) -- what the
// completion dashboard and missing-parts report show per bag, instead of
// just one set-wide total.
func (d *DB) MissingByBag(c *SetCheck) ([]BagMissing, error) {
	totals := map[int64]int{}
	kinds := map[int64]BagKind{}
	for _, l := range c.Lines {
		m := l.Missing()
		if m <= 0 {
			continue
		}
		k, err := d.RequiredBagKind(l, "have")
		if err != nil {
			return nil, err
		}
		if k == nil {
			continue
		}
		totals[k.ID] += m
		kinds[k.ID] = *k
	}
	out := make([]BagMissing, 0, len(totals))
	for id, n := range totals {
		out = append(out, BagMissing{BagKind: kinds[id], Missing: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// ---- lost-bag lookup ----

// BagLookup is FindBag's result -- whatever is known about a code,
// independent of any active checking session (a bag found on a shelf
// without its checker, or double-checking a label before it goes on).
type BagLookup struct {
	Code        string
	Found       bool // false = nothing at all is known about this code
	SetNum      string
	CheckID     int64
	CheckDone   bool
	BagKindKey  string
	BagKindName string
	ScannedAt   time.Time // zero = registered/printed but never actually scanned in
	Pool        bool      // true = code belongs to a reusable pool bag, not a fresh per-check one
	PoolStatus  string    // "available" or "assigned" -- only meaningful when Pool
	// CreatedBy/CreatedAt are who registered/printed this bag instance and
	// when -- blank/zero for a pool bag (pool bags are admin-provisioned
	// once, not per-use) or a code that's never been registered at all (the
	// "printed but never scanned in" fallback path below).
	CreatedBy string
	CreatedAt time.Time
	// Missing/Extra/Disputed/DisputeNote are the outcome of the check this
	// bag is (or was) tied to -- "what mess-ups did it make" -- only
	// meaningful when CheckID != 0. Zero/false/blank for a bag whose check
	// is still in progress or has nothing to report.
	Missing, Extra int
	Disputed       bool
	DisputeNote    string
}

// FindBag resolves any bag code to whatever is known about it: a pool bag's
// own identity, a registered per-check instance (scanned or not, on a
// finished check or a live one), or -- for a code that was printed but never
// actually scanned into anything yet -- the set its own "<set>-<label>"
// shape says it was made for. Unlike ScanBag, this never registers or
// scans anything in; it's read-only, for "what is this bag" rather than
// "mark this bag scanned".
func (d *DB) FindBag(code string) (BagLookup, error) {
	code = strings.TrimSpace(code)
	out := BagLookup{Code: code}
	if code == "" {
		return out, nil
	}
	if pb, err := d.PoolBagByCode(code); err != nil {
		return out, err
	} else if pb != nil {
		out.Found, out.Pool, out.PoolStatus, out.BagKindKey = true, true, pb.Status, pb.BagKindKey
		out.CreatedBy = pb.CreatedBy
		out.CreatedAt, _ = time.Parse(timeLayout, pb.CreatedAt)
		if k, _ := d.BagKindByKey(pb.BagKindKey); k != nil {
			out.BagKindName = k.Name
		}
		if pb.CurrentCheckID > 0 {
			out.CheckID = pb.CurrentCheckID
			out.fillCheckOutcome(d, pb.CurrentCheckID)
		}
		return out, nil
	}
	if b, err := d.ResolveBagCode(code); err != nil {
		return out, err
	} else if b != nil {
		out.Found, out.CheckID, out.BagKindKey, out.ScannedAt = true, b.CheckID, b.BagKindKey, b.ScannedAt
		out.CreatedBy = b.CreatedBy
		out.CreatedAt, _ = time.Parse(timeLayout, b.CreatedAt)
		if k, _ := d.BagKindByKey(b.BagKindKey); k != nil {
			out.BagKindName = k.Name
		}
		out.fillCheckOutcome(d, b.CheckID)
		return out, nil
	}
	// Not registered anywhere yet, but its own shape (same parse as ScanBag)
	// says which set and kind it was printed for.
	if i := strings.LastIndex(code, "-"); i > 0 {
		apparentSet, label := code[:i], code[i+1:]
		if looksLikeInstanceLabel(label) {
			if kindKey := kindForLabel(label); kindKey != "" {
				out.Found, out.SetNum, out.BagKindKey = true, apparentSet, kindKey
				if k, _ := d.BagKindByKey(kindKey); k != nil {
					out.BagKindName = k.Name
				}
			}
		}
	}
	return out, nil
}

// fillCheckOutcome sets SetNum/CheckDone/Missing/Extra/Disputed/DisputeNote
// from the check a bag is (or was) tied to -- "what mess-ups did it make",
// for an admin looking a bag up by its code. Best-effort: a check that no
// longer exists (shouldn't happen, but GetCheck can fail) just leaves these
// fields at their zero value rather than failing the whole lookup.
func (out *BagLookup) fillCheckOutcome(d *DB, checkID int64) {
	c, err := d.GetCheck(checkID)
	if err != nil || c == nil {
		return
	}
	out.SetNum, out.CheckDone = c.SetNum, c.Status == StatusDone
	_, _, out.Missing, out.Extra, _ = c.Totals()
	out.Disputed, out.DisputeNote = c.Disputed, c.DisputeNote
}

// PrintableBags is every bag label worth offering to print for setNum right
// now, true code and kind for each one -- what "reprint it" actually needs.
// Earlier printing code recomputed a code from a hardcoded small/main/extra
// kind list via BagCode, which is only ever right for a bag's first,
// auto-generated instance: a pool-assigned bag's real code is whatever an
// admin chose (see AssignPoolBag), a second instance's is "<set>-A2" not
// "<set>-A" (see AddBagInstance), and a custom kind beyond the built-in
// three was never in that hardcoded list at all -- reprinting any of those
// silently printed the wrong label, or none. This reads every bag actually
// registered on the set's current (or, once finished, most recent) check,
// which carries its own true code and kind regardless of how it came to
// exist, falling back to the small/main kinds' deterministic not-yet-
// printed codes only when nothing has been registered yet at all (a set
// never checked, or a check that hasn't needed either bag yet).
func (d *DB) PrintableBags(setNum string) ([]CheckBag, error) {
	// By kind, not by code: a real instance's code (a pool bag's own chosen
	// code, say) will almost never match the deterministic placeholder's, so
	// de-duplicating on the code string would print both -- the virtual
	// small/main placeholder only belongs here when that KIND has no real
	// instance registered yet at all, regardless of what code it ended up with.
	seenKind := map[int64]bool{}
	var out []CheckBag
	c, err := d.CurrentOrLastCheck(setNum)
	if err != nil {
		return nil, err
	}
	if c != nil {
		bags, err := d.CheckBags(c.ID)
		if err != nil {
			return nil, err
		}
		for _, b := range bags {
			out = append(out, b)
			seenKind[b.BagKindID] = true
		}
	}
	for _, kind := range []string{BagSmall, BagMain} {
		k, err := d.BagKindByKey(kind)
		if err != nil {
			return nil, err
		}
		if k == nil || seenKind[k.ID] {
			continue
		}
		out = append(out, CheckBag{BagKindID: k.ID, BagKindKey: kind, InstanceLabel: instanceLabelFor(kind, 1), Code: BagCode(setNum, kind)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}
