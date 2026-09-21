package wmsdb

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateASNNo(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 30, 0, 0, time.UTC)
	n1 := generateASNNo(now)
	n2 := generateASNNo(now)

	if !strings.HasPrefix(n1, "ASN20260819153000") {
		t.Fatalf("unexpected prefix: %s", n1)
	}
	if len(n1) != len("ASN20260819153000")+4 {
		t.Fatalf("unexpected length: %s (%d)", n1, len(n1))
	}
	if n1 == n2 {
		t.Errorf("expected distinct suffixes for same-second calls, got %s twice", n1)
	}
}

func TestPyStringLitEscaping(t *testing.T) {
	cases := []string{
		`plain`,
		`has "double quotes"`,
		`has 'single quotes'`,
		`back\slash`,
		"unicode: café ☃",
		"newline\nand\ttab",
	}
	for _, c := range cases {
		lit := pyStringLit(c)
		if !strings.HasPrefix(lit, `"`) || !strings.HasSuffix(lit, `"`) {
			t.Errorf("pyStringLit(%q) = %q, expected double-quoted", c, lit)
		}
	}
}
