package gateway

import (
	"net"
	"unicode/utf8"
)

// Handheld terminals (Symbol/Motorola MC9090 and similar Windows CE telnet clients) show
// UTF-8 as Latin-1, so the TUI's box-drawing lines and bullets arrive as "â" garbage.
// asciiConn wraps the connection for sessions on a handheld port and rewrites every
// non-ASCII rune to a single ASCII character, so columns stay aligned.
type asciiConn struct {
	net.Conn
	tail []byte // an incomplete UTF-8 sequence carried over from the previous Write
}

// Write rewrites p and returns len(p) even though fewer/more bytes are sent, as net.Conn
// callers expect. Only the session's single pump goroutine writes, so no locking.
func (c *asciiConn) Write(p []byte) (int, error) {
	in := append(c.tail, p...)
	c.tail = nil
	out := make([]byte, 0, len(in))
	for len(in) > 0 {
		if in[0] < utf8.RuneSelf {
			out = append(out, in[0])
			in = in[1:]
			continue
		}
		if !utf8.FullRune(in) {
			c.tail = append([]byte(nil), in...) // wait for the rest of the sequence
			break
		}
		r, n := utf8.DecodeRune(in)
		in = in[n:]
		out = append(out, asciiFor(r))
	}
	if len(out) > 0 {
		if _, err := c.Conn.Write(out); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// asciiFor maps one rune to one ASCII byte ('?' when there is no sensible match).
func asciiFor(r rune) byte {
	switch r {
	case '•', '◦', '▪', '▫', '●', '■', '□', '◆', '★':
		return '*'
	case '○':
		return 'o'
	case '·':
		return '.'
	case '–', '—', '―', '−', '─', '━', '╌', '╍', '┄', '┅', '┈', '┉', '╴', '╶':
		return '-'
	case '═':
		return '='
	case '│', '┃', '║', '╎', '╏', '┆', '┇', '┊', '┋', '╵', '╷':
		return '|'
	case '‘', '’':
		return '\''
	case '“', '”':
		return '"'
	case '…':
		return '.'
	case '→', '➔', '►', '▶', '›', '»':
		return '>'
	case '←', '◄', '◀', '‹', '«':
		return '<'
	case '↑', '▲':
		return '^'
	case '↓', '▼':
		return 'v'
	case '✓', '✔':
		return '+'
	case '✗', '✘', '×':
		return 'x'
	case '°':
		return 'o'
	case '±':
		return '+'
	case '╱':
		return '/'
	case '╲':
		return '\\'
	case '╳':
		return 'X'
	case '░':
		return '.'
	case '▒':
		return ':'
	case 0xFFFD:
		return '?'
	}
	switch {
	case r >= 0x2500 && r <= 0x257F: // remaining box drawing: corners, tees, crosses
		return '+'
	case r >= 0x2580 && r <= 0x259F: // block elements
		return '#'
	}
	return '?'
}
