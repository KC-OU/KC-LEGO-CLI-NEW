package api

import "testing"

func TestPartLink(t *testing.T) {
	for _, c := range []struct {
		base string
		id   int
		want string
	}{{"", 5, ""}, {"https://partdb.example.com/", 5, "https://partdb.example.com/part/5"}, {"https://partdb.example.com", 5, "https://partdb.example.com/part/5"}, {"https://x.example/en/", 9, "https://x.example/en/part/9"}} {
		if got := partLink(c.base, c.id); got != c.want {
			t.Errorf("partLink(%q, %d) = %q, want %q", c.base, c.id, got, c.want)
		}
	}
}
