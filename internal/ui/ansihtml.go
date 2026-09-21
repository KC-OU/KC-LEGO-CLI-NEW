package ui

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

var csiRE = regexp.MustCompile("\x1b\\[([0-9;:?]*)([A-Za-z])")

// strayRE matches what csiRE leaves: OSC strings ("ESC ] ... BEL"), a truncated CSI at the end, and lone escapes.
var strayRE = regexp.MustCompile("\x1b(\\][^\x07\x1b]*(\x07|\x1b\\\\)?|\\[[0-9;:?]*$|[@-Z\\\\-_]|$)")

// ansi16 is the standard 16-colour palette (the xterm defaults), used to draw terminal
// colours on a web page.
var ansi16 = [16]string{
	"#000000", "#cd0000", "#00cd00", "#cdcd00", "#0000ee", "#cd00cd", "#00cdcd", "#e5e5e5",
	"#7f7f7f", "#ff0000", "#00ff00", "#ffff00", "#5c5cff", "#ff00ff", "#00ffff", "#ffffff",
}

func color256(n int) string {
	switch {
	case n < 16:
		return ansi16[n]
	case n < 232:
		n -= 16
		lv := func(i int) int {
			if i == 0 {
				return 0
			}
			return 55 + i*40
		}
		return fmt.Sprintf("#%02x%02x%02x", lv(n/36), lv(n/6%6), lv(n%6))
	}
	g := 8 + (n-232)*10
	return fmt.Sprintf("#%02x%02x%02x", g, g, g)
}

type sgrState struct {
	fg, bg                  string
	bold, faint, under, rev bool
}

func (s sgrState) style() string {
	fg, bg := s.fg, s.bg
	if s.rev {
		if fg == "" {
			fg = "#cccccc"
		}
		if bg == "" {
			bg = "#0c0c0c"
		}
		fg, bg = bg, fg
	}
	var b []string
	if fg != "" {
		b = append(b, "color:"+fg)
	}
	if bg != "" {
		b = append(b, "background:"+bg)
	}
	if s.bold {
		b = append(b, "font-weight:bold")
	}
	if s.faint {
		b = append(b, "opacity:.6")
	}
	if s.under {
		b = append(b, "text-decoration:underline")
	}
	return strings.Join(b, ";")
}

// apply updates the state for one SGR parameter list ("1;38;2;255;0;0").
func (s *sgrState) apply(params string) {
	if params == "" {
		*s = sgrState{}
		return
	}
	p := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	num := func(i int) int {
		if i >= len(p) {
			return 0
		}
		n, _ := strconv.Atoi(p[i])
		return n
	}
	for i := 0; i < len(p); i++ {
		switch n := num(i); {
		case n == 0:
			*s = sgrState{}
		case n == 1:
			s.bold = true
		case n == 2:
			s.faint = true
		case n == 4:
			s.under = true
		case n == 7:
			s.rev = true
		case n == 22:
			s.bold, s.faint = false, false
		case n == 24:
			s.under = false
		case n == 27:
			s.rev = false
		case n >= 30 && n <= 37:
			s.fg = ansi16[n-30]
		case n >= 90 && n <= 97:
			s.fg = ansi16[n-90+8]
		case n >= 40 && n <= 47:
			s.bg = ansi16[n-40]
		case n >= 100 && n <= 107:
			s.bg = ansi16[n-100+8]
		case n == 39:
			s.fg = ""
		case n == 49:
			s.bg = ""
		case n == 38 || n == 48:
			target := &s.fg
			if n == 48 {
				target = &s.bg
			}
			switch num(i + 1) {
			case 5:
				*target = color256(num(i + 2))
				i += 2
			case 2:
				*target = fmt.Sprintf("#%02x%02x%02x", num(i+2)&255, num(i+3)&255, num(i+4)&255)
				i += 4
			}
		}
	}
}

// ANSIToHTML turns terminal output with colour codes into HTML (spans with inline styles),
// escaping all text. Cursor movement and other escape sequences are dropped. Used to
// draw real screens in the documentation.
func ANSIToHTML(s string) string {
	var out strings.Builder
	var st sgrState
	flush := func(text string) {
		text = strayRE.ReplaceAllString(text, "")
		if text == "" {
			return
		}
		esc := html.EscapeString(text)
		if style := st.style(); style != "" {
			out.WriteString(`<span style="` + style + `">` + esc + `</span>`)
		} else {
			out.WriteString(esc)
		}
	}
	pos := 0
	for _, m := range csiRE.FindAllStringSubmatchIndex(s, -1) {
		flush(s[pos:m[0]])
		if s[m[4]:m[5]] == "m" {
			st.apply(s[m[2]:m[3]])
		}
		pos = m[1]
	}
	flush(s[pos:])
	return strings.ReplaceAll(strings.ReplaceAll(out.String(), "\x1b", ""), "\r", "")
}
