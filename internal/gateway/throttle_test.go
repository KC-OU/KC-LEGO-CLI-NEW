package gateway

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testThrottle() (*Throttle, *time.Time) {
	th := NewThrottle()
	clock := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	th.now = func() time.Time { return clock }
	return th, &clock
}

func TestSessionsPerAddressAreCapped(t *testing.T) {
	th, _ := testThrottle()
	var rel []func()
	for i := 0; i < th.MaxSessions; i++ {
		r, why := th.Admit("203.0.113.9")
		if r == nil {
			t.Fatalf("session %d refused: %s", i, why)
		}
		rel = append(rel, r)
	}
	if r, why := th.Admit("203.0.113.9"); r != nil || why == "" {
		t.Fatal("one session too many must be refused with a reason")
	}
	if r, _ := th.Admit("203.0.113.10"); r == nil {
		t.Fatal("another address is unaffected")
	}
	rel[0]()
	if r, _ := th.Admit("203.0.113.9"); r == nil {
		t.Fatal("a released slot can be used again")
	}
}

// TestConcurrentAdmitNeverExceedsTheCap hammers one address from many
// goroutines at once (real telnet connections each run Admit/release on
// their own goroutine — see acceptLoop/handleTelnetSession) and checks the
// session cap holds exactly, not just under the sequential access
// TestSessionsPerAddressAreCapped exercises. Run with -race, this also
// catches any unprotected access to Throttle's internal map.
func TestConcurrentAdmitNeverExceedsTheCap(t *testing.T) {
	th := NewThrottle()
	const addr = "203.0.113.50"
	const attempts = 200

	var admitted atomic.Int32
	var wg sync.WaitGroup
	release := make(chan func(), attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r, _ := th.Admit(addr); r != nil {
				admitted.Add(1)
				release <- r
			}
		}()
	}
	wg.Wait()
	close(release)
	if got := admitted.Load(); got != int32(th.MaxSessions) {
		t.Fatalf("admitted %d concurrent session(s), want exactly MaxSessions=%d", got, th.MaxSessions)
	}
	for r := range release {
		r() // every admitted session must release cleanly, exactly once
	}
	if r, _ := th.Admit(addr); r == nil {
		t.Fatal("after every session released, the address should be admitted again")
	}
}

func TestRepeatedFailedSessionsBlockTheAddressWithGrowingBlocks(t *testing.T) {
	th, clock := testThrottle()
	addr := "198.51.100.7"
	for i := 0; i < th.Strikes; i++ {
		th.Strike(addr)
	}
	if r, why := th.Admit(addr); r != nil || why == "" {
		t.Fatal("the address should be blocked after enough failed sessions")
	}
	*clock = clock.Add(th.Block + time.Second)
	if r, _ := th.Admit(addr); r == nil {
		t.Fatal("the block should lapse")
	}
	for i := 0; i < th.Strikes; i++ {
		th.Strike(addr)
	}
	*clock = clock.Add(th.Block + time.Second)
	if r, _ := th.Admit(addr); r != nil {
		t.Fatal("the second block must be longer than the first")
	}
	*clock = clock.Add(th.Block)
	if r, _ := th.Admit(addr); r == nil {
		t.Fatal("the doubled block should have lapsed by now")
	}
}

func TestOldStrikesExpireAndLoopbackIsExempt(t *testing.T) {
	th, clock := testThrottle()
	th.Strike("198.51.100.8")
	th.Strike("198.51.100.8")
	*clock = clock.Add(th.StrikeTTL + time.Minute)
	th.Strike("198.51.100.8") // the first two no longer count
	if r, _ := th.Admit("198.51.100.8"); r == nil {
		t.Fatal("stale strikes must not add up to a block")
	}
	for i := 0; i < 10; i++ {
		th.Strike("127.0.0.1")
	}
	if r, _ := th.Admit("127.0.0.1"); r == nil {
		t.Fatal("loopback is never throttled")
	}
}

func TestIdleAddressesAreForgotten(t *testing.T) {
	th, _ := testThrottle()
	r, _ := th.Admit("192.0.2.1")
	r()
	if len(th.m) != 0 {
		t.Errorf("state for a finished, clean session should be dropped: %v", th.m)
	}
}

// End to end: sessions whose TUI exits with ExitTooManyFailures earn strikes,
// and the address is then turned away at the door without spawning anything.
func TestGatewayBlocksAnAddressWhoseSessionsKeepFailingSignIn(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-wms")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 42\n"), 0755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	th := NewThrottle()
	th.KeepLoopback = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go acceptLoop(ctx, ln, fake, th)

	dial := func() string {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		b, _ := io.ReadAll(c)
		return string(b)
	}
	for i := 0; i < th.Strikes; i++ {
		if out := dial(); strings.Contains(out, "Too many") {
			t.Fatalf("session %d should still be admitted: %q", i, out)
		}
	}
	// The strike is recorded after the child is reaped, just after the client sees EOF.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if out := dial(); strings.Contains(out, "Too many failed sign-ins") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the address was never blocked")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
