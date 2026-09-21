// Package ui implements the IBM i / 5250 green-screen rendering primitives:
// a fixed-row header band, reverse-video titles and column headings, plain
// (non-boxed) columnar data display, a message line, and a function-key
// legend. Base-16 ANSI colors only — no 256-color/truecolor gradients — is
// what makes this read as a real terminal rather than a green-tinted modern
// UI. Every function here returns a string; nothing prints directly, so the
// same primitives back both one-shot CLI command output and, later, a
// bubbletea TUI.
package ui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

const (
	IconOK   = "✓"
	IconFail = "✗"
	IconWarn = "⚠"
	IconKey  = "•"
	Brand    = "■"

	Width = 80
)

type Theme struct {
	Name    string
	Labels  bool // spell out OK / FAIL / WARN beside the icons, so meaning never rests on colour or a glyph alone
	Mono    bool // no colour at all (NO_COLOR): emphasis by bold/underline/reverse only, and no colour swatches
	Classic bool // old Python-TUI layout (default); MODERNWMS_TUI_CLASSIC=0 selects the 5250 frame — see App.View in internal/uiapp
	Width   int  // real terminal width from tea.WindowSizeMsg; 0 means the fixed 80-column default — use W()

	Brand         lipgloss.Style
	Text          lipgloss.Style
	Muted         lipgloss.Style
	Success       lipgloss.Style
	Warning       lipgloss.Style
	Danger        lipgloss.Style
	Accent        lipgloss.Style // bright input-capable field text
	Protected     lipgloss.Style // dim display-only field text
	Strong        lipgloss.Style // bold white without Accent's underline — the Python TUI's "accent" (menu keys, list markers)
	TitleReverse  lipgloss.Style // black-on-bright, panel titles
	HeaderReverse lipgloss.Style // black-on-bright, column headings
	KeyLegend     lipgloss.Style // reverse-video Fn labels in the footer
	BadgeAdmin    lipgloss.Style
	BadgeOperator lipgloss.Style
	BadgeView     lipgloss.Style
}

// Themes lists the names MODERNWMS_TUI_THEME accepts, default first.
var Themes = []string{"green", "amber", "high-contrast", "colorblind"}

// New selects the color theme from MODERNWMS_TUI_THEME (green by default; amber,
// high-contrast and colorblind are the alternatives; Admin > Settings can save
// the choice) and the layout from MODERNWMS_TUI_CLASSIC — the old Python TUI's
// full-width flow layout is the default, "0" falls back to the fixed 5250
// frame. The two axes are independent. A NO_COLOR environment variable
// (no-color.org) wins over any theme and turns colour off entirely.
func New() Theme {
	t := ByName(config.Get(config.TUITheme))
	if os.Getenv("NO_COLOR") != "" {
		t = monoTheme()
	}
	t.Classic = os.Getenv("MODERNWMS_TUI_CLASSIC") != "0"
	return t
}

// ByName returns the named theme; an unknown or empty name is the default green.
func ByName(name string) Theme {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "amber":
		return amberTheme()
	case "high-contrast", "highcontrast", "contrast":
		return highContrastTheme()
	case "colorblind", "colourblind", "cvd":
		return colorblindTheme()
	}
	return greenTheme()
}

// W is the width every full-width element (rules, header band, panels)
// should span: the real terminal width once the TUI has received one, else
// the fixed 80 columns (a telnet client that never reports its window size,
// and tests).
func (t Theme) W() int {
	if t.Width <= 0 {
		return Width
	}
	return t.Width
}

// WithWidth is the theme for a screen of a known width, which is what makes tables narrow their
// columns to fit it; command-line output (width unknown) is never cut.
func (t Theme) WithWidth(w int) Theme {
	t.Width = w
	return t
}

// highContrastTheme is for low vision and bright rooms: nothing is dimmed, text is
// plain white on black, and warnings and errors use reverse video, so they
// stand out by more than hue.
func highContrastTheme() Theme {
	white, gray, black := lipgloss.Color("15"), lipgloss.Color("7"), lipgloss.Color("0")
	yellow, red := lipgloss.Color("11"), lipgloss.Color("9")
	rev := lipgloss.NewStyle().Bold(true).Foreground(black).Background(white)
	return Theme{
		Name: "high-contrast", Labels: true,
		Brand:         lipgloss.NewStyle().Bold(true).Foreground(yellow),
		Text:          lipgloss.NewStyle().Foreground(white),
		Muted:         lipgloss.NewStyle().Foreground(gray),
		Success:       lipgloss.NewStyle().Bold(true).Foreground(white),
		Warning:       lipgloss.NewStyle().Bold(true).Foreground(black).Background(yellow),
		Danger:        lipgloss.NewStyle().Bold(true).Foreground(white).Background(red),
		Accent:        lipgloss.NewStyle().Bold(true).Underline(true).Foreground(white),
		Protected:     lipgloss.NewStyle().Foreground(gray),
		Strong:        lipgloss.NewStyle().Bold(true).Foreground(white),
		TitleReverse:  rev,
		HeaderReverse: rev,
		KeyLegend:     rev,
		BadgeAdmin:    rev,
		BadgeOperator: rev,
		BadgeView:     lipgloss.NewStyle().Bold(true).Foreground(white).Background(red),
	}
}

// colorblindTheme avoids the red/green pair that red-green colour blindness
// merges: blue means fine and orange means a problem (both stay distinct for
// deuteranopia, protanopia and tritanopia-adjacent viewers), and the OK / FAIL
// labels back the colours up. Orange and blue need 256 colours.
func colorblindTheme() Theme {
	blue, lightBlue := lipgloss.Color("33"), lipgloss.Color("81")
	orange, yellow := lipgloss.Color("208"), lipgloss.Color("227")
	white, black := lipgloss.Color("15"), lipgloss.Color("0")
	t := buildTheme("colorblind", "33", "81")
	t.Labels = true
	t.Success = lipgloss.NewStyle().Bold(true).Foreground(lightBlue)
	t.Warning = lipgloss.NewStyle().Bold(true).Foreground(yellow)
	t.Danger = lipgloss.NewStyle().Bold(true).Foreground(orange)
	t.Text = lipgloss.NewStyle().Foreground(blue)
	t.BadgeView = lipgloss.NewStyle().Bold(true).Foreground(black).Background(orange)
	t.BadgeAdmin = lipgloss.NewStyle().Bold(true).Foreground(black).Background(white)
	return t
}

// monoTheme is what NO_COLOR gets: no colour codes at all, emphasis by bold,
// underline and reverse video.
func monoTheme() Theme {
	plain := lipgloss.NewStyle()
	bold := plain.Bold(true)
	rev := bold.Reverse(true)
	return Theme{
		Name: "mono", Labels: true, Mono: true,
		Brand: bold, Text: plain, Muted: plain, Success: bold, Warning: bold.Underline(true), Danger: rev,
		Accent: bold.Underline(true), Protected: plain, Strong: bold,
		TitleReverse: rev, HeaderReverse: rev, KeyLegend: rev,
		BadgeAdmin: rev, BadgeOperator: rev, BadgeView: bold.Underline(true),
	}
}

func greenTheme() Theme { return buildTheme("green", "2", "10") }
func amberTheme() Theme { return buildTheme("amber", "3", "11") }

// buildTheme takes the base-16 ANSI color (e.g. "2" green) and its bright
// variant (e.g. "10" bright green); danger/warning stay fixed regardless of
// theme, matching real 5250 error-line conventions.
func buildTheme(name, base, bright string) Theme {
	baseColor := lipgloss.Color(base)
	brightColor := lipgloss.Color(bright)
	black := lipgloss.Color("0")
	white := lipgloss.Color("15")
	red := lipgloss.Color("1")
	yellow := lipgloss.Color("11")

	return Theme{
		Name:          name,
		Brand:         lipgloss.NewStyle().Bold(true).Foreground(brightColor),
		Text:          lipgloss.NewStyle().Foreground(baseColor),
		Muted:         lipgloss.NewStyle().Faint(true).Foreground(baseColor),
		Success:       lipgloss.NewStyle().Bold(true).Foreground(baseColor),
		Warning:       lipgloss.NewStyle().Bold(true).Foreground(yellow),
		Danger:        lipgloss.NewStyle().Bold(true).Foreground(red),
		Accent:        lipgloss.NewStyle().Bold(true).Underline(true).Foreground(white),
		Protected:     lipgloss.NewStyle().Faint(true).Foreground(baseColor),
		Strong:        lipgloss.NewStyle().Bold(true).Foreground(white),
		TitleReverse:  lipgloss.NewStyle().Bold(true).Foreground(black).Background(brightColor),
		HeaderReverse: lipgloss.NewStyle().Bold(true).Foreground(black).Background(brightColor),
		KeyLegend:     lipgloss.NewStyle().Bold(true).Foreground(black).Background(brightColor),
		BadgeAdmin:    lipgloss.NewStyle().Bold(true).Foreground(black).Background(white),
		BadgeOperator: lipgloss.NewStyle().Bold(true).Foreground(black).Background(brightColor),
		BadgeView:     lipgloss.NewStyle().Bold(true).Foreground(white).Background(red),
	}
}

// RoleBadge mirrors the original's role -> badge-text heuristic.
func RoleBadge(t Theme, role string, canWrite bool) string {
	roleLower := strings.ToLower(role)
	isView := strings.Contains(roleLower, "view") || strings.Contains(roleLower, "read")
	switch {
	case isView || !canWrite:
		return t.BadgeView.Render(" VIEW-ONLY ")
	case roleLower == "admin" || roleLower == "partdb admin":
		return t.BadgeAdmin.Render(" ADMIN ")
	default:
		return t.BadgeOperator.Render(" OPERATOR ")
	}
}
