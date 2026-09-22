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
var Themes = []string{"green", "amber", "high-contrast", "colorblind", "dracula", "half-life", "nord", "gruvbox", "catppuccin", "tokyo-night", "ibm-3270", "matrix", "lego"}

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
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "amber":
		return amberTheme()
	case "high-contrast", "highcontrast", "contrast":
		return highContrastTheme()
	case "colorblind", "colourblind", "cvd":
		return colorblindTheme()
	case "halflife", "hev":
		name = "half-life"
	case "tokyonight", "tokyo":
		name = "tokyo-night"
	case "3270", "ibm":
		name = "ibm-3270"
	case "lego-classic", "brick":
		name = "lego"
	}
	if p, ok := palettes[name]; ok {
		return paletteTheme(name, p)
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

// palette is a named colour scheme in 256-colour codes (what telnet clients show
// reliably); paletteTheme turns one into a Theme.
type palette struct {
	brand, text, muted, ok, warn, bad, accent, strong string
	bar, barText                                      string // title/heading/key-legend bars: background and its text
	admin, operator, view                             string // role badge backgrounds (text is barText; view is always light text)
}

func paletteTheme(name string, p palette) Theme {
	c := func(s string) lipgloss.Color { return lipgloss.Color(s) }
	fg := func(col string) lipgloss.Style { return lipgloss.NewStyle().Foreground(c(col)) }
	bold := func(col string) lipgloss.Style { return fg(col).Bold(true) }
	badge := func(bg string) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).Foreground(c(p.barText)).Background(c(bg))
	}
	rev := badge(p.bar)
	return Theme{
		Name:          name,
		Brand:         bold(p.brand),
		Text:          fg(p.text),
		Muted:         fg(p.muted),
		Success:       bold(p.ok),
		Warning:       bold(p.warn),
		Danger:        bold(p.bad),
		Accent:        bold(p.accent).Underline(true),
		Protected:     fg(p.muted),
		Strong:        bold(p.strong),
		TitleReverse:  rev,
		HeaderReverse: rev,
		KeyLegend:     rev,
		BadgeAdmin:    badge(p.admin),
		BadgeOperator: badge(p.operator),
		BadgeView:     lipgloss.NewStyle().Bold(true).Foreground(c("15")).Background(c(p.view)),
	}
}

// palettes are the named schemes beyond the classic terminal colours, approximated in
// 256 colours from each scheme's published hex values.
var palettes = map[string]palette{
	// draculatheme.com: purple bars, pink brand, cyan input, on a dark grey background.
	"dracula": {brand: "212", text: "253", muted: "61", ok: "84", warn: "228", bad: "203", accent: "117", strong: "212",
		bar: "141", barText: "236", admin: "212", operator: "117", view: "203"},
	// ~/.zsh/themes/half-life.zsh-theme: hazard orange, HEV cyan, lambda gold, biohazard green.
	"half-life": {brand: "208", text: "208", muted: "240", ok: "118", warn: "214", bad: "196", accent: "51", strong: "214",
		bar: "208", barText: "0", admin: "51", operator: "208", view: "196"},
	// nordtheme.com: frost blues on polar night, aurora for states.
	"nord": {brand: "110", text: "253", muted: "60", ok: "108", warn: "222", bad: "167", accent: "116", strong: "255",
		bar: "110", barText: "236", admin: "116", operator: "110", view: "167"},
	// gruvbox: warm retro browns, yellow brand, aqua input.
	"gruvbox": {brand: "214", text: "223", muted: "245", ok: "142", warn: "214", bad: "167", accent: "108", strong: "229",
		bar: "214", barText: "235", admin: "175", operator: "214", view: "124"},
	// catppuccin mocha: soft pastels, mauve bars.
	"catppuccin": {brand: "183", text: "189", muted: "103", ok: "151", warn: "223", bad: "211", accent: "117", strong: "218",
		bar: "183", barText: "235", admin: "218", operator: "117", view: "161"},
	// tokyo night: deep navy, neon blue and violet.
	"tokyo-night": {brand: "111", text: "189", muted: "60", ok: "149", warn: "179", bad: "204", accent: "117", strong: "141",
		bar: "111", barText: "234", admin: "141", operator: "111", view: "204"},
	// IBM 3270: the colour mainframe terminal — green fields, turquoise input, blue/white headings.
	"ibm-3270": {brand: "15", text: "40", muted: "33", ok: "40", warn: "226", bad: "196", accent: "51", strong: "15",
		bar: "33", barText: "15", admin: "15", operator: "51", view: "196"},
	// the Matrix: everything green, brightness does the work.
	"matrix": {brand: "46", text: "34", muted: "22", ok: "46", warn: "154", bad: "160", accent: "118", strong: "46",
		bar: "46", barText: "16", admin: "118", operator: "46", view: "124"},
	// LEGO Classic: brick red, yellow and blue, like the logo and a 2x4 brick.
	"lego": {brand: "196", text: "231", muted: "250", ok: "34", warn: "226", bad: "196", accent: "226", strong: "226",
		bar: "196", barText: "226", admin: "226", operator: "27", view: "88"},
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
