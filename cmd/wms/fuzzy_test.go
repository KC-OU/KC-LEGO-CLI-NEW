package main

import (
	"strings"
	"testing"
)

func TestFuzzyGroupSuggestsOnATypoAtAnyDepth(t *testing.T) {
	cases := []struct {
		args []string
		want string // a substring expected in the suggestion
	}{
		{[]string{"lego", "repot"}, "report"},
		{[]string{"lego", "report", "mising"}, "missing"},
	}
	for _, c := range cases {
		stdout, code := run(t, c.args...)
		if code == 0 {
			t.Errorf("%v: expected a non-zero exit for an unknown subcommand", c.args)
		}
		if !strings.Contains(stdout, "unknown command") || !strings.Contains(stdout, "Did you mean") || !strings.Contains(stdout, c.want) {
			t.Errorf("%v: stdout = %q, want an unknown-command error suggesting %q", c.args, stdout, c.want)
		}
	}
}

func TestFuzzyGroupStillShowsHelpWithNoArgs(t *testing.T) {
	stdout, code := run(t, "lego", "report")
	if code != 0 || !strings.Contains(stdout, "Available Commands") {
		t.Fatalf("code=%d out=%q", code, stdout)
	}
}
