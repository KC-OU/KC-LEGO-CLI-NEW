package mobileapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui/img"
)

// lineView is one line — a check line or an order line — as the mobile app
// sees it, same shape either way so one screen (PickWalkScreen on the
// mobile side) handles both: Kind tells the client which actions are valid
// (have_all/missing/extra/have for a check, receive for an order). /next
// and /confirm both return this, so a client can show /confirm's result
// without a second round-trip.
type lineView struct {
	Kind         string `json:"kind"` // "check" | "order"
	Position     int    `json:"position"`
	Total        int    `json:"total"`
	Target       string `json:"target"`
	SetName      string `json:"set_name"`
	PartNum      string `json:"part_num"`
	ColorName    string `json:"color_name"`
	ColorRGB     string `json:"color_rgb,omitempty"` // e.g. "C91A09", no leading '#' — "" when unknown
	Name         string `json:"name"`
	Need         int    `json:"need"`
	Have         int    `json:"have"`
	Missing      int    `json:"missing"`
	Extra        int    `json:"extra"`
	Location     string `json:"location"`
	NextLocation string `json:"next_location"`
	Done         bool   `json:"done"`                    // true only when there's nothing assigned at all
	CompletedSet string `json:"completed_set,omitempty"` // set here only when a receive just finished it
	// SetImageURL/PartImageURL are plain Rebrickable CDN URLs, not fetched or
	// cached here — the phone loads them directly (see img.PartURLs; unlike
	// the TUI's guided walk, there's no terminal to render into, so a client
	// that doesn't want pictures just doesn't show the <Image>). "" when
	// there's nothing to show (an unknown set, a colour-less part lookup).
	SetImageURL  string `json:"set_image_url,omitempty"`
	PartImageURL string `json:"part_image_url,omitempty"`
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
	v := lineView{
		Kind: "check", Position: pos + 1, Total: len(c.Lines),
		Target: c.SetNum, PartNum: l.PartNum, ColorName: l.ColorName, Name: l.PartName,
		Need: l.Need, Have: l.Have, Missing: l.Missing(), Extra: l.Extra,
		Location:         s.legoDB.LocationFor(s.pdb, l.PartNum, l.ColorID, l.ColorName),
		ColorRGB:         s.colorRGB(l.ColorID),
		PartImageURL:     s.partImageURL(l.PartNum, l.ColorID),
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

// partImageURL/colorRGB back the mobile app's own picture (see internal/
// uiapp's ShowPictures preference for the TUI's equivalent) — a plain URL
// and hex string, not fetched or decoded here; the phone does that itself.
func (s *Server) partImageURL(partNum string, colorID int) string {
	urls := img.PartURLs(partNum, colorID, nil)
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
	v := lineView{
		Kind: "order", Position: pos + 1, Total: len(o.Lines),
		Target: strconv.FormatInt(o.ID, 10), SetName: o.Supplier,
		PartNum: l.PartNum, ColorName: l.ColorName, Name: l.PartName,
		Need: l.Qty, Have: l.ReceivedQty, Missing: missing,
		Location:         s.legoDB.LocationFor(s.pdb, l.PartNum, l.ColorID, l.ColorName),
		ColorRGB:         s.colorRGB(l.ColorID),
		PartImageURL:     s.partImageURL(l.PartNum, l.ColorID),
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
	writeJSON(w, http.StatusOK, s.lineViewAtOrder(o, min(pos, len(o.Lines)-1), sess.Username))
}

type confirmRequest struct {
	Position int    `json:"position"`
	Action   string `json:"action"` // check: "have_all" | "missing" | "extra" | "have" — order: "receive" | "unreceive"
	Count    int    `json:"count"`  // used by missing/extra/have/receive
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

func (s *Server) confirmCheckLine(w http.ResponseWriter, sess lego.MobileSession, c *lego.SetCheck, req confirmRequest) {
	if req.Position < 0 || req.Position >= len(c.Lines) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no such line"})
		return
	}
	l := &c.Lines[req.Position]
	switch req.Action {
	case "have_all":
		l.Have = l.Need
	case "missing":
		l.Have = max(0, l.Need-req.Count)
	case "extra":
		l.Extra = req.Count
	case "have":
		l.Have = req.Count
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be have_all, missing, extra, or have"})
		return
	}
	if err := s.legoDB.SaveCheck(c); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.audit.Log(sess.Username, sess.Role, "MOBILE_CONFIRM", "SUCCESS", c.SetNum+" line "+strconv.Itoa(req.Position)+": "+req.Action)
	writeJSON(w, http.StatusOK, s.lineViewAt(c, req.Position, sess.Username))
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
