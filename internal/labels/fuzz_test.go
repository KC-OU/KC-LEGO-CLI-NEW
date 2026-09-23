package labels

import (
	"strings"
	"testing"
)

// FuzzPDFEscape checks that whatever text a label carries (a part name, a
// set title — arbitrary user/catalog data), pdfEscape always produces
// something safe to embed literally inside a PDF string literal: printable
// ASCII only, with '(' ')' '\' escaped so they can never terminate the
// string early or desync the reader.
func FuzzPDFEscape(f *testing.F) {
	for _, s := range []string{"Millennium Falcon", "3001 (Red)", `C:\path\brick`, "—em dash—", "üñïçødé", "", "\x00\x01control", strings.Repeat("(", 50)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := pdfEscape(in)
		for i := 0; i < len(out); i++ {
			c := out[i]
			if c < 32 || c >= 127 {
				t.Fatalf("non-printable-ASCII byte %d in escaped output %q from %q", c, out, in)
			}
			if (c == '(' || c == ')') && (i == 0 || out[i-1] != '\\') {
				t.Fatalf("unescaped %q in output %q from %q", c, out, in)
			}
		}
	})
}
