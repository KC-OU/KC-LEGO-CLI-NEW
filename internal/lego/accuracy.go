package lego

import (
	"time"
)

// A picker/checker's accuracy starts today at 100% and only this table's rows for
// today move it — there's nothing to reset at midnight, "today" is just today's
// date, so a new day starts with no rows and is 100% by definition. Tracked per
// role: someone holding both the picker and checker groups has two independent
// percentages.
const (
	AccuracyPicker  = "picker"
	AccuracyChecker = "checker"

	accKindSetCheck  = "set_check"
	accKindOrderPick = "order_pick"
	accKindAdminDock = "admin_dock"
	accKindRecovery  = "recovery"
	accKindEscalate  = "escalation" // 21+ missing: flagged, never auto-deducted
)

// DeductionFor is how many percentage points a completed check/order with this
// many missing parts costs, scaling linearly within its band — ponytail: the bands
// and their linear scaling are exactly what was signed off on; they're a flat
// per-missing-part cost, not scaled by the set's total piece count, so a review of
// where that trade-off matters is worth doing once this has real usage behind it.
// 21+ never auto-deducts; escalate reports that so the caller can flag it instead.
func DeductionFor(missing int) (delta float64, escalate bool) {
	switch {
	case missing <= 0:
		return 0, false
	case missing <= 5:
		return 2 + float64(missing-1)*1.0, false // 2 .. 6
	case missing <= 15:
		return 10 + float64(missing-6)*(4.0/9.0), false // 10 .. 14
	case missing <= 20:
		return 15 + float64(missing-16)*1.25, false // 15 .. 20
	default:
		return 0, true
	}
}

// recoveryRate is the fraction of today's remaining deficit a clean (0 missing)
// set claws back — half each time, so it converges toward 100% over a few clean
// sets in a row rather than snapping back after just one.
const recoveryRate = 0.5

// AccuracyToday is 100 plus every delta logged for user/role today.
func (d *DB) AccuracyToday(username, role string) (float64, error) {
	var sum float64
	err := d.QueryRow(`SELECT COALESCE(SUM(delta), 0) FROM accuracy_log WHERE username = ? AND role = ? AND day = ?`,
		username, role, today()).Scan(&sum)
	return max(0, min(100, 100+sum)), err
}

// RecordCheckOutcome logs a completed check or order pick's effect on username's
// accuracy for role, returning the delta actually applied (positive is a recovery,
// negative a deduction, zero on a flagged 21+ escalation).
func (d *DB) RecordCheckOutcome(username, role, kind, target string, pieces, missing int) (delta float64, escalate bool, err error) {
	if missing == 0 {
		current, err := d.AccuracyToday(username, role)
		if err != nil {
			return 0, false, err
		}
		delta = (100 - current) * recoveryRate
		if delta > 0 {
			err = d.logAccuracy(username, role, accKindRecovery, target, pieces, missing, delta, "", "")
		}
		return delta, false, err
	}
	delta, escalate = DeductionFor(missing)
	if escalate {
		return 0, true, d.logAccuracy(username, role, accKindEscalate, target, pieces, missing, 0, "", "")
	}
	delta = -delta
	akind := accKindSetCheck
	if kind == TicketOrder {
		akind = accKindOrderPick
	}
	return delta, false, d.logAccuracy(username, role, akind, target, pieces, missing, delta, "", "")
}

// DockAccuracy is an admin's manual deduction (the 21+ "have a word instead"
// case, or anything else worth a conversation) — never automatic.
func (d *DB) DockAccuracy(username, role string, amount float64, reason, by string) error {
	return d.logAccuracy(username, role, accKindAdminDock, "", 0, 0, -amount, reason, by)
}

func (d *DB) logAccuracy(username, role, kind, target string, pieces, missing int, delta float64, reason, by string) error {
	_, err := d.Exec(`INSERT INTO accuracy_log (username, role, day, kind, target, pieces, missing, delta, reason, created_by, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		username, role, today(), kind, target, pieces, missing, delta, reason, by, time.Now().Format(time.RFC3339))
	return err
}

// AccuracyEvent is one line of AccuracyHistory.
type AccuracyEvent struct {
	Kind, Target, Reason string
	Pieces, Missing      int
	Delta                float64
	CreatedAt            time.Time
}

// AccuracyHistory is username/role's events for the given day ("" = today), oldest first.
func (d *DB) AccuracyHistory(username, role, day string) ([]AccuracyEvent, error) {
	if day == "" {
		day = today()
	}
	rows, err := d.Query(`SELECT kind, target, reason, pieces, missing, delta, created_at FROM accuracy_log
		WHERE username = ? AND role = ? AND day = ? ORDER BY id`, username, role, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccuracyEvent
	for rows.Next() {
		var e AccuracyEvent
		var createdAt string
		if err := rows.Scan(&e.Kind, &e.Target, &e.Reason, &e.Pieces, &e.Missing, &e.Delta, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, e)
	}
	return out, rows.Err()
}

// AccuracyTrend is today's score followed by each of the last n-1 days before it,
// oldest last-n first — for the "7/30-day trend" next to today's number.
func (d *DB) AccuracyTrend(username, role string, days int) ([]float64, error) {
	out := make([]float64, 0, days)
	for i := days - 1; i >= 0; i-- {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		var sum float64
		if err := d.QueryRow(`SELECT COALESCE(SUM(delta), 0) FROM accuracy_log WHERE username = ? AND role = ? AND day = ?`,
			username, role, day).Scan(&sum); err != nil {
			return nil, err
		}
		out = append(out, max(0, min(100, 100+sum)))
	}
	return out, nil
}
