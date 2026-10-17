package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestEveryThemeStylesInfoDistinctly guards against the easy mistake of
// adding a new Theme field (here: Info, the blue "extra" counterpart to
// Danger's red "missing") to one constructor and forgetting another --
// every named theme must actually set a foreground or background colour on
// Info, not leave it as an unstyled zero-value lipgloss.Style. Forces a
// truecolor profile first: lipgloss otherwise auto-detects a non-TTY test
// run and silently strips all colour, which would make every theme look
// "unstyled" whether or not the code actually set one.
func TestEveryThemeStylesInfoDistinctly(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	noColor := lipgloss.NoColor{}
	for _, name := range Themes {
		th := ByName(name)
		if th.Info.GetForeground() == noColor && th.Info.GetBackground() == noColor {
			t.Errorf("theme %q: Info has no foreground or background colour set", name)
		}
	}
}

// TestMonoThemeStylesInfoDistinctly is monoTheme's own check: NO_COLOR means
// no colour at all, so Info is distinguished by emphasis (bold/underline/
// reverse) instead -- it must still differ from plain, unstyled text.
func TestMonoThemeStylesInfoDistinctly(t *testing.T) {
	info := monoTheme().Info
	if !info.GetBold() && !info.GetUnderline() && !info.GetReverse() && !info.GetItalic() {
		t.Error("mono theme: Info has no emphasis at all -- indistinguishable from plain text")
	}
}
