package gateway

import "testing"

// FuzzFilterIAC checks the Telnet IAC filter never panics or hangs on a
// hostile stream, and that whatever it does emit is smaller than the input
// once IAC-escaping is accounted for (it strips negotiation, never adds).
func FuzzFilterIAC(f *testing.F) {
	f.Add([]byte("hello\r\n"))
	f.Add([]byte{iacByte, doByte, nawsOption})
	f.Add([]byte{iacByte, sbByte, nawsOption, 0, 80, 0, 24, iacByte, seByte})
	f.Add([]byte{iacByte, iacByte}) // escaped literal 0xFF
	f.Add([]byte{iacByte})          // truncated mid-sequence
	f.Add([]byte{iacByte, sbByte})  // truncated mid-subnegotiation
	f.Fuzz(func(t *testing.T, data []byte) {
		st := &IACState{}
		out := FilterIAC(data, st)
		if len(out) > len(data) {
			t.Fatalf("output %d bytes longer than input %d bytes", len(out), len(data))
		}
		// split across two calls: chunking must never change what comes out
		st2 := &IACState{}
		mid := len(data) / 2
		got := append(FilterIAC(data[:mid], st2), FilterIAC(data[mid:], st2)...)
		if string(got) != string(out) {
			t.Fatalf("chunked filtering differs: whole=%q chunked=%q", out, got)
		}
	})
}
