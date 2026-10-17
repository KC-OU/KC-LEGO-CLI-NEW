package mobileapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// lineView is one line — a check line or an order line — as the mobile app
// sees it, same shape either way so one screen (PickWalkScreen on the
// mobile side) handles both: Kind tells the client which actions are valid
// (have_all/missing/extra/have/damaged for a check, receive for an order).
// /next and /confirm both return this, so a client can show /confirm's
// result without a second round-trip.
type lineView struct {
	Kind      string `json:"kind"` // "check" | "order"
	Position  int    `json:"position"`
	Total     int    `json:"total"`
	Target    string `json:"target"`
	SetName   string `json:"set_name"`
	PartNum   string `json:"part_num"`
	ColorName string `json:"color_name"`
	ColorRGB  string `json:"color_rgb,omitempty"` // e.g. "C91A09", no leading '#' — "" when unknown
	Name      string `json:"name"`
	Need      int    `json:"need"`
	Have      int    `json:"have"`
	Missing   int    `json:"missing"`
	Extra     int    `json:"extra"`
	// TotalMissing/TotalExtra are the whole check's running totals (see
	// lego.SetCheck.Totals), not just this one line's -- the app's own
	// equivalent of the TUI's always-visible "MISSING N / EXTRA N" badge, so
	// a checker never has to add it up themself or guess. Always 0 for an
	// order line (orders have no "extra" concept).
	TotalMissing int `json:"total_missing"`
	TotalExtra   int `json:"total_extra"`
	// Damaged is a condition flag, not a quantity -- see lego.CheckLine.
	// Always 0 for an order line (orders have no condition-flag action).
	Damaged      int    `json:"damaged"`
	Location     string `json:"location"`
	NextLocation string `json:"next_location"`
	Done         bool   `json:"done"`                    // true only when there's nothing assigned at all
	CompletedSet string `json:"completed_set,omitempty"` // set here only when a receive just finished it
	// SetImageURL/PartImageURL(s) are plain Rebrickable CDN URLs, not fetched
	// or cached here — the phone loads them directly; unlike the TUI's
	// guided walk, there's no terminal to render into, so a client that
	// doesn't want pictures just doesn't show the <Image>. "" / empty when
	// there's nothing to show (an unknown set, a colour-less part lookup).
	// PartImageURLs is ordered best-first (requested colour, then other
	// known colours/sources) — a single URL 404ing doesn't mean the part has
	// no picture, since not every part+colour combination has one; the
	// client should try the next one rather than giving up. PartImageURL
	// mirrors PartImageURLs[0] for any consumer that only wants one.
	SetImageURL   string   `json:"set_image_url,omitempty"`
	PartImageURL  string   `json:"part_image_url,omitempty"`
	PartImageURLs []string `json:"part_image_urls,omitempty"`
	// AccuracyPct is the signed-in user's own today-so-far accuracy for
	// whichever role this walk is (checker for a check, picker for an
	// order) — internal/lego/accuracy.go's AccuracyToday, the same number
	// "My Accuracy" shows in the TUI, read fresh on every /next and
	// /confirm so it's never more than one request stale.
	AccuracyPct float64 `json:"accuracy_pct"`
	// ElapsedSeconds/EstimatedSeconds are "how long it will take, how long
	// it has taken": Elapsed is now minus this ticket's own ClaimedAt;
	// Estimated is lego.DB.EstimatePace's historical average for this kind
	// of ticket. Both 0/omitted when there's nothing to show yet (no
	// current ticket, or no finished-ticket history for Estimated).
	ElapsedSeconds   int64 `json:"elapsed_seconds,omitempty"`
	EstimatedSeconds int64 `json:"estimated_seconds,omitempty"`
	// BagWarning is non-empty when a confirm's bag_code didn't match what the
	// line needed, or none was ever scanned — set only by confirmCheckLine,
	// a warning never a block (see docs/guides/set-checks.md): the mark above
	// still applied regardless. Always "" for /next and for an order line.
	BagWarning string `json:"bag_warning,omitempty"`
}

// ticketTiming is lineViewAt/lineViewAtOrder's shared "how long" lookup —
// read fresh every time, same as everything else in this file, rather than
// threading the ticket this request already found for CurrentTicket through
// every call site.
func (s *Server) ticketTiming(kind, username string) (elapsed, estimated int64) {
	if t, _ := s.legoDB.CurrentTicket(username); t != nil && !t.ClaimedAt.IsZero() {
		elapsed = int64(time.Since(t.ClaimedAt).Seconds())
	}
	if pace, _ := s.legoDB.EstimatePace(kind, 20); pace.Samples > 0 {
		estimated = pace.AvgSeconds
	}
	return
}

// loadSortedCheck resolves the signed-in user's current claimed check ticket
// and loads its lines in the same shelf-walk order the TUI's guided view
// uses (sortCheckLinesByLocation) — called fresh on every request rather
// than held in server memory, so two requests (or the TUI, open at the same
// time) never see stale state. nil, nil when the current ticket isn't a
// check (an order ticket, or nothing claimed) — see loadSortedOrder.
func (s *Server) loadSortedCheck(ctx context.Context, username string) (*lego.SetCheck, error) {
	t, err := s.legoDB.CurrentTicket(username)
	if err != nil {
		return nil, err
	}
	if t == nil || t.Kind != lego.TicketCheck {
		return nil, nil
	}
	kind := lego.CheckIntake
	if s.legoDB.GetSetState(t.Target).Checked() {
		kind = lego.CheckRecount
	}
	c, err := s.legoDB.NewCheck(ctx, s.rebrick, t.Target, kind, username)
	if err != nil {
		return nil, err
	}
	s.legoDB.SortCheckLinesByLocation(s.pdb, c.Lines)
	return c, nil
}

// loadSortedOrder is loadSortedCheck's counterpart for an order-picking
// ticket (lego.TicketOrder) — receiving a supplier order's lines, the other
// half of "the check and the picking" from the original request. nil, nil
// when the current ticket isn't an order.
func (s *Server) loadSortedOrder(username string) (*lego.Order, error) {
	t, err := s.legoDB.CurrentTicket(username)
	if err != nil {
		return nil, err
	}
	if t == nil || t.Kind != lego.TicketOrder {
		return nil, nil
	}
	id, err := strconv.ParseInt(t.Target, 10, 64)
	if err != nil {
		return nil, err
	}
	o, err := s.legoDB.GetOrder(id)
	if err != nil {
		return nil, err
	}
	o.Lines = s.legoDB.SortedOrderLines(s.pdb, o.Lines)
	return o, nil
}

func (s *Server) lineViewAt(c *lego.SetCheck, pos int, username string) lineView {
	l := c.Lines[pos]
	acc, _ := s.legoDB.AccuracyToday(username, lego.AccuracyChecker)
	elapsed, estimated := s.ticketTiming(lego.TicketCheck, username)
	imgs := s.partImageURLs(l.PartNum, l.ColorID)
	_, _, totMissing, totExtra, _ := c.Totals()
	v := lineView{
		Kind: "check", Position: pos + 1, Total: len(c.Lines),
		Target: c.SetNum, PartNum: l.PartNum, ColorName: l.ColorName, Name: l.PartName,
		Need: l.Need, Have: l.Have, Missing: l.Missing(), Extra: l.Extra, Damaged: l.Damaged,
		TotalMissing: totMissing, TotalExtra: totExtra,
		Location:         s.legoDB.LocationFor(s.pdb, l.PartNum, l.ColorID, l.ColorName),
		ColorRGB:         s.colorRGB(l.ColorID),
		PartImageURL:     firstOr(imgs),
		PartImageURLs:    imgs,
		AccuracyPct:      acc,
		ElapsedSeconds:   elapsed,
		EstimatedSeconds: estimated,
	}
	if pos+1 < len(c.Lines) {
		n := c.Lines[pos+1]
		v.NextLocation = s.legoDB.LocationFor(s.pdb, n.PartNum, n.ColorID, n.ColorName)
	}
	if cs, _ := s.legoDB.CatalogSet(c.SetNum); cs != nil {
		v.SetName = cs.Name
		v.SetImageURL = cs.ImgURL
	}
	return v
}

// partImageURLs/colorRGB back the mobile app's own picture (see internal/
// uiapp's ShowPictures preference for the TUI's equivalent) — plain URLs
// and a hex string, not fetched or decoded here; the phone does that
// itself. lego.DB.PartImageURLs is the shared colour-fallback decision,
// also used by the TUI's own guided walk (set_check.go).
func (s *Server) partImageURLs(partNum string, colorID int) []string {
	return s.legoDB.PartImageURLs(partNum, colorID)
}

func firstOr(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func (s *Server) colorRGB(colorID int) string {
	if colorID < 0 {
		return ""
	}
	c, ok := s.legoDB.ColorByID(colorID)
	if !ok {
		return ""
	}
	return c.RGB
}

// lineViewAtOrder mirrors lineViewAt: Need/Have read as "ordered" / "received
// so far" — ReceiveLine already treats a line as done once Have reaches
// Need, so "missing" (need-have) reads the same way a check's does.
func (s *Server) lineViewAtOrder(o *lego.Order, pos int, username string) lineView {
	l := o.Lines[pos]
	missing := l.Qty - l.ReceivedQty
	if missing < 0 {
		missing = 0
	}
	acc, _ := s.legoDB.AccuracyToday(username, lego.AccuracyPicker)
	elapsed, estimated := s.ticketTiming(lego.TicketOrder, username)
	imgs := s.partImageURLs(l.PartNum, l.ColorID)
	v := lineView{
		Kind: "order", Position: pos + 1, Total: len(o.Lines),
		Target: strconv.FormatInt(o.ID, 10), SetName: o.Supplier,
		PartNum: l.PartNum, ColorName: l.ColorName, Name: l.PartName,
		Need: l.Qty, Have: l.ReceivedQty, Missing: missing,
		Location:         s.legoDB.LocationFor(s.pdb, l.PartNum, l.ColorID, l.ColorName),
		ColorRGB:         s.colorRGB(l.ColorID),
		PartImageURL:     firstOr(imgs),
		PartImageURLs:    imgs,
		AccuracyPct:      acc,
		ElapsedSeconds:   elapsed,
		EstimatedSeconds: estimated,
	}
	if pos+1 < len(o.Lines) {
		n := o.Lines[pos+1]
		v.NextLocation = s.legoDB.LocationFor(s.pdb, n.PartNum, n.ColorID, n.ColorName)
	}
	if l.SetNum != "" {
		if cs, _ := s.legoDB.CatalogSet(l.SetNum); cs != nil {
			v.SetImageURL = cs.ImgURL
		}
	}
	return v
}

// handleNext is read-only: here's line N of the signed-in user's current
// check or order, with where it lives. No scan required to use it —
// "position" just steps through like the TUI's guided walk does,
// client-driven (the server holds no walk-position state of its own).
func (s *Server) handleNext(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if err := s.legoDB.RequireClockedIn(sess.Username); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	// No ?pos at all (as opposed to an explicit pos=0) means "resume" --
	// a fresh app load on any device, with no local memory of where this
	// ticket was left off. Resolves to the ticket's own last_position
	// (AdvanceCheckPosition/AdvanceOrderPosition), not literally line 0.
	hasPos := r.URL.Query().Has("pos")
	pos, _ := strconv.Atoi(r.URL.Query().Get("pos"))
	if pos < 0 {
		pos = 0
	}

	c, err := s.loadSortedCheck(r.Context(), sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if c != nil {
		if len(c.Lines) == 0 {
			writeJSON(w, http.StatusOK, lineView{Done: true})
			return
		}
		if !hasPos {
			pos = c.LastPosition
		}
		writeJSON(w, http.StatusOK, s.lineViewAt(c, min(pos, len(c.Lines)-1), sess.Username))
		return
	}

	o, err := s.loadSortedOrder(sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if o == nil || len(o.Lines) == 0 {
		writeJSON(w, http.StatusOK, lineView{Done: true})
		return
	}
	if !hasPos {
		pos = o.LastPosition
	}
	writeJSON(w, http.StatusOK, s.lineViewAtOrder(o, min(pos, len(o.Lines)-1), sess.Username))
}

// handleFull returns every line of the signed-in user's current check or
// order in one response, in the same order handleNext walks through by
// position — full offline mode's prefetch. A client caches this once while
// still online so a dead-signal shelf area can keep advancing through
// already-fetched lines instead of getting stuck waiting on a network call
// that a one-line-at-a-time /mobile/next can't make from nowhere. Confirms
// still go through /mobile/confirm (queued client-side while offline,
// unchanged) — this only solves "what does the next line say," not writes.
func (s *Server) handleFull(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if err := s.legoDB.RequireClockedIn(sess.Username); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	c, err := s.loadSortedCheck(r.Context(), sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if c != nil {
		lines := make([]lineView, len(c.Lines))
		for i := range c.Lines {
			lines[i] = s.lineViewAt(c, i, sess.Username)
		}
		writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
		return
	}

	o, err := s.loadSortedOrder(sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if o == nil {
		writeJSON(w, http.StatusOK, map[string]any{"lines": []lineView{}})
		return
	}
	lines := make([]lineView, len(o.Lines))
	for i := range o.Lines {
		lines[i] = s.lineViewAtOrder(o, i, sess.Username)
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

type scanBagRequest struct {
	Code string `json:"code"`
}

// handleScanBag resolves a scanned/typed bag code against the signed-in
// user's currently claimed check: finds or registers the matching instance
// and marks it scanned, same resolution ScanBag gives the TUI's B key.
// Needs a check already claimed (see loadSortedCheck) — scanning a bag with
// nothing claimed yet doesn't auto-claim one; that would mean deciding how a
// scan interacts with the ticket queue's own assignment rules, which this
// endpoint deliberately leaves alone rather than guessing at.
func (s *Server) handleScanBag(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if err := s.legoDB.RequireClockedIn(sess.Username); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	var req scanBagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code is required"})
		return
	}
	c, err := s.loadSortedCheck(r.Context(), sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if c == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nothing claimed to check — claim a set first, then scan its bag"})
		return
	}
	if c.ID == 0 {
		if err := s.legoDB.SaveCheck(c); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	res, err := s.legoDB.ScanBag(c.ID, c.SetNum, req.Code)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	switch {
	case res.Bag != nil:
		s.audit.Log(sess.Username, sess.Role, "MOBILE_BAG_SCAN", "SUCCESS", c.SetNum+" "+res.Bag.Code)
		view := s.lineViewAt(c, min(c.LastPosition, max(0, len(c.Lines)-1)), sess.Username)
		writeJSON(w, http.StatusOK, map[string]any{"line": view, "bag_kind": res.Bag.BagKindKey, "bag_code": res.Bag.Code})
	case res.OtherSet != "":
		writeJSON(w, http.StatusConflict, map[string]string{"error": req.Code + " is a bag for set " + res.OtherSet + ", not this one"})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown bag code: " + req.Code})
	}
}

// handleBagFind is the lost-bag lookup: what a code belongs to, independent
// of any claimed check — a bag found without its checker, or double-
// checking a label before it goes on (see DB.FindBag). Read-only, any
// signed-in session; unlike handleScanBag it never registers or marks
// anything scanned.
func (s *Server) handleBagFind(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code is required"})
		return
	}
	res, err := s.legoDB.FindBag(code)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !res.Found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "nothing known about bag code " + code})
		return
	}
	scanned := ""
	if !res.ScannedAt.IsZero() {
		scanned = res.ScannedAt.Format(time.RFC3339)
	}
	out := map[string]any{
		"code": res.Code, "set": res.SetNum, "check_id": res.CheckID, "check_done": res.CheckDone,
		"bag_kind": res.BagKindKey, "bag_kind_name": res.BagKindName, "scanned_at": scanned,
		"pool": res.Pool, "pool_status": res.PoolStatus,
	}
	// Who printed it and what its check turned up -- admin-only, same as the
	// TUI's Find a Bag screen: everyone else just needs "what is this bag".
	if s.isAdmin(sess) {
		out["created_by"] = res.CreatedBy
		if !res.CreatedAt.IsZero() {
			out["created_at"] = res.CreatedAt.Format(time.RFC3339)
		}
		if res.CheckDone {
			out["missing"] = res.Missing
			out["extra"] = res.Extra
		}
		out["disputed"] = res.Disputed
		if res.Disputed {
			out["dispute_note"] = res.DisputeNote
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type confirmRequest struct {
	Position int    `json:"position"`
	Action   string `json:"action"` // check: "have_all" | "missing" | "extra" | "have" — order: "receive" | "unreceive"
	Count    int    `json:"count"`  // used by missing/extra/have/receive
	// BagCode is the bag the checker currently has in hand, scanned/typed
	// fresh on this request, or "" to mean "whatever I already scanned
	// earlier this session" (see confirmCheckLine's bagWarning) — mobile is
	// stateless, so "already scanned" lives in check_bags.scanned_at, not in
	// any server-held session state. Ignored for an order (no bag gate).
	BagCode string `json:"bag_code,omitempty"`
}

// handleConfirm is the one write this app needs — optional, since the
// scanner isn't required (see the printable barcode sheet, set_check.go's
// P key), just a faster way to mark a line than opening the TUI.
//
// For a check: mirrors H/M/E exactly (set_check.go's HandleKey) and
// persists through SaveCheck, the same draft-save path the TUI's own S key
// uses, so nothing is lost between taps and the TUI sees the same state if
// opened concurrently.
//
// For an order: "receive" books count more as arrived via ReceiveLine —
// the exact same call Order Lines' own R key makes — count is incremental
// (how many just showed up), not a total, since that's what ReceiveLine
// itself expects.
func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if err := s.legoDB.RequireClockedIn(sess.Username); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	var req confirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}

	c, err := s.loadSortedCheck(r.Context(), sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if c != nil {
		s.confirmCheckLine(w, sess, c, req)
		return
	}

	o, err := s.loadSortedOrder(sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if o != nil {
		s.confirmOrderLine(w, sess, o, req)
		return
	}

	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nothing assigned"})
}

// bagWarningFor is confirmCheckLine's bag-gate check — mirrors the TUI's own
// bagWarningFor (set_check.go) but stateless: a bag_code sent on this request
// is resolved and recorded fresh; with none sent, "already scanned" means a
// scanned check_bags row already exists for the required kind from an
// earlier request this session. Never blocks: the caller still applies the
// mark regardless of what this returns (see docs/guides/set-checks.md).
func (s *Server) bagWarningFor(c *lego.SetCheck, l lego.CheckLine, action, bagCode string) string {
	if !s.legoDB.GetSetState(c.SetNum).BagGatingOn() {
		return ""
	}
	k, err := s.legoDB.RequiredBagKind(l, action)
	if err != nil || k == nil {
		return ""
	}
	if bagCode != "" {
		res, err := s.legoDB.ScanBag(c.ID, c.SetNum, bagCode)
		if err != nil {
			return ""
		}
		switch {
		case res.Bag != nil && res.Bag.BagKindKey == k.Key:
			return ""
		case res.Bag != nil:
			return fmt.Sprintf("wrong bag — this part needs the %s bag, not %s. Scan a different bag ID label.", k.Name, res.Bag.Code)
		case res.OtherSet != "":
			return fmt.Sprintf("%s is a bag for set %s, not this one — scan a different bag ID label", bagCode, res.OtherSet)
		default:
			return "unknown bag code: " + bagCode
		}
	}
	if scanned, err := s.legoDB.BagScannedForKind(c.ID, k.ID); err == nil && scanned {
		return ""
	}
	return fmt.Sprintf("no bag scanned — this part needs the %s bag", k.Name)
}

func (s *Server) confirmCheckLine(w http.ResponseWriter, sess lego.MobileSession, c *lego.SetCheck, req confirmRequest) {
	if req.Position < 0 || req.Position >= len(c.Lines) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no such line"})
		return
	}
	l := &c.Lines[req.Position]
	bagAction := req.Action
	if bagAction == "have_all" {
		bagAction = "have"
	}
	bagWarning := s.bagWarningFor(c, *l, bagAction, req.BagCode)
	switch req.Action {
	case "have_all":
		l.Have = l.Need
	case "missing":
		l.Have = max(0, l.Need-req.Count)
	case "extra":
		l.Extra = req.Count
		if req.Count > 0 {
			// The first extra this session: create the extra bag's instance
			// now (it isn't printed up front — see docs/guides/set-checks.md).
			_, _ = s.legoDB.EnsureCheckBag(c.ID, c.SetNum, lego.BagExtra, sess.Username)
		}
	case "have":
		l.Have = req.Count
	case "damaged":
		l.Damaged = req.Count
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be have_all, missing, extra, have, or damaged"})
		return
	}
	if err := s.legoDB.SaveCheck(c); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = s.legoDB.AdvanceCheckPosition(c.ID, req.Position+1) // best-effort: a missed resume point is a UX inconvenience, not a data problem
	s.audit.Log(sess.Username, sess.Role, "MOBILE_CONFIRM", "SUCCESS", c.SetNum+" line "+strconv.Itoa(req.Position)+": "+req.Action)
	view := s.lineViewAt(c, req.Position, sess.Username)
	view.BagWarning = bagWarning
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) confirmOrderLine(w http.ResponseWriter, sess lego.MobileSession, o *lego.Order, req confirmRequest) {
	if req.Position < 0 || req.Position >= len(o.Lines) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no such line"})
		return
	}
	if req.Action != "receive" && req.Action != "unreceive" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be receive or unreceive"})
		return
	}
	if req.Count <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "count must be positive"})
		return
	}
	l := o.Lines[req.Position]
	var completedSet string
	var err error
	if req.Action == "receive" {
		completedSet, err = s.legoDB.ReceiveLine(l.ID, req.Count)
	} else {
		err = s.legoDB.UnreceiveLine(l.ID, req.Count)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = s.legoDB.AdvanceOrderPosition(o.ID, req.Position+1) // best-effort, same as confirmCheckLine
	s.audit.Log(sess.Username, sess.Role, "MOBILE_CONFIRM", "SUCCESS", "order "+strconv.FormatInt(o.ID, 10)+" line "+strconv.Itoa(req.Position)+": "+req.Action+" "+strconv.Itoa(req.Count))

	// Re-fetch so the response reflects ReceiveLine/UnreceiveLine's own
	// clamping (each caps count at what's actually still outstanding).
	fresh, err := s.legoDB.GetOrder(o.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	fresh.Lines = s.legoDB.SortedOrderLines(s.pdb, fresh.Lines)
	view := s.lineViewAtOrder(fresh, req.Position, sess.Username)
	view.CompletedSet = completedSet
	writeJSON(w, http.StatusOK, view)
}
