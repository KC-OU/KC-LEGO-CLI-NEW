package sync

import (
	"regexp"
	"strings"
	"testing"
)

func TestAggregateStockNeverNil(t *testing.T) {
	got := AggregateStock(nil, nil, nil)
	if got == nil {
		t.Fatal("AggregateStock must never return nil (breaks JSON->Python `.items()`)")
	}
}

// TestBuildApplyScriptNoNullLiterals guards against the exact production bug
// this test file was written for: a nil Go slice/map in syncPayload
// serializes as JSON `null`, and the generated script does an unconditional
// `for x in data["field"]` / `.items()` over every one of these fields, which
// crashes on `null` instead of iterating zero times.
func TestBuildApplyScriptNoNullLiterals(t *testing.T) {
	script, err := buildApplyScript("/app/wms.db", syncPayload{Now: "2026-01-01 00:00:00.000000"})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`\"categories\":null`, `\"category_soft_delete\":null`, `\"spus\":null`, `\"spu_soft_delete\":null`, `\"stock\":null`} {
		if strings.Contains(script, field) {
			t.Fatalf("generated script embeds a null payload field: %s", field)
		}
	}
}

func TestTheViewOnlyAccountIsOnlySeededFromSettingsNeverFromACompiledInDefault(t *testing.T) {
	script, err := buildApplyScript("/app/wms.db", syncPayload{Now: "2026-01-01 00:00:00.000000"})
	if err != nil {
		t.Fatal(err)
	}
	// No compiled-in credential-shaped value: a 32-digit hex hash or an e-mail address.
	for _, leak := range []*regexp.Regexp{regexp.MustCompile(`[0-9a-f]{32}`), regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.]+`)} {
		if m := leak.FindString(script); m != "" {
			t.Errorf("the script must not carry a built-in value, found %q", m)
		}
	}
	if !strings.Contains(script, `if data.get("viewonly_auth"):`) {
		t.Error("the viewonly INSERT must be guarded: with no hash configured, nothing is created")
	}
	withHash, _ := buildApplyScript("/app/wms.db", syncPayload{Now: "x", ViewOnlyAuth: "abc123", ViewOnlyEmail: "v@example.com"})
	if !strings.Contains(withHash, `\"viewonly_auth\":\"abc123\"`) {
		t.Errorf("a configured hash travels in the JSON payload, not in the SQL text")
	}
}
