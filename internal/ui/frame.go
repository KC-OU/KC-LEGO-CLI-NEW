package ui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"time"
)

// SystemName is the fixed top-left signon-screen identity, matching the
// original TUI's "KCPARTS".
const SystemName = "KCPARTS"

// DefaultFKeys is the standard footer legend shown on most screens.
var DefaultFKeys = [][2]string{
	{"F1", "Help"}, {"F2", "Go To"}, {"F3", "Exit"}, {"F9", "Undo"}, {"F6", "Quick Add"}, {"F10", "Lock"}, {"F12", "Cancel"},
}

const DefaultMnemonics = "?=Help  Q=Exit  U=Undo  +=Quick Add  L=Lock  Esc=Back"

// Frame composes the full 5250 screen: header band, body, row-23 message
// line, function-key row, and the row-24 command line with the caller's
// current input buffer echoed after "===>". Rows are stacked in the spec's
// order rather than addressed into a literal fixed character-cell grid —
// bubbletea repaints the whole frame each update, so ordered composition
// gets the same visual contract without a cell-buffer renderer.
func Frame(t Theme, screenID, title, user, role, extra, badge, body, message string, messageIsError bool, fkeys [][2]string, mnemonics, commandBuffer string) string {
	var b strings.Builder
	when := time.Now().Format("2006-01-02 15:04:05")
	b.WriteString(HeaderBand(t, SystemName, screenID, title, user, role, when, extra, badge))
	b.WriteString("\n\n")
	b.WriteString(body)
	b.WriteString("\n\n")
	if message != "" {
		b.WriteString(MessageLine(t, message, messageIsError))
	}
	b.WriteString("\n")
	b.WriteString(FKeyRow(t, fkeys, mnemonics))
	b.WriteString("\n")
	b.WriteString(t.Text.Render("Selection or command ===> ") + t.Accent.Render(commandBuffer+"_"))
	return b.String()
}

// ClassicFrame composes a post-login screen the way modernwms_tui.py's
// draw_header + screen body does: header band, one-line tab bar, a rule, and
// the F-key legend directly under it (no letter-mnemonics line — the letter
// keys still work, the Python app just never listed them), then the body, a
// message line, optionally the legend repeated at the bottom (the main menu
// does this), and the caller's inline prompt. It also returns the 0-indexed
// row the body starts on, counted from the assembled prefix rather than
// assumed, so touch-mode click mapping can't drift from what was drawn.
func ClassicFrame(t Theme, screenID, title, user, role, extra, badge string, tabs [][2]string, activeTab string, fkeys [][2]string, body, message string, messageIsError, repeatLegend bool, prompt string) (string, int) {
	var b strings.Builder
	when := time.Now().Format("2006-01-02 15:04:05")
	b.WriteString(HeaderBand(t, SystemName, screenID, title, user, role, when, extra, badge))
	b.WriteString("\n")
	b.WriteString(TabBar(t, tabs, activeTab))
	b.WriteString("\n")
	b.WriteString(RuleLine(t))
	b.WriteString("\n")
	b.WriteString(ClassicLegend(t, fkeys))
	b.WriteString("\n")
	bodyRow := strings.Count(b.String(), "\n")

	b.WriteString(body)
	if message != "" || repeatLegend || prompt != "" {
		b.WriteString("\n")
	}
	if message != "" {
		// wms_console.status: a coloured ✓/✗ icon, then the plain message.
		// One row only (the screen has no spare line), so a long message ends in an ellipsis.
		b.WriteString("\n" + ansi.Truncate(Status(t, !messageIsError, message), t.Width, "…"))
	}
	if repeatLegend {
		b.WriteString("\n" + RuleLine(t) + "\n" + ClassicLegend(t, fkeys))
	}
	if prompt != "" {
		b.WriteString("\n" + prompt)
	}
	return b.String(), bodyRow
}
