package main

import "testing"

func TestGraceOrigin(t *testing.T) {
	cases := []struct {
		name    string
		gateway bool
		remote  string
		want    string
	}{
		{"started from a shell on this box", false, "", "local"},
		{"telnet client with a known address", true, "10.1.1.5", "telnet:10.1.1.5"},
		{"web terminal: ttyd hides the address, so it is never trusted", true, "", ""},
		// Setting WMS_REMOTE_ADDR by hand in a shell must not fake a remote origin.
		{"env var ignored outside a gateway session", false, "10.1.1.5", "local"},
	}
	for _, c := range cases {
		if got := graceOrigin(c.gateway, c.remote); got != c.want {
			t.Errorf("%s: graceOrigin(%v, %q) = %q, want %q", c.name, c.gateway, c.remote, got, c.want)
		}
	}
}
