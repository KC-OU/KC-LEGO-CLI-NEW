package mobileapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
)

// missingLine is one still-short line in a finish warning/summary. Position
// is 1-based, same convention as lineView.Position, so a client can jump
// straight to it (GET /mobile/next?pos=Position-1) without a second lookup.
type missingLine struct {
	Position  int    `json:"position"`
	PartNum   string `json:"part_num"`
	ColorName string `json:"color_name"`
	Name      string `json:"name"`
	Missing   int    `json:"missing"`
}

type finishSummary struct {
	Kind          string        `json:"kind"` // "check" | "order"
	Pieces        int           `json:"pieces,omitempty"`
	Have          int           `json:"have,omitempty"`
	Missing       int           `json:"missing"`
	Extra         int           `json:"extra,omitempty"`
	Lines         []missingLine `json:"lines,omitempty"` // only the still-missing ones
	Finished      bool          `json:"finished"`        // false = a warning only, nothing was recorded
	AccuracyPct   float64       `json:"accuracy_pct"`
	PartDBNote    string        `json:"part_db_note,omitempty"`
	NeedsBagCode  bool          `json:"needs_bag_code,omitempty"`
	SmallBagParts []string      `json:"small_bag_parts,omitempty"` // which lines are why a bag code is needed
}

type finishRequest struct {
	Force   bool   `json:"force"`
	BagCode string `json:"bag_code"` // the small bag's own barcode/number, scanned or typed — see lego.RecordCheckBag
}

// handleFinish is the one thing /next and /confirm never covered: actually
// closing out a check or order. Without it, a check or order worked entirely
// from the phone never called FinishCheck/FinishTicket/RecordCheckOutcome —
// set_check.go's F key and workshop.go's "mark received" are the TUI's only
// paths that do, so the ticket stayed claimed forever, nothing reached
// Part-DB, and accuracy never moved. This mirrors both exactly.
//
// Without force, anything still missing comes back as a warning (Finished:
// false) instead of being completed, so the client can show "3 parts still
// missing — finish anyway?" with the specifics, before the picker/checker
// loses the chance to go back and fill them in. Force finishes regardless —
// for a check that's the same as pressing F with lines still marked missing
// (it gets logged and docked, not silently dropped); for an order it's the
// same as the TUI's "mark received" on a partially-received order (it force-
// receives what's left, same as that always has).
func (s *Server) handleFinish(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	var req finishRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // an empty body just means force=false

	c, err := s.loadSortedCheck(r.Context(), sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if c != nil {
		s.finishCheckMobile(w, sess, c, req.Force, req.BagCode)
		return
	}

	o, err := s.loadSortedOrder(sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if o != nil {
		s.finishOrderMobile(w, sess, o, req.Force)
		return
	}

	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nothing assigned"})
}

func missingCheckLines(c *lego.SetCheck) []missingLine {
	var out []missingLine
	for i, l := range c.Lines {
		if m := l.Missing(); m > 0 {
			out = append(out, missingLine{Position: i + 1, PartNum: l.PartNum, ColorName: l.ColorName, Name: l.PartName, Missing: m})
		}
	}
	return out
}

func (s *Server) finishCheckMobile(w http.ResponseWriter, sess lego.MobileSession, c *lego.SetCheck, force bool, bagCode string) {
	pieces, have, missing, extra, _ := c.Totals()
	if missing > 0 && !force {
		writeJSON(w, http.StatusConflict, finishSummary{
			Kind: "check", Pieces: pieces, Have: have, Missing: missing, Extra: extra,
			Lines: missingCheckLines(c), Finished: false,
		})
		return
	}

	// At least one small-bag line and no bag code confirmed yet (this request or
	// an earlier one): hold the finish — "a verified link", not just a printed
	// label, so this isn't optional the way bagging itself is. Only a read here
	// (c.ID may still be 0 for a check that's never been saved before — that's
	// fine for a lookup, CheckBagCode just reports "no code yet" for it; the
	// actual RecordCheckBag call happens after FinishCheck below, once c.ID is
	// guaranteed to be the real, final one).
	small := lego.SmallBagLines(c)
	if len(small) > 0 {
		if existing, _ := s.legoDB.CheckBagCode(c.ID); existing == "" && bagCode == "" {
			parts := make([]string, len(small))
			for i, l := range small {
				parts[i] = l.PartNum
			}
			writeJSON(w, http.StatusConflict, finishSummary{
				Kind: "check", Pieces: pieces, Have: have, Missing: missing, Extra: extra,
				Finished: false, NeedsBagCode: true, SmallBagParts: parts,
			})
			return
		}
	}

	extrasParts, err := s.legoDB.FinishCheck(c)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(small) > 0 && bagCode != "" {
		if err := s.legoDB.RecordCheckBag(c.ID, bagCode, sess.Username); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	action := "SET_CHECKED"
	if c.Kind == lego.CheckRecount {
		action = "STOCK_CHECK"
	}
	s.audit.Log(sess.Username, sess.Role, action, "SUCCESS", fmt.Sprintf("set=%s pieces=%d missing=%d extra=%d (mobile)", c.SetNum, pieces, missing, extra))
	_ = s.legoDB.FinishTicket(lego.TicketCheck, c.SetNum, sess.Username)
	if _, escalate, err := s.legoDB.RecordCheckOutcome(sess.Username, lego.AccuracyChecker, lego.TicketCheck, c.SetNum, pieces, missing); err == nil && escalate {
		go notify.Dispatch("accuracy_escalation", fmt.Sprintf("%s is missing %d parts on %s — accuracy needs a manual review, not an automatic deduction.", sess.Username, missing, c.SetNum), "")
		_ = s.legoDB.LogEvent(lego.EventAccuracyEscalation, sess.Username, c.SetNum, fmt.Sprintf("missing %d on %s — needs manual review", missing, c.SetNum))
	}
	if missing > 0 {
		go notify.Dispatch("set_incomplete", fmt.Sprintf("Set %s is missing %d part(s)", c.SetNum, missing), "")
		_ = s.legoDB.LogEvent(lego.EventMissingParts, sess.Username, c.SetNum, fmt.Sprintf("missing %d part(s)", missing))
	}

	accNow, _ := s.legoDB.AccuracyToday(sess.Username, lego.AccuracyChecker)
	summary := finishSummary{Kind: "check", Pieces: pieces, Have: have, Missing: missing, Extra: extra, Finished: true, AccuracyPct: accNow}

	if s.pdbw == nil || !s.pdbw.API().Enabled() {
		summary.PartDBNote = "Part-DB not updated: no API token configured."
		writeJSON(w, http.StatusOK, summary)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	syncer := &lego.PartSyncer{Lego: s.legoDB, Writer: s.pdbw}
	setName := ""
	if cs, _ := s.legoDB.CatalogSet(c.SetNum); cs != nil {
		setName = cs.Name
	}
	switch res, err := syncer.PushSet(ctx, c, setName); {
	case err != nil:
		summary.PartDBNote = "Part-DB not updated: " + err.Error()
	case len(res.Errors) > 0:
		summary.PartDBNote = fmt.Sprintf("Part-DB: %d line(s) failed (%v).", len(res.Errors), res.Errors[0])
	default:
		summary.PartDBNote = fmt.Sprintf("Part-DB: %d line(s) updated.", res.Lines)
	}
	for _, p := range extrasParts {
		_, _ = syncer.Push(ctx, p)
	}
	writeJSON(w, http.StatusOK, summary)
}

func missingOrderLines(o *lego.Order) (missing int, lines []missingLine) {
	for i, l := range o.Lines {
		if m := l.Qty - l.ReceivedQty; m > 0 {
			missing += m
			lines = append(lines, missingLine{Position: i + 1, PartNum: l.PartNum, ColorName: l.ColorName, Name: l.PartName, Missing: m})
		}
	}
	return missing, lines
}

func (s *Server) finishOrderMobile(w http.ResponseWriter, sess lego.MobileSession, o *lego.Order, force bool) {
	missing, lines := missingOrderLines(o)
	if missing > 0 && !force {
		writeJSON(w, http.StatusConflict, finishSummary{Kind: "order", Missing: missing, Lines: lines, Finished: false})
		return
	}

	done, err := s.legoDB.SetOrderStatus(o.ID, "received")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.audit.Log(sess.Username, sess.Role, "ORDER_STATUS", "SUCCESS", fmt.Sprintf("order #%d received (mobile)", o.ID))
	_ = s.legoDB.FinishTicket(lego.TicketOrder, strconv.FormatInt(o.ID, 10), sess.Username)
	_, _, _ = s.legoDB.RecordCheckOutcome(sess.Username, lego.AccuracyPicker, lego.TicketOrder, strconv.FormatInt(o.ID, 10), 1, 0)
	for _, set := range done {
		go notify.Dispatch("set_complete", "Set "+set+" is complete", "All missing parts have arrived.")
	}

	accNow, _ := s.legoDB.AccuracyToday(sess.Username, lego.AccuracyPicker)
	summary := finishSummary{Kind: "order", Missing: 0, Finished: true, AccuracyPct: accNow}

	if s.pdbw == nil || !s.pdbw.API().Enabled() {
		summary.PartDBNote = "Part-DB not updated: no API token configured."
		writeJSON(w, http.StatusOK, summary)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	syncer := &lego.PartSyncer{Lego: s.legoDB, Writer: s.pdbw}
	fresh, _ := s.legoDB.GetOrder(o.ID)
	pushed := map[string]bool{}
	if fresh != nil {
		for _, l := range fresh.Lines {
			if l.SetNum == "" || pushed[l.SetNum] {
				continue
			}
			pushed[l.SetNum] = true
			if c, _ := s.legoDB.LastCheck(l.SetNum); c != nil {
				name := ""
				if cs, _ := s.legoDB.CatalogSet(l.SetNum); cs != nil {
					name = cs.Name
				}
				_, _ = syncer.PushSet(ctx, c, name)
			}
		}
	}
	summary.PartDBNote = "Part-DB sync attempted for the sets this order filled."
	writeJSON(w, http.StatusOK, summary)
}
