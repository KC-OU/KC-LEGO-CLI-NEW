package notify

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/backup"
)

// Monitor watches the things that need a human and sends at most one alert per
// kind per cooldown, so an ongoing problem nags instead of flooding.
type Monitor struct {
	Send      func(context.Context, Message) error
	Audit     *audit.Logger
	BackupDir string
	// LowStock returns one line per part that is below its minimum (nil = none/unused).
	LowStock func() []string
	// PriceDrops checks the price watchlist (it may call BrickLink) and returns one line per
	// watch at or under its limit. It is called at most once a day.
	PriceDrops func(ctx context.Context) []string
	// OnEvent, when set, is told about "low_stock" and "price_drop" (with the lines), so plugins can react.
	OnEvent func(event string, lines []string)
	Now     func() time.Time

	// FailedLogins in FailedWindow raise the burst alert.
	FailedLogins int
	FailedWindow time.Duration

	last map[string]time.Time
}

func NewMonitor(send func(context.Context, Message) error, a *audit.Logger, backupDir string) *Monitor {
	return &Monitor{Send: send, Audit: a, BackupDir: backupDir, Now: time.Now, FailedLogins: 10, FailedWindow: 10 * time.Minute, last: map[string]time.Time{}}
}

// due reports whether kind may alert now, and records it if so.
func (m *Monitor) due(kind string, cooldown time.Duration) bool {
	now := m.Now()
	if t, ok := m.last[kind]; ok && now.Sub(t) < cooldown {
		return false
	}
	m.last[kind] = now
	return true
}

func (m *Monitor) alert(ctx context.Context, kind string, cooldown time.Duration, msg Message) {
	if m.due(kind, cooldown) {
		_ = m.Send(ctx, msg) // an unreachable alert endpoint must never disturb the gateway
	}
}

// Check runs every watch once.
func (m *Monitor) Check(ctx context.Context) {
	m.checkFailedLogins(ctx)
	m.checkChain(ctx)
	m.checkBackup(ctx)
	m.checkPrices(ctx)
	if m.LowStock != nil {
		if low := m.LowStock(); len(low) > 0 {
			if m.OnEvent != nil && m.due("lowstock-event", 24*time.Hour) {
				m.OnEvent("low_stock", low)
			}
			body := strings.Join(low[:min(len(low), 15)], "\n")
			if len(low) > 15 {
				body += fmt.Sprintf("\n… and %d more", len(low)-15)
			}
			m.alert(ctx, "lowstock", 24*time.Hour, Message{Title: fmt.Sprintf("Low stock: %d part(s)", len(low)), Body: body, Priority: 3, Tag: "package"})
		}
	}
}

func (m *Monitor) checkPrices(ctx context.Context) {
	if m.PriceDrops == nil || !m.due("pricecheck", 24*time.Hour) {
		return
	}
	if lines := m.PriceDrops(ctx); len(lines) > 0 {
		if m.OnEvent != nil {
			m.OnEvent("price_drop", lines)
		}
		body := strings.Join(lines[:min(len(lines), 15)], "\n")
		_ = m.Send(ctx, Message{Title: fmt.Sprintf("Price drop: %d item(s)", len(lines)), Body: body, Priority: 3, Tag: "moneybag"})
	}
}

// Heartbeat sends the audit chain's head hash, so a copy exists somewhere the
// box cannot rewrite (see the audit package's limits).
func (m *Monitor) Heartbeat(ctx context.Context) {
	r, err := m.Audit.Verify()
	if err != nil || r.Head == "" {
		return
	}
	state := "intact"
	if !r.OK() {
		state = fmt.Sprintf("BROKEN at line %d", r.BrokenAt)
	}
	_ = m.Send(ctx, Message{Title: "Audit log head", Body: fmt.Sprintf("chain %s, %d chained lines\n%s", state, r.Chained, r.Head), Priority: 1, Tag: "lock"})
}

func (m *Monitor) checkFailedLogins(ctx context.Context) {
	lines, err := m.Audit.Tail(400)
	if err != nil {
		return
	}
	since := m.Now().Add(-m.FailedWindow)
	n := 0
	for _, ln := range lines {
		if !strings.Contains(ln, "ACTION:LOGIN") || !strings.Contains(ln, "STATUS:FAILED") || len(ln) < 21 || ln[0] != '[' {
			continue
		}
		if ts, err := time.ParseInLocation("2006-01-02 15:04:05", ln[1:20], m.Now().Location()); err == nil && ts.After(since) {
			n++
		}
	}
	if n >= m.FailedLogins {
		m.alert(ctx, "failedlogins", time.Hour, Message{
			Title: "Failed sign-ins", Priority: 4, Tag: "warning",
			Body: fmt.Sprintf("%d failed sign-ins in the last %d minutes. Someone may be guessing passwords; see `wms audit verify` and the audit log.", n, int(m.FailedWindow/time.Minute)),
		})
	}
}

func (m *Monitor) checkChain(ctx context.Context) {
	if r, err := m.Audit.Verify(); err == nil && !r.OK() {
		m.alert(ctx, "chain", 6*time.Hour, Message{
			Title: "Audit log was changed", Priority: 5, Tag: "rotating_light",
			Body: fmt.Sprintf("The audit log's hash chain is broken at line %d: %s.", r.BrokenAt, r.BrokenWhy),
		})
	}
}

func (m *Monitor) checkBackup(ctx context.Context) {
	if m.BackupDir == "" {
		return
	}
	if _, err := os.Stat(m.BackupDir); err != nil && !os.IsNotExist(err) {
		return
	}
	name, mod, ok := backup.Newest(m.BackupDir)
	switch {
	case !ok:
		m.alert(ctx, "backup", 24*time.Hour, Message{Title: "No backups", Body: "No ModernWMS backup exists in " + m.BackupDir + ". Run: wms backup", Priority: 4, Tag: "floppy_disk"})
	case m.Now().Sub(mod) > backup.StaleAfter:
		m.alert(ctx, "backup", 24*time.Hour, Message{
			Title: "Backup is stale", Priority: 4, Tag: "floppy_disk",
			Body: fmt.Sprintf("The newest ModernWMS backup (%s) is %d days old. Run: wms backup", name, int(m.Now().Sub(mod).Hours()/24)),
		})
	}
}

// Run checks every few minutes until ctx ends, and sends the heartbeat at
// start-up and then once a day.
func (m *Monitor) Run(ctx context.Context) {
	m.Heartbeat(ctx)
	checks := time.NewTicker(5 * time.Minute)
	beats := time.NewTicker(24 * time.Hour)
	defer checks.Stop()
	defer beats.Stop()
	m.Check(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-checks.C:
			m.Check(ctx)
		case <-beats.C:
			m.Heartbeat(ctx)
		}
	}
}
