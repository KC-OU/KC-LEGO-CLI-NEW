// Package gateway ports the original suite's Telnet daemon (RFC 854, for
// barcode scanners and PuTTY-style clients) and its web-terminal front end
// (the already-installed ttyd binary, wrapped by a Go reverse proxy) into
// native Go, both dropping straight into the compiled wms binary's `tui`
// subcommand as the session's child process.
package gateway

const (
	iacByte  = 255
	dontByte = 254
	doByte   = 253
	wontByte = 252
	willByte = 251
	sbByte   = 250
	seByte   = 240
)

type iacMode int

const (
	modeNormal iacMode = iota
	modeGotIAC
	modeGotCommand
	modeInSubneg
	modeInSubnegGotIAC
)

const (
	nawsOption = 31 // RFC 1073 Negotiate About Window Size
	// A NAWS payload is option + 4 size bytes; anything much longer is not
	// one, so a hostile client can't make the parser buffer without bound.
	maxSubnegPayload = 16
)

// IACState carries the parser's position across chunked reads — a real
// socket can split any multi-byte IAC sequence across two Read calls — and
// the most recent window size the client reported via NAWS.
type IACState struct {
	mode       iacMode
	sb         []byte // payload of the subnegotiation in progress, option byte first
	cols, rows uint16
	resized    bool
}

// TakeResize returns the window size the client most recently reported via
// NAWS, once: it reports false until the client sends a new (or its first)
// size, so callers resize the PTY exactly when the window actually changed.
func (st *IACState) TakeResize() (cols, rows uint16, ok bool) {
	if !st.resized {
		return 0, 0, false
	}
	st.resized = false
	return st.cols, st.rows, true
}

func (st *IACState) endSubneg() {
	sb := st.sb
	st.sb = st.sb[:0]
	if len(sb) != 5 || sb[0] != nawsOption {
		return
	}
	cols := uint16(sb[1])<<8 | uint16(sb[2])
	rows := uint16(sb[3])<<8 | uint16(sb[4])
	// 0 means "unknown" in NAWS; keep the previous size rather than resize to nothing.
	if cols > 0 && rows > 0 {
		st.cols, st.rows, st.resized = cols, rows, true
	}
}

// FilterIAC strips Telnet IAC negotiation and subnegotiation bytes from
// data, passing everything else through untouched, mirroring
// telnet_server.py's inline filter: 3-byte IAC DO/DONT/WILL/WONT <opt>
// sequences are dropped with no reply sent, IAC SB ... IAC SE
// subnegotiation payload is removed from the output (a NAWS window size in
// it is recorded on st — see TakeResize — so the PTY can follow the client's
// real terminal size), and IAC IAC unescapes to one literal 0xFF byte.
func FilterIAC(data []byte, st *IACState) []byte {
	out := make([]byte, 0, len(data))
	for _, b := range data {
		switch st.mode {
		case modeNormal:
			if b == iacByte {
				st.mode = modeGotIAC
			} else {
				out = append(out, b)
			}
		case modeGotIAC:
			switch b {
			case iacByte:
				out = append(out, 0xFF)
				st.mode = modeNormal
			case doByte, dontByte, willByte, wontByte:
				st.mode = modeGotCommand
			case sbByte:
				st.sb = st.sb[:0]
				st.mode = modeInSubneg
			default:
				st.mode = modeNormal
			}
		case modeGotCommand:
			st.mode = modeNormal
		case modeInSubneg:
			if b == iacByte {
				st.mode = modeInSubnegGotIAC
			} else if len(st.sb) < maxSubnegPayload {
				st.sb = append(st.sb, b)
			}
		case modeInSubnegGotIAC:
			switch b {
			case seByte:
				st.endSubneg()
				st.mode = modeNormal
			case iacByte: // escaped 0xFF data byte inside the subnegotiation (e.g. a width of 255)
				if len(st.sb) < maxSubnegPayload {
					st.sb = append(st.sb, 0xFF)
				}
				st.mode = modeInSubneg
			default:
				st.mode = modeInSubneg
			}
		}
	}
	return out
}

// negotiationPreamble is what the original sends proactively on connect,
// fire-and-forget: IAC WILL ECHO, IAC WILL SGA, IAC WILL BINARY,
// IAC DO BINARY, IAC DO NAWS (the client's reply to that last one is what
// carries its window size, and again whenever the window is resized).
var negotiationPreamble = []byte{
	iacByte, willByte, 1, // ECHO
	iacByte, willByte, 3, // SGA
	iacByte, willByte, 0, // BINARY
	iacByte, doByte, 0, // BINARY
	iacByte, doByte, 31, // NAWS
}
