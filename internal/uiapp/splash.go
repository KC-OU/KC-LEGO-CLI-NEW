package uiapp

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// A short themed animation after sign-on (about 1.2 s, any key skips it): an HEV
// suit boot for half-life, a brick being filled in for lego, falling glyphs for
// matrix, a brand banner for the rest. Off with MODERNWMS_TUI_SPLASH=0 and under
// NO_COLOR. It is plain text and colour codes, so it works over telnet.

const (
	splashFrames = 7
	splashStep   = 180 * time.Millisecond
)

type splashTickMsg struct{}

func splashTick() tea.Cmd {
	return tea.Tick(splashStep, func(time.Time) tea.Msg { return splashTickMsg{} })
}

func (a *App) startSplash() {
	if config.Get(config.TUISplash) == "0" || a.theme.Mono {
		return
	}
	a.splashFrame = 1
	a.pendingCmd = splashTick()
}

// splashUpdate advances or ends the animation; it reports whether msg was used.
func (a *App) splashUpdate(msg tea.Msg) (bool, tea.Cmd) {
	if a.splashFrame == 0 {
		return false, nil
	}
	switch msg.(type) {
	case splashTickMsg:
		a.splashFrame++
		if a.splashFrame > splashFrames {
			a.splashFrame = 0
			return true, nil
		}
		return true, splashTick()
	case tea.KeyMsg:
		a.splashFrame = 0 // any key skips it, and is not passed on
		return true, nil
	}
	return false, nil
}

func (a *App) splashView() string {
	t := a.theme
	f := a.splashFrame
	var art []string
	switch t.Name {
	case "half-life":
		steps := []string{
			"H.E.V. MARK VI PROTECTIVE SYSTEM ........ ONLINE",
			"Atmospheric contaminant sensors ......... ACTIVE",
			"Vital sign monitoring ................... ACTIVE",
			"Automatic medical systems ............... ACTIVE",
			"Parts inventory uplink .................. ACTIVE",
			"Munition level monitoring ............... ACTIVE",
		}
		art = append(art, t.Brand.Render("λ  BLACK MESA — KC-PARTS CONTROL SUITE"), "")
		for i := 0; i < min(f, len(steps)); i++ {
			art = append(art, t.Text.Render(steps[i]))
		}
		if f >= splashFrames {
			art = append(art, "", t.Strong.Render("Have a very safe day."))
		}
	case "lego":
		studs := t.Danger.Render(" ▄█▄  ▄█▄  ▄█▄  ▄█▄ ")
		row := t.Danger.Render(strings.Repeat("█", 21))
		empty := t.Muted.Render(strings.Repeat("░", 21))
		art = append(art, studs)
		for i := 0; i < 4; i++ {
			if i < f {
				art = append(art, row)
			} else {
				art = append(art, empty)
			}
		}
		art = append(art, "", t.Warning.Render("KC-PARTS")+t.Strong.Render("  LEGO collection"))
	case "matrix":
		const glyphs = "ｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿ01ﾀﾁﾂﾃﾄ"
		g := []rune(glyphs)
		for y := 0; y < 8; y++ {
			var b strings.Builder
			for x := 0; x < 30; x++ {
				head := (x*7 + f*2) % 11
				switch {
				case y == head:
					b.WriteString(t.Strong.Render(string(g[(x*3+y+f)%len(g)])))
				case y < head && (x+y)%3 != 0:
					b.WriteString(t.Muted.Render(string(g[(x+y*5+f)%len(g)])))
				default:
					b.WriteString(" ")
				}
			}
			art = append(art, b.String())
		}
		art = append(art, "", t.Brand.Render("Wake up. The parts are waiting."))
	default:
		w := 30
		done := min(w, f*w/splashFrames)
		art = append(art, t.Brand.Render(ui.Brand+" KC-PARTS")+t.Muted.Render("  ModernWMS · Part-DB · LEGO"), "",
			t.Accent.Render(strings.Repeat("█", done))+t.Muted.Render(strings.Repeat("░", w-done)))
	}
	art = append(art, "", t.Muted.Render("any key to skip"))
	h := a.height
	if h <= 0 {
		h = 24
	}
	return lipgloss.Place(t.W(), h, lipgloss.Center, lipgloss.Center, strings.Join(art, "\n"))
}
