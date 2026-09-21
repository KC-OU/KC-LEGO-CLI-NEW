// Package audit writes the same pipe-delimited audit trail the Python suite
// wrote, to the same default path, so existing log-tailing habits/tooling
// keep working.
//
// Every line this package writes ends in ` | CHAIN:<sha256>`, where the hash
// covers the line and the previous line's hash. Editing, deleting or
// reordering an earlier line breaks every hash after it, which
// `wms audit verify` finds. The first chained line (CHAIN_START) also pins a
// hash of everything already in the file, so the history from before the
// chain existed is covered too.
//
// The limit is honest: the gateway runs as root, so an attacker who is root can
// rewrite the file AND recompute the chain. The chain catches accidents, casual
// edits and out-of-band changes; for the rest, keep a copy of `wms audit head`
// somewhere the box cannot write (the alerts in `NOTIFY_URL` send it).
package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

type Logger struct {
	Path string
}

func New() *Logger {
	return &Logger{Path: config.Get(config.AuditLogFile)}
}

const (
	chainMark     = " | CHAIN:"
	genesisAction = "CHAIN_START"
	tailWindow    = 8192
)

// Sanitize makes a value safe to put on one log line: no newline can forge a
// second entry, and no "|" can fake another field. Control characters become
// "?", "|" becomes "/", and an over-long value is cut.
func Sanitize(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n == max {
			b.WriteString("…")
			break
		}
		switch {
		case r < 0x20 || r == 0x7f:
			r = '?'
		case r == '|':
			r = '/'
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func lineBody(ts time.Time, user, role, action, status, details string) string {
	return fmt.Sprintf("[%s] USER:%s | ROLE:%s | ACTION:%s | STATUS:%s | DETAILS:%s",
		ts.Format("2006-01-02 15:04:05"), Sanitize(user, 64), Sanitize(role, 32), Sanitize(action, 48), Sanitize(status, 32), Sanitize(details, 512))
}

func chainHash(prev, body string) string {
	sum := sha256.Sum256([]byte(prev + "\n" + body))
	return hex.EncodeToString(sum[:])
}

// splitChain separates a line's body from its trailing chain hash ("" if none).
func splitChain(line string) (body, hash string) {
	if i := strings.LastIndex(line, chainMark); i >= 0 {
		h := line[i+len(chainMark):]
		if len(h) == 64 && isHex(h) {
			return line[:i], h
		}
	}
	return line, ""
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

// Log appends one chained line. Concurrent writers (every telnet and web
// session is its own process) are serialised with flock so the chain never forks.
func (l *Logger) Log(user, role, action, status, details string) error {
	f, err := os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	prev, err := lastChain(f)
	if err != nil {
		return err
	}
	now := time.Now()
	var out strings.Builder
	if prev == "" { // first chained line ever: pin what is already in the file
		prefix, err := io.ReadAll(io.NewSectionReader(f, 0, mustSize(f)))
		if err != nil {
			return err
		}
		if len(prefix) > 0 && prefix[len(prefix)-1] != '\n' { // never glue the genesis onto an unterminated line
			prefix = append(prefix, '\n')
			out.WriteString("\n")
		}
		sum := sha256.Sum256(prefix)
		body := lineBody(now, "system", "", genesisAction, "INFO",
			fmt.Sprintf("chain begins; %d earlier line(s) sha256=%s", bytes.Count(prefix, []byte("\n")), hex.EncodeToString(sum[:])))
		prev = chainHash("", body)
		out.WriteString(body + chainMark + prev + "\n")
	}
	body := lineBody(now, user, role, action, status, details)
	out.WriteString(body + chainMark + chainHash(prev, body) + "\n")
	_, err = f.WriteString(out.String())
	return err
}

func mustSize(f *os.File) int64 {
	fi, err := f.Stat()
	if err != nil {
		return 0
	}
	return fi.Size()
}

// lastChain returns the hash on the newest chained line, or "" when the file
// has none. It reads a window from the end and widens it only if that window
// holds no chained line (a long run of lines from another writer).
func lastChain(f *os.File) (string, error) {
	size := mustSize(f)
	for window := int64(tailWindow); ; window *= 4 {
		start := max(size-window, 0)
		buf := make([]byte, size-start)
		if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
			return "", err
		}
		lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			if i == 0 && start > 0 {
				break // a partial first line
			}
			if _, h := splitChain(lines[i]); h != "" {
				return h, nil
			}
		}
		if start == 0 {
			return "", nil
		}
	}
}

// Tail returns the last n lines of the log, oldest first, without the chain
// hashes (they are for `wms audit verify`, not for reading). Reads the whole
// file — this is an operator-facing log viewer, not a hot path, so a
// reverse-seek reader would be premature.
func (l *Logger) Tail(n int) ([]string, error) {
	data, err := os.ReadFile(l.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i], _ = splitChain(ln)
	}
	return out, nil
}

// Report is the result of checking the chain.
type Report struct {
	Lines     int    // total lines
	Chained   int    // lines carrying a chain hash
	Foreign   []int  // 1-based numbers of lines after the chain began that carry no hash (another writer)
	Genesis   int    // line number of CHAIN_START, 0 if the chain never began
	Head      string // hash of the newest chained line
	BrokenAt  int    // 1-based line where verification failed, 0 if it did not
	BrokenWhy string
}

// OK is true when nothing failed (a log that never began a chain is OK but unprotected).
func (r *Report) OK() bool { return r.BrokenAt == 0 }

// Verify recomputes the chain from the CHAIN_START line to the end.
func (l *Logger) Verify() (*Report, error) {
	data, err := os.ReadFile(l.Path)
	if os.IsNotExist(err) {
		return &Report{}, nil
	}
	if err != nil {
		return nil, err
	}
	text := string(data)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	r := &Report{Lines: len(lines)}

	prev, offset := "", 0
	for i, ln := range lines {
		no := i + 1
		body, h := splitChain(ln)
		if h == "" {
			if r.Genesis != 0 {
				r.Foreign = append(r.Foreign, no)
			}
			offset += len(ln) + 1
			continue
		}
		if r.Genesis == 0 { // the first chained line must be the genesis, and pins the prefix
			r.Genesis = no
			if !strings.Contains(body, "ACTION:"+genesisAction) {
				return r.broke(no, "the first chained line is not a CHAIN_START line"), nil
			}
			sum := sha256.Sum256([]byte(text[:offset]))
			if !strings.Contains(body, "sha256="+hex.EncodeToString(sum[:])) {
				return r.broke(no, "the lines before the chain began were changed (CHAIN_START no longer matches them)"), nil
			}
			prev = ""
		}
		want := chainHash(prev, body)
		if h != want {
			return r.broke(no, "hash mismatch: this line, or an earlier one, was edited, removed or reordered"), nil
		}
		prev = h
		r.Chained++
		r.Head = h
		offset += len(ln) + 1
	}
	return r, nil
}

func (r *Report) broke(line int, why string) *Report {
	r.BrokenAt, r.BrokenWhy = line, why
	return r
}
