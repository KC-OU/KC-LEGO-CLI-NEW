package sync

import "testing"

func TestDeriveSPUCode(t *testing.T) {
	cases := []struct {
		mfgPN, ipn, name string
		id               int
		want             string
	}{
		{"MFG123", "IPN1", "Widget", 1, "MFG123"},
		{"", "IPN1", "Widget", 1, "IPN1"},
		{"", "", "Widget", 1, "Widget"},
		{"", "", "", 42, "PART-42"},
		{"  ", "  ", "  ", 7, "PART-7"},
	}
	for _, c := range cases {
		got := DeriveSPUCode(c.mfgPN, c.ipn, c.name, c.id)
		if got != c.want {
			t.Errorf("DeriveSPUCode(%q,%q,%q,%d) = %q, want %q", c.mfgPN, c.ipn, c.name, c.id, got, c.want)
		}
	}
}

func TestDeriveSPUName(t *testing.T) {
	if got := DeriveSPUName("Widget", "A small widget"); got != "Widget" {
		t.Errorf("got %q, want Widget", got)
	}
	if got := DeriveSPUName("", "A small widget"); got != "A small widget" {
		t.Errorf("got %q, want fallback description", got)
	}
}

func TestDeriveSPUDescription(t *testing.T) {
	if got := DeriveSPUDescription("Widget", "A small widget"); got != "Specification Code: Widget | A small widget" {
		t.Errorf("got %q", got)
	}
	if got := DeriveSPUDescription("Widget", "Widget"); got != "Widget" {
		t.Errorf("identical spec/description should collapse, got %q", got)
	}
	if got := DeriveSPUDescription("", "Widget"); got != "Widget" {
		t.Errorf("got %q", got)
	}
}

func TestDeriveGTIN(t *testing.T) {
	if got := DeriveGTIN("012345", "MFG1", "SPU1"); got != "012345" {
		t.Errorf("got %q", got)
	}
	if got := DeriveGTIN("", "MFG1", "SPU1"); got != "MFG1" {
		t.Errorf("got %q", got)
	}
	if got := DeriveGTIN("", "", "SPU1"); got != "SPU1" {
		t.Errorf("got %q", got)
	}
}

func TestAggregateStock(t *testing.T) {
	active := []int{1, 2, 3}
	lotPartIDs := []int{1, 1, 2, 99}
	lotAmounts := []float64{5, 3, 10, 1000}

	got := AggregateStock(active, lotPartIDs, lotAmounts)
	want := map[int]int{1: 8, 2: 10, 3: 0}
	for id, qty := range want {
		if got[id] != qty {
			t.Errorf("part %d: got %d, want %d", id, got[id], qty)
		}
	}
	if _, ok := got[99]; ok {
		t.Error("inactive part 99 must not appear in aggregated stock")
	}
}

func TestSoftDeleteTargets(t *testing.T) {
	existing := []int{1, 2, 3, 4}
	active := []int{2, 4}
	got := SoftDeleteTargets(existing, active)

	want := map[int]bool{1: true, 3: true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want ids %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected soft-delete target %d", id)
		}
	}
}

// TestSoftDeleteTargetsNeverNil guards a real production bug: a nil []int
// here json.Marshal's to `null`, and the generated apply script does an
// unconditional `for id in data["spu_soft_delete"]`, which crashes on `null`
// instead of iterating zero times — the common case when nothing needs
// soft-deleting on a given sync run.
func TestSoftDeleteTargetsNeverNil(t *testing.T) {
	if got := SoftDeleteTargets([]int{1, 2}, []int{1, 2}); got == nil {
		t.Fatal("SoftDeleteTargets must never return nil")
	}
	if got := SoftDeleteTargets(nil, nil); got == nil {
		t.Fatal("SoftDeleteTargets must never return nil")
	}
}
