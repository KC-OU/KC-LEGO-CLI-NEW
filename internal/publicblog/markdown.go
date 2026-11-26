package publicblog

import (
	"html"
	"html/template"
	"regexp"
	"strings"
)

var (
	reBold   = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	reItalic = regexp.MustCompile(`\*([^*\n]+)\*`)
	reCode   = regexp.MustCompile("`([^`\n]+)`")
	reImg    = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)\)`)
	reLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	reOL     = regexp.MustCompile(`^\d+[.)] `)
)

// safeURL allows only http(s) links and site-relative paths -- never javascript: or data:.
func safeURL(u string) bool {
	u = html.UnescapeString(u)
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") || (strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//"))
}

func inline(s string) string {
	s = html.EscapeString(s) // everything is escaped first, so no raw HTML can get through
	s = reCode.ReplaceAllString(s, "<code>$1</code>")
	s = reImg.ReplaceAllStringFunc(s, func(m string) string {
		g := reImg.FindStringSubmatch(m)
		// Own photos only: an image from another site would send the visitor's address to that site.
		if !strings.HasPrefix(html.UnescapeString(g[2]), "/") || !safeURL(g[2]) {
			return g[1]
		}
		return `<img src="` + g[2] + `" alt="` + g[1] + `" loading="lazy">`
	})
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		g := reLink.FindStringSubmatch(m)
		if !safeURL(g[2]) {
			return g[1]
		}
		return `<a href="` + g[2] + `" rel="noopener nofollow">` + g[1] + `</a>`
	})
	s = reBold.ReplaceAllString(s, "<strong>$1</strong>")
	return reItalic.ReplaceAllString(s, "<em>$1</em>")
}

// Markdown renders the small subset posts use: # headings, paragraphs, - and 1. lists,
// > quotes, --- rules, **bold**, *italic*, `code`, links and images. Raw HTML is escaped,
// not passed through.
func Markdown(src string) template.HTML {
	var b strings.Builder
	var para []string
	list := "" // "ul" or "ol" while inside a list
	closeList := func() {
		if list != "" {
			b.WriteString("</" + list + ">\n")
			list = ""
		}
	}
	flush := func() {
		if len(para) > 0 {
			b.WriteString("<p>" + inline(strings.Join(para, " ")) + "</p>\n")
			para = nil
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			flush()
			closeList()
		case strings.HasPrefix(t, "#"):
			flush()
			closeList()
			n := len(t) - len(strings.TrimLeft(t, "#"))
			if n > 3 {
				n = 3
			}
			b.WriteString("<h" + string(rune('1'+n)) + ">" + inline(strings.TrimSpace(t[n:])) + "</h" + string(rune('1'+n)) + ">\n")
		case t == "---":
			flush()
			closeList()
			b.WriteString("<hr>\n")
		case strings.HasPrefix(t, "> "):
			flush()
			closeList()
			b.WriteString("<blockquote>" + inline(t[2:]) + "</blockquote>\n")
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* "), reOL.MatchString(t):
			flush()
			kind := "ul"
			item := t[2:]
			if reOL.MatchString(t) {
				kind = "ol"
				item = reOL.ReplaceAllString(t, "")
			}
			if list != kind {
				closeList()
				b.WriteString("<" + kind + ">")
				list = kind
			}
			b.WriteString("<li>" + inline(item) + "</li>")
		default:
			closeList()
			para = append(para, t)
		}
	}
	flush()
	closeList()
	return template.HTML(b.String())
}
