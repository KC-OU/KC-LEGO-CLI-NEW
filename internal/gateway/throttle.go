package gateway

import (
	"net"
	"sync"
	"time"
)

// ExitTooManyFailures is the exit status `wms tui` ends with when a session
// used up its sign-in attempts; the gateway counts it as a strike against the
// client's address.
const ExitTooManyFailures = 42

// Throttle limits what one network address can do to the telnet gateway. Port 23
// is scanned constantly, and every connection costs a full TUI process, so an
// address may hold only a few sessions at once, and one that keeps burning
// through its sign-in attempts is refused for a while. State is in memory: a
// gateway restart clears it, which is fine for a rate limit.
type Throttle struct {
	MaxSessions int           // concurrent sessions per address
	Strikes     int           // failed sessions (within StrikeTTL) that trigger a block
	StrikeTTL   time.Duration // a strike this old no longer counts
	Block       time.Duration // first block; each repeat doubles, up to MaxBlock
	MaxBlock    time.Duration
	// KeepLoopback set (tests only) throttles 127.0.0.1 like any other address.
	KeepLoopback bool

	mu  sync.Mutex
	now func() time.Time
	m   map[string]*addrState
}

type addrState struct {
	active       int
	strikes      []time.Time
	blocks       int
	blockedUntil time.Time
}

func NewThrottle() *Throttle {
	return &Throttle{MaxSessions: 4, Strikes: 3, StrikeTTL: time.Hour, Block: 15 * time.Minute, MaxBlock: time.Hour, now: time.Now, m: map[string]*addrState{}}
}

// exempt: this machine itself is never throttled (local admin, the web gateway).
func (t *Throttle) exempt(addr string) bool {
	ip := net.ParseIP(addr)
	return !t.KeepLoopback && ip != nil && ip.IsLoopback()
}

func (t *Throttle) state(addr string) *addrState {
	s := t.m[addr]
	if s == nil {
		s = &addrState{}
		t.m[addr] = s
	}
	return s
}

// Admit reserves a session slot for addr. When it refuses, reason is a line to
// show the client; otherwise call release when the session ends.
func (t *Throttle) Admit(addr string) (release func(), reason string) {
	if t.exempt(addr) {
		return func() {}, ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.state(addr)
	if now := t.now(); now.Before(s.blockedUntil) {
		return nil, "Too many failed sign-ins from this address. Try again in " + (s.blockedUntil.Sub(now).Round(time.Minute)).String() + "."
	}
	if s.active >= t.MaxSessions {
		return nil, "Too many open sessions from this address."
	}
	s.active++
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if s.active > 0 {
			s.active--
		}
		if s.active == 0 && len(s.strikes) == 0 && !t.now().Before(s.blockedUntil) && s.blocks == 0 {
			delete(t.m, addr) // nothing worth remembering: keep the map from growing with every scanner
		}
	}, ""
}

// Strike records a session from addr that failed its sign-in; enough of them
// in a short while block the address.
func (t *Throttle) Strike(addr string) {
	if t.exempt(addr) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.state(addr)
	now := t.now()
	kept := s.strikes[:0]
	for _, at := range s.strikes {
		if now.Sub(at) < t.StrikeTTL {
			kept = append(kept, at)
		}
	}
	s.strikes = append(kept, now)
	if len(s.strikes) >= t.Strikes {
		d := t.Block
		for i := 0; i < s.blocks && d < t.MaxBlock; i++ {
			d *= 2
		}
		s.blockedUntil = now.Add(min(d, t.MaxBlock))
		s.blocks++
		s.strikes = nil
	}
}
