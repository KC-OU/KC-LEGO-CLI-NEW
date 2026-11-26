package lego

import "testing"

func TestSortSetHits(t *testing.T) {
	mk := func() []SetHit {
		return []SetHit{{Num: "40622-1", Year: 2022}, {Num: "3866-1", Year: 2008}, {Num: "75344-1", Year: 2022}}
	}
	order := func(h []SetHit) string {
		s := ""
		for _, x := range h {
			s += x.Num + " "
		}
		return s
	}
	for mode, want := range map[string]string{
		"year":     "40622-1 75344-1 3866-1 ", // ties keep their order
		"year_asc": "3866-1 40622-1 75344-1 ",
		"num":      "3866-1 40622-1 75344-1 ", // numeric, not text
		"num_desc": "75344-1 40622-1 3866-1 ",
		"":         "40622-1 3866-1 75344-1 ",
	} {
		h := mk()
		SortSetHits(h, mode)
		if got := order(h); got != want {
			t.Errorf("%q: got %s want %s", mode, got, want)
		}
	}
}
