package gateway

import (
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

// session wraps one child `wms tui` process and its PTY master with exactly
// one goroutine (pump) ever reading the master for the process's whole
// life — including across a reconnect's hand-off from one net.Conn to the
// next. That single-reader invariant is what makes reconnect safe: two
// connections' io.Copy-style loops both blocked on the same PTY master would
// race for whatever the child writes next, and the loser would silently
// drop it (or worse, write it to the wrong, already-closed connection).
type session struct {
	cmd  *exec.Cmd
	ptmx *os.File

	mu          sync.Mutex
	current     net.Conn
	currentDone func() // the attached connection's closeDone, called if a write to it fails
}

func newSession(cmd *exec.Cmd, ptmx *os.File) *session {
	s := &session{cmd: cmd, ptmx: ptmx}
	go s.pump()
	return s
}

// pump continuously drains ptmx and forwards to whichever connection is
// currently attached, for as long as the child lives. While detached
// (parked, between connections) it keeps draining and discards the output —
// deliberately: that only happens for an idle app during a brief reconnect
// gap, and actively draining means the child can never block on a full PTY
// buffer waiting for a reader that isn't coming back.
func (s *session) pump() {
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.mu.Lock()
			c := s.current
			s.mu.Unlock()
			if c != nil {
				if _, werr := c.Write(buf[:n]); werr != nil {
					s.signalCurrentDone(c)
				}
			}
		}
		if err != nil {
			// The child exited (or its PTY slave otherwise closed) on its
			// own — not triggered by the attached connection going away.
			// Whoever's currently attached needs to know the session is
			// over just the same, or they'd sit waiting for output from a
			// process that's already gone (matches the original io.Copy's
			// EOF-ends-the-session behaviour before this type existed).
			s.mu.Lock()
			c := s.current
			s.mu.Unlock()
			if c != nil {
				s.signalCurrentDone(c)
			}
			return
		}
	}
}

// signalCurrentDone detaches c (only if it's still the attached connection)
// and calls its closeDone, if any.
func (s *session) signalCurrentDone(c net.Conn) {
	s.mu.Lock()
	if s.current != c {
		s.mu.Unlock()
		return
	}
	s.current = nil
	done := s.currentDone
	s.currentDone = nil
	s.mu.Unlock()
	if done != nil {
		done()
	}
}

// attach makes conn this session's active connection; onWriteFail is called
// (at most once) if a write to conn fails before detach does.
func (s *session) attach(conn net.Conn, onWriteFail func()) {
	s.mu.Lock()
	s.current = conn
	s.currentDone = onWriteFail
	s.mu.Unlock()
}

// detach clears conn as the active connection, but only if it's still the
// one attached — a no-op if pump already detached it after a failed write.
func (s *session) detach(conn net.Conn) {
	s.mu.Lock()
	if s.current == conn {
		s.current = nil
		s.currentDone = nil
	}
	s.mu.Unlock()
}

// killSession kills the child and waits (briefly) for it to exit — once it
// does, pump's blocked Read returns EOF on its own, so there's no need to
// force-close ptmx out from under a concurrent reader.
func killSession(sess *session) {
	if sess.cmd.Process != nil {
		_ = sess.cmd.Process.Kill()
	}
	waitDone := make(chan struct{})
	go func() { _ = sess.cmd.Wait(); close(waitDone) }()
	select {
	case <-waitDone:
	case <-time.After(time.Second):
	}
	_ = sess.ptmx.Close()
}

// reconnectRegistry holds a disconnected session open for a short grace
// window, keyed by the client's remote IP, so a handheld scanner's dropped
// Wi-Fi link can pick the same session back up on reconnect instead of
// restarting at the login screen (the AMT/WT41N0-class use case this was
// built for).
//
// Off by default (grace <= 0, config.TelnetReconnectSeconds unset): holding a
// live, already-authenticated TUI open for whatever connects next from the
// same address is only safe when every device on that network has its own
// IP — behind shared NAT, a different device sharing the address within the
// grace window would be dropped straight into the previous user's signed-in
// session with no password. See docs/guides/telnet-and-web.md before turning
// this on.
type reconnectRegistry struct {
	grace time.Duration

	mu     sync.Mutex
	parked map[string]*parkedSession
}

type parkedSession struct {
	sess  *session
	timer *time.Timer
}

func newReconnectRegistry(grace time.Duration) *reconnectRegistry {
	return &reconnectRegistry{grace: grace, parked: make(map[string]*parkedSession)}
}

// claim removes and returns a still-parked session for host, if any.
func (r *reconnectRegistry) claim(host string) *parkedSession {
	if r == nil || host == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ps, ok := r.parked[host]
	if !ok {
		return nil
	}
	delete(r.parked, host)
	ps.timer.Stop()
	return ps
}

// park holds sess open for the grace window so a reconnect from host can
// claim it; the child is killed if nobody does, or immediately if reconnect
// is off (grace <= 0) or host is unknown. Returns immediately either way.
func (r *reconnectRegistry) park(host string, sess *session) {
	if r == nil || r.grace <= 0 || host == "" {
		killSession(sess)
		return
	}
	ps := &parkedSession{sess: sess}
	ps.timer = time.AfterFunc(r.grace, func() {
		r.mu.Lock()
		cur, still := r.parked[host]
		if still && cur == ps {
			delete(r.parked, host)
		} else {
			still = false
		}
		r.mu.Unlock()
		if still {
			killSession(sess)
		}
	})
	r.mu.Lock()
	if old, ok := r.parked[host]; ok { // shouldn't normally happen — don't leak if it does
		old.timer.Stop()
		killSession(old.sess)
	}
	r.parked[host] = ps
	r.mu.Unlock()
}

// closeAll kills every still-parked session — called on gateway shutdown so
// a parked child never outlives the process that's supposed to own it.
func (r *reconnectRegistry) closeAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	parked := r.parked
	r.parked = make(map[string]*parkedSession)
	r.mu.Unlock()
	for _, ps := range parked {
		ps.timer.Stop()
		killSession(ps.sess)
	}
}
