package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLogAndTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	l := &Logger{Path: path}

	if err := l.Log("admin", "Admin", "LOGIN_MODERNWMS", "SUCCESS", ""); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if err := l.Log("admin", "Admin", "CREATE_USER", "SUCCESS", "user=picker1"); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if err := l.Log("picker1", "Picker", "RECEIVE_STOCK_ASN", "SUCCESS", "part=2780 qty=3"); err != nil {
		t.Fatalf("Log: %v", err)
	}

	lines, err := l.Tail(2)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "CREATE_USER") || !strings.Contains(lines[1], "RECEIVE_STOCK_ASN") {
		t.Errorf("unexpected tail order/content: %v", lines)
	}
	for _, want := range []string{"USER:picker1", "ROLE:Picker", "ACTION:RECEIVE_STOCK_ASN", "STATUS:SUCCESS", "DETAILS:part=2780 qty=3"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("line %q missing %q", lines[1], want)
		}
	}
}

func TestTailMissingFile(t *testing.T) {
	l := &Logger{Path: filepath.Join(t.TempDir(), "does-not-exist.log")}
	lines, err := l.Tail(5)
	if err != nil {
		t.Fatalf("Tail on missing file should not error: %v", err)
	}
	if lines != nil {
		t.Errorf("expected nil lines, got %v", lines)
	}
}

func newLog(t *testing.T) *Logger {
	t.Helper()
	return &Logger{Path: filepath.Join(t.TempDir(), "audit.log")}
}

func logN(t *testing.T, l *Logger, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := l.Log("admin", "Admin", "ACTION", "SUCCESS", fmt.Sprintf("n=%d", i)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFreshLogChainsAndVerifies(t *testing.T) {
	l := newLog(t)
	logN(t, l, 5)
	r, err := l.Verify()
	if err != nil || !r.OK() || r.Chained != 6 || r.Genesis != 1 || r.Head == "" || len(r.Foreign) != 0 {
		t.Fatalf("report = %+v err=%v", r, err)
	}
}

func TestTheChainPinsHistoryThatWasThereBefore(t *testing.T) {
	l := newLog(t)
	old := "[2026-08-10 08:01:40] USER:viewonly | ROLE:N/A | ACTION:LOGIN | STATUS:FAILED_INVALID_CREDENTIALS\n" +
		"[2026-08-10 08:01:47] USER:viewonly | ROLE:N/A | ACTION:LOGIN | STATUS:FAILED_INVALID_CREDENTIALS" // no final newline
	if err := os.WriteFile(l.Path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	logN(t, l, 2)
	if r, _ := l.Verify(); !r.OK() || r.Genesis != 3 {
		t.Fatalf("report = %+v", r)
	}
	data, _ := os.ReadFile(l.Path)
	if err := os.WriteFile(l.Path, []byte(strings.Replace(string(data), "viewonly", "vieWonly", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if r, _ := l.Verify(); r.OK() || r.BrokenAt != 3 || !strings.Contains(r.BrokenWhy, "before the chain began") {
		t.Fatalf("editing pre-chain history must be caught at the genesis line: %+v", r)
	}
}

func rewrite(t *testing.T, l *Logger, f func(lines []string) []string) {
	t.Helper()
	data, _ := os.ReadFile(l.Path)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if err := os.WriteFile(l.Path, []byte(strings.Join(f(lines), "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestTamperingIsFoundAtTheRightLine(t *testing.T) {
	cases := map[string]struct {
		edit func([]string) []string
		line int
	}{
		"edit":    {func(l []string) []string { l[3] = strings.Replace(l[3], "n=2", "n=9", 1); return l }, 4},
		"delete":  {func(l []string) []string { return append(l[:3:3], l[4:]...) }, 4},
		"reorder": {func(l []string) []string { l[3], l[4] = l[4], l[3]; return l }, 4},
		"strip":   {func(l []string) []string { l[3], _ = splitChain(l[3]); return l }, 5}, // the next chained line no longer follows
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			l := newLog(t)
			logN(t, l, 5)
			rewrite(t, l, c.edit)
			r, err := l.Verify()
			if err != nil || r.OK() || r.BrokenAt != c.line {
				t.Fatalf("report = %+v err=%v, want a break at line %d", r, err, c.line)
			}
		})
	}
}

func TestTruncatingTheTailIsNotDetectedByTheChainAlone(t *testing.T) {
	// Documented limit: dropping the newest lines leaves a valid (shorter) chain.
	// `wms audit head` compared against an off-host copy is what catches it.
	l := newLog(t)
	logN(t, l, 5)
	before, _ := l.Verify()
	rewrite(t, l, func(lines []string) []string { return lines[:4] })
	after, _ := l.Verify()
	if !after.OK() || after.Head == before.Head {
		t.Fatalf("truncation should verify but move the head: before=%s after=%+v", before.Head, after)
	}
}

func TestForeignLinesAreReportedNotFatal(t *testing.T) {
	l := newLog(t)
	logN(t, l, 2)
	f, _ := os.OpenFile(l.Path, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("[2026-09-20 10:00:00] USER:py | ROLE:x | ACTION:PYTHON_SUITE | STATUS:SUCCESS\n")
	f.Close()
	logN(t, l, 2) // the chain carries on across the foreign line
	r, _ := l.Verify()
	if !r.OK() || len(r.Foreign) != 1 || r.Foreign[0] != 4 {
		t.Fatalf("report = %+v", r)
	}
}

func TestLogInjectionIsNeutralised(t *testing.T) {
	l := newLog(t)
	if err := l.Log("bob\n[2026-01-01 00:00:00] USER:admin | ROLE:Admin | ACTION:LOGIN | STATUS:SUCCESS", "r|x", "A", "S", "d\r\nmore | ACTION:FORGED"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(l.Path)
	if n := strings.Count(string(data), "\n"); n != 2 { // genesis + the one entry
		t.Fatalf("a newline in a field forged an extra line (%d lines):\n%s", n, data)
	}
	if strings.Contains(string(data), "ROLE:r|x") || strings.Count(string(data), "ACTION:FORGED") != 1 {
		t.Errorf("a '|' in a field must not look like a field separator:\n%s", data)
	}
	if r, _ := l.Verify(); !r.OK() {
		t.Fatalf("report = %+v", r)
	}
	long := strings.Repeat("x", 5000)
	_ = l.Log(long, "", "A", "S", long)
	if data, _ := os.ReadFile(l.Path); len(data) > 3000 {
		t.Errorf("over-long fields must be cut, log is %d bytes", len(data))
	}
}

func TestTailHidesTheChainHashes(t *testing.T) {
	l := newLog(t)
	logN(t, l, 2)
	lines, _ := l.Tail(10)
	for _, ln := range lines {
		if strings.Contains(ln, "CHAIN:") {
			t.Errorf("Tail shows a chain hash: %q", ln)
		}
	}
}

// Sessions are separate processes; concurrent writers must not fork the chain.
func TestConcurrentWritersKeepOneChain(t *testing.T) {
	l := newLog(t)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lw := &Logger{Path: l.Path} // its own Logger, as another process would have
			for i := 0; i < 10; i++ {
				if err := lw.Log("u", "r", "A", "S", "x"); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	r, err := l.Verify()
	if err != nil || !r.OK() || r.Chained != 81 {
		t.Fatalf("report = %+v err=%v", r, err)
	}
}

func TestVerifyOnMissingOrEmptyLog(t *testing.T) {
	l := newLog(t)
	if r, err := l.Verify(); err != nil || !r.OK() || r.Lines != 0 || r.Genesis != 0 {
		t.Fatalf("missing log: %+v %v", r, err)
	}
	os.WriteFile(l.Path, nil, 0600)
	if r, err := l.Verify(); err != nil || !r.OK() || r.Lines != 0 {
		t.Fatalf("empty log: %+v %v", r, err)
	}
}
