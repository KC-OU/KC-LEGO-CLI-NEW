package uiapp

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The "/" filter on every list: type to narrow the rows, Enter keeps the
// filter, Esc clears it. It is fuzzy: every word you type must appear in the
// row's text in order but not necessarily together ("bk 2x4" finds "Brick 2 x
// 4"), and rows where a word matches as typed rank above scattered matches.

// subsequence reports whether the letters of needle appear in hay in order.
func subsequence(hay, needle string) bool {
	h := []rune(hay)
	i := 0
	for _, r := range needle {
		for i < len(h) && h[i] != r {
			i++
		}
		if i == len(h) {
			return false
		}
		i++
	}
	return true
}

// filterRows keeps the rows matching every word of query, best matches first
// (a stable order otherwise). An empty query keeps everything as it was.
func filterRows(rows [][]string, query string) [][]string {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return rows
	}
	type scored struct {
		row   []string
		score int
	}
	var kept []scored
rowLoop:
	for _, r := range rows {
		hay := strings.ToLower(strings.Join(r, " "))
		score := 0
		for _, term := range terms {
			switch {
			case strings.Contains(hay, term):
				score += 2
			case subsequence(hay, term):
				score++
			default:
				continue rowLoop
			}
		}
		kept = append(kept, scored{r, score})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].score > kept[j].score })
	out := make([][]string, len(kept))
	for i, k := range kept {
		out[i] = k.row
	}
	return out
}

// capturing is true while the screen is taking typed text, so global letter
// shortcuts (q, u, l, g, +, ?) must not fire.
func (s *tableScreen) capturing() bool { return s.filtering }

func (s *tableScreen) applyFilter() { s.rows, s.top = filterRows(s.all, s.filter), 0 }

// filterLabel is what the title line appends while a filter is active.
func (s *tableScreen) filterLabel() string {
	if !s.filtering && s.filter == "" {
		return ""
	}
	cursor := ""
	if s.filtering {
		cursor = "_"
	}
	return fmt.Sprintf("   [/ %s%s : %d of %d]", s.filter, cursor, len(s.rows), len(s.all))
}

// handleFilterKey consumes a key while filtering and reports whether it did.
func (s *tableScreen) handleFilterKey(msg tea.KeyMsg) bool {
	if !s.filtering {
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '/' {
			s.filtering = true
			return true
		}
		return false
	}
	switch msg.Type {
	case tea.KeyEnter:
		s.filtering = false
	case tea.KeyEsc:
		s.filtering, s.filter = false, ""
		s.applyFilter()
	case tea.KeyBackspace:
		if r := []rune(s.filter); len(r) > 0 {
			s.filter = string(r[:len(r)-1])
			s.applyFilter()
		}
	case tea.KeySpace:
		s.filter += " "
		s.applyFilter()
	case tea.KeyRunes:
		s.filter += string(msg.Runes)
		s.applyFilter()
	}
	return true
}
