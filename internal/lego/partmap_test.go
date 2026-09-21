package lego

import "testing"

func TestIPNNameAndSpec(t *testing.T) {
	cases := []struct {
		id        int
		name, ipn string
	}{
		{4, "Red", "3001-4"},
		{0, "Black", "3001-0"}, // Rebrickable colour 0 is Black, a real colour
		{NoColor, "Glow in Dark!", "3001-x-glow-in-dark"},
		{NoColor, "", "3001"},
	}
	for _, c := range cases {
		if got := IPNFor("3001", c.id, c.name); got != c.ipn {
			t.Errorf("IPNFor(%d, %q) = %q, want %q", c.id, c.name, got, c.ipn)
		}
	}
	if PartNameFor("Brick 2 x 4", "Red") != "Brick 2 x 4 - Red" || PartNameFor("Brick 2 x 4", "") != "Brick 2 x 4" {
		t.Error("part name should be 'Name - Colour', or just the name with no colour")
	}
	if p := CategoryPath("Bricks"); len(p) != 2 || p[0] != "Lego" || p[1] != "Bricks" {
		t.Errorf("category path = %v", p)
	}
	if p := CategoryPath("  "); len(p) != 1 || p[0] != "Lego" {
		t.Errorf("unknown category should file under Lego alone, got %v", p)
	}

	spec := SpecFor(OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", ColorID: 4, ColorName: "Red"}, 12)
	if spec.IPN != "3001-4" || spec.Name != "Brick 2 x 4 - Red" || spec.CategoryID != 12 || spec.Tags != "lego,Red" || spec.MfgPN != "" {
		t.Errorf("spec = %+v (manufacturer number must stay empty so ModernWMS's item code follows the IPN)", spec)
	}
}
