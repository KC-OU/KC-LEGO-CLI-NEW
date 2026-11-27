package lego

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The onboarding of one set by one checker: a paper check (the printed or filled-in sorting sheet) that an
// admin verifies, then the check itself, then a submit that finishes the onboarding and opens the blog.
//
//	awaiting_paper -> awaiting_verify -> verified -> complete
//	                        ^   changes_needed <-'
//
// Until the paper check is verified a checker cannot start the check anywhere (dashboard, app, CLI, TUI, phone):
// see OnboardingGate. An admin can waive the gate for one set, or for every set with the "*" waiver.
const (
	FlowAwaitingPaper  = "awaiting_paper"
	FlowAwaitingVerify = "awaiting_verify"
	FlowChanges        = "changes_needed"
	FlowVerified       = "verified"
	FlowComplete       = "complete"
)

// FlowTags are the quick reasons an admin can attach when sending a paper check back.
var FlowTags = []string{"IDs missing", "counts wrong", "sheet unreadable", "wrong set"}

type OnboardingFlow struct {
	ID                                          int64
	SetNum, User, Status                        string
	PaperFile, PaperKind, SheetJSON             string
	Verifier, VerifyNote, VerifyTags            string
	VerifiedAt, Discrepancy, Answers, Completed string
	CreatedAt, UpdatedAt                        string
}

const flowCols = `id, set_num, user_name, status, paper_file, paper_kind, sheet_json, verifier, verify_note, verify_tags, verified_at,
	discrepancy, answers, completed_at, created_at, updated_at`

func scanFlow(row interface{ Scan(...any) error }) (*OnboardingFlow, error) {
	var f OnboardingFlow
	err := row.Scan(&f.ID, &f.SetNum, &f.User, &f.Status, &f.PaperFile, &f.PaperKind, &f.SheetJSON, &f.Verifier, &f.VerifyNote, &f.VerifyTags,
		&f.VerifiedAt, &f.Discrepancy, &f.Answers, &f.Completed, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func flowNow() string { return time.Now().UTC().Format(timeLayout) }

func cleanFlowSet(s string) string { return strings.TrimSpace(s) }

// StartFlow returns the user's open onboarding of the set, creating one (awaiting the paper check) when there
// is none, or when the last one is already complete (a later check of the same set starts again).
func (d *DB) StartFlow(setNum, user string) (*OnboardingFlow, error) {
	setNum, user = cleanFlowSet(setNum), strings.TrimSpace(user)
	if setNum == "" || user == "" {
		return nil, errors.New("a set and a user are required")
	}
	if f, err := d.FlowFor(setNum, user); err == nil && f != nil && f.Status != FlowComplete {
		return f, nil
	}
	now := flowNow()
	res, err := d.Exec(`INSERT INTO onboarding_flows (set_num, user_name, status, created_at, updated_at) VALUES (?,?,?,?,?)`, setNum, user, FlowAwaitingPaper, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return d.GetFlow(id)
}

func (d *DB) GetFlow(id int64) (*OnboardingFlow, error) {
	f, err := scanFlow(d.QueryRow(`SELECT `+flowCols+` FROM onboarding_flows WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("no onboarding #%d", id)
	}
	return f, err
}

// FlowFor is the user's newest onboarding of the set (nil, nil when there is none).
func (d *DB) FlowFor(setNum, user string) (*OnboardingFlow, error) {
	f, err := scanFlow(d.QueryRow(`SELECT `+flowCols+` FROM onboarding_flows WHERE set_num = ? AND user_name = ? ORDER BY id DESC LIMIT 1`, setNum, user))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return f, err
}

// ListFlows lists onboardings, oldest first for a queue ("awaiting_verify"); status "" lists everything newest first.
func (d *DB) ListFlows(status string) ([]OnboardingFlow, error) {
	q, order := `SELECT `+flowCols+` FROM onboarding_flows`, ` ORDER BY id DESC LIMIT 300`
	var args []any
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
		order = ` ORDER BY id LIMIT 300`
	}
	rows, err := d.Query(q+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OnboardingFlow
	for rows.Next() {
		f, err := scanFlow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// SubmitPaper records the paper check the user uploaded (file is the stored name; sheetJSON the structured
// marks of a saved digital sheet, "" for a photo or scan) and queues it for an admin.
func (d *DB) SubmitPaper(id int64, user, kind, file, sheetJSON string) error {
	f, err := d.GetFlow(id)
	if err != nil {
		return err
	}
	if !strings.EqualFold(f.User, user) {
		return errors.New("this onboarding belongs to someone else")
	}
	if f.Status != FlowAwaitingPaper && f.Status != FlowChanges {
		return fmt.Errorf("the paper check can't be sent now (status %s)", f.Status)
	}
	_, err = d.Exec(`UPDATE onboarding_flows SET status = ?, paper_file = ?, paper_kind = ?, sheet_json = ?, updated_at = ? WHERE id = ?`,
		FlowAwaitingVerify, file, kind, sheetJSON, flowNow(), id)
	return err
}

// VerifyFlow is the admin's answer: ok lets the user carry on, otherwise the note (required) says what to change.
func (d *DB) VerifyFlow(id int64, verifier string, ok bool, note, tags string) (*OnboardingFlow, error) {
	f, err := d.GetFlow(id)
	if err != nil {
		return nil, err
	}
	if f.Status != FlowAwaitingVerify {
		return nil, fmt.Errorf("this paper check is not waiting to be verified (status %s)", f.Status)
	}
	note = strings.TrimSpace(note)
	if !ok && note == "" {
		return nil, errors.New("say what needs to change: a note is required when the paper check is sent back")
	}
	status, at := FlowChanges, ""
	if ok {
		status, at = FlowVerified, flowNow()
	}
	if _, err := d.Exec(`UPDATE onboarding_flows SET status = ?, verifier = ?, verify_note = ?, verify_tags = ?, verified_at = ?, updated_at = ? WHERE id = ?`,
		status, verifier, note, tags, at, flowNow(), id); err != nil {
		return nil, err
	}
	return d.GetFlow(id)
}

// CompleteFlow finishes the onboarding once the check is done: the flow must be verified and the set's last
// finished check must be newer than the verification (the check can't have happened before it was allowed).
func (d *DB) CompleteFlow(id int64, user, answers string) (*OnboardingFlow, error) {
	f, err := d.GetFlow(id)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(f.User, user) {
		return nil, errors.New("this onboarding belongs to someone else")
	}
	if f.Status != FlowVerified {
		return nil, fmt.Errorf("the paper check has to be verified first (status %s)", f.Status)
	}
	last, _ := d.LastCheck(f.SetNum)
	if last == nil || last.Status != StatusDone {
		return nil, errors.New("finish the check first, then submit")
	}
	if v, err := time.Parse(timeLayout, f.VerifiedAt); err == nil && last.FinishedAt.Before(v) {
		return nil, errors.New("the set's last check is older than the verification: do the check, then submit")
	}
	if why := d.BlogBlock(f.SetNum); why != "" {
		return nil, errors.New("the blog can't be generated: " + why)
	}
	if _, err := d.Exec(`UPDATE onboarding_flows SET status = ?, answers = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
		FlowComplete, answers, flowNow(), flowNow(), id); err != nil {
		return nil, err
	}
	return d.GetFlow(id)
}

// FlowCheckDone reports whether the set has a finished check from after the flow was verified: what Submit needs.
func (d *DB) FlowCheckDone(f *OnboardingFlow) bool {
	last, _ := d.LastCheck(f.SetNum)
	if last == nil || last.Status != StatusDone {
		return false
	}
	v, err := time.Parse(timeLayout, f.VerifiedAt)
	return err != nil || !last.FinishedAt.Before(v)
}

// SetFlowDiscrepancy records what the admin found different between the paper and the dashboard check.
func (d *DB) SetFlowDiscrepancy(id int64, note string) error {
	_, err := d.Exec(`UPDATE onboarding_flows SET discrepancy = ?, updated_at = ? WHERE id = ?`, strings.TrimSpace(note), flowNow(), id)
	return err
}

// WaiveOnboarding lets anyone check setNum without the paper-check onboarding ("*" waives it for every set).
func (d *DB) WaiveOnboarding(setNum, by string, on bool) error {
	setNum = strings.TrimSpace(setNum)
	if setNum == "" {
		return errors.New("a set (or * for all sets) is required")
	}
	if !on {
		_, err := d.Exec(`DELETE FROM onboarding_waivers WHERE set_num = ?`, setNum)
		return err
	}
	_, err := d.Exec(`INSERT INTO onboarding_waivers (set_num, waived_by, waived_at) VALUES (?,?,?)
		ON CONFLICT(set_num) DO UPDATE SET waived_by = excluded.waived_by, waived_at = excluded.waived_at`, setNum, by, flowNow())
	return err
}

// Waivers lists the sets the gate is waived for.
func (d *DB) Waivers() []string {
	rows, err := d.Query(`SELECT set_num FROM onboarding_waivers ORDER BY set_num`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			out = append(out, s)
		}
	}
	return out
}

// OnboardingCleared reports whether user may start an intake check of setNum: the set (or every set) is waived,
// they have a verified or completed onboarding for it, or a check of it is already under way (resuming a draft
// is never blocked).
func (d *DB) OnboardingCleared(setNum, user string) bool {
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM onboarding_waivers WHERE set_num IN (?, '*')`, setNum).Scan(&n)
	if n > 0 {
		return true
	}
	if f, _ := d.FlowFor(setNum, user); f != nil && (f.Status == FlowVerified || f.Status == FlowComplete) {
		return true
	}
	if c, _ := d.draftCheck(setNum, CheckIntake); c != nil {
		return true
	}
	return false
}

// OnboardingGate is OnboardingCleared as an error whose text says what to do next, for the places that start a
// check. Only an intake check is gated: a recount of an already-checked set is not.
func (d *DB) OnboardingGate(setNum, kind, user string) error {
	if kind != CheckIntake || d.OnboardingCleared(setNum, user) {
		return nil
	}
	return &OnboardingGateError{Set: setNum}
}

// OnboardingGateError is the refusal to start a check before the paper check is verified; its text says what to do.
type OnboardingGateError struct{ Set string }

func (e *OnboardingGateError) Error() string {
	return fmt.Sprintf("onboarding first: do the paper check for set %s and have it verified (dashboard → Onboarding, or wms-go onboarding status %s)", e.Set, e.Set)
}

// sheetState is the structured marks a saved digital sorting sheet carries (see sorting.go's script).
type sheetState struct {
	Set   string `json:"set"`
	Lines []struct {
		P string `json:"p"`
		C int    `json:"c"`
		S int    `json:"s"`
		M string `json:"m"`
		X string `json:"x"`
	} `json:"lines"`
	Borrowed [][]string `json:"borrowed"`
	Rec      int        `json:"rec"` // borrowed quantity already recorded when the sheet was printed
}

// SheetDiff compares the marks on the digital paper sheet with the set's last finished check and returns what
// differs, one line each. nil when the sheet is not a saved digital one or everything agrees.
func (d *DB) SheetDiff(f *OnboardingFlow) []string {
	if f == nil || f.SheetJSON == "" {
		return nil
	}
	var sh sheetState
	if json.Unmarshal([]byte(f.SheetJSON), &sh) != nil {
		return []string{"the saved sheet could not be read"}
	}
	last, _ := d.LastCheck(f.SetNum)
	if last == nil {
		return []string{"no finished check to compare with yet"}
	}
	type key struct {
		part  string
		color int
	}
	paperMiss, paperExtra, unresolved := map[key]int{}, map[key]int{}, map[key]bool{}
	for _, l := range sh.Lines {
		k := key{strings.ToLower(l.P), l.C}
		m, _ := strconv.Atoi(l.M)
		x, _ := strconv.Atoi(l.X)
		paperMiss[k], paperExtra[k] = m, x
		if l.S == 3 {
			unresolved[k] = true
		}
	}
	var out []string
	for _, l := range last.Lines {
		k := key{strings.ToLower(l.PartNum), l.ColorID}
		short := max(0, l.Need-l.Have) // what was short, before anything borrowed is taken off
		if l.Optional {
			short = 0
		}
		if paperMiss[k] != short {
			out = append(out, fmt.Sprintf("%s %s: paper says %d missing, the check says %d", l.PartNum, l.ColorName, paperMiss[k], short))
		}
		if paperExtra[k] != l.Extra {
			out = append(out, fmt.Sprintf("%s %s: paper says %d extra, the check says %d", l.PartNum, l.ColorName, paperExtra[k], l.Extra))
		}
		if unresolved[k] {
			out = append(out, fmt.Sprintf("%s %s: still marked ? on the paper", l.PartNum, l.ColorName))
		}
	}
	paperBorrowed := sh.Rec
	for _, r := range sh.Borrowed {
		if len(r) > 2 {
			n, _ := strconv.Atoi(r[2])
			paperBorrowed += n
		}
	}
	sys := 0
	if rows, err := d.BorrowedForSet(f.SetNum); err == nil {
		for _, b := range rows {
			sys += b.Qty
		}
	}
	if paperBorrowed != sys {
		out = append(out, fmt.Sprintf("borrowed parts: the paper lists %d, the system has %d", paperBorrowed, sys))
	}
	sort.Strings(out)
	return out
}

// ParseSheetState pulls the saved state out of a digital sorting sheet ("" when the file is not one).
func ParseSheetState(html []byte) string {
	s := string(html)
	i := strings.Index(s, `<script type="application/json" id="kc-state">`)
	if i < 0 {
		return ""
	}
	rest := s[i+len(`<script type="application/json" id="kc-state">`):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		return ""
	}
	raw := strings.TrimSpace(rest[:j])
	var probe sheetState
	if json.Unmarshal([]byte(raw), &probe) != nil {
		return ""
	}
	return raw
}

// BlogTemplate is the starting Markdown for the set's blog post (front matter plus an overview), downloaded
// when the onboarding is complete so the writer can carry on in Obsidian.
func (d *DB) BlogTemplate(setNum string) string {
	title, theme, year, pieces := setNum, "", 0, 0
	if s, _ := d.CatalogSet(setNum); s != nil {
		title, theme, year, pieces = s.Name, s.Theme, s.Year, s.Pieces
	} else if s, _ := d.GetSetByNum(strings.TrimSuffix(setNum, "-1")); s != nil {
		title, theme, year, pieces = s.Name, s.Theme, s.Year, s.PartsQty
	}
	slug := strings.ToLower(setNum)
	return fmt.Sprintf("---\nslug: %s\ntitle: %q\nset: %s\ntheme: %s\nsummary: \"\"\nstatus: checked\n---\n# About this set\n\n"+
		"Name: %s\nSet: %s\nReleased: %d\nParts: %d\nTheme: %s\n\nWrite a few lines in your own words about the set.\n", slug, title, setNum, theme, title, setNum, year, pieces, theme)
}
