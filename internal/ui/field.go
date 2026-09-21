package ui

import "strings"

// Field is one entry on a data-entry screen: labeled, in-place editable at
// its own position (not a scroll-and-reprint prompt), rendered bright/
// underlined when input-capable and dim when protected (display-only) —
// the same attribute distinction a real 5250 field uses.
type Field struct {
	Label     string
	Value     string
	Protected bool
	Password  bool
	// Fresh marks a suggested value (a quantity of 1, the current count): the first character typed
	// replaces it instead of being appended, so typing 12 over a suggested 1 gives 12, not 112.
	// Backspace edits it like any other text.
	Fresh bool
}

// FieldList is the reusable field-navigation contract every data-entry
// screen composes from, so tab order/cursor/edit logic lives in one place
// instead of being reimplemented per screen.
type FieldList struct {
	Fields  []Field
	Active  int
	Reached int // highest field index the cursor has visited; RenderClassic reveals fields up to here, one prompt at a time like smart_input
}

func NewFieldList(fields ...Field) *FieldList {
	fl := &FieldList{Fields: fields}
	fl.skipProtectedForward()
	fl.Reached = fl.Active
	return fl
}

func (f *FieldList) skipProtectedForward() {
	for i := 0; i < len(f.Fields); i++ {
		if !f.Fields[f.Active].Protected {
			return
		}
		f.Active = (f.Active + 1) % len(f.Fields)
	}
}

func (f *FieldList) Next() {
	if len(f.Fields) == 0 {
		return
	}
	for i := 0; i < len(f.Fields); i++ {
		f.Active = (f.Active + 1) % len(f.Fields)
		if f.Active > f.Reached {
			f.Reached = f.Active
		}
		if !f.Fields[f.Active].Protected {
			return
		}
	}
}

func (f *FieldList) Prev() {
	if len(f.Fields) == 0 {
		return
	}
	for i := 0; i < len(f.Fields); i++ {
		f.Active = (f.Active - 1 + len(f.Fields)) % len(f.Fields)
		if f.Active > f.Reached {
			f.Reached = f.Active
		}
		if !f.Fields[f.Active].Protected {
			return
		}
	}
}

func (f *FieldList) Type(r rune) {
	if len(f.Fields) == 0 || f.Fields[f.Active].Protected {
		return
	}
	fld := &f.Fields[f.Active]
	if fld.Fresh {
		fld.Value, fld.Fresh = "", false
	}
	fld.Value += string(r)
}

func (f *FieldList) Backspace() {
	if len(f.Fields) == 0 || f.Fields[f.Active].Protected {
		return
	}
	f.Fields[f.Active].Fresh = false
	v := f.Fields[f.Active].Value
	if len(v) > 0 {
		f.Fields[f.Active].Value = v[:len(v)-1]
	}
}

// ActiveEmpty reports whether the focused editable field has no text yet —
// this is the condition under which letter mnemonics (Q/U/L/+) still act as
// global commands instead of literal input, mirroring the original's
// "empty buffer" gate on smart_input.
func (f *FieldList) ActiveEmpty() bool {
	if len(f.Fields) == 0 {
		return true
	}
	return f.Fields[f.Active].Value == ""
}

func (f *FieldList) Value(i int) string {
	if i < 0 || i >= len(f.Fields) {
		return ""
	}
	return f.Fields[i].Value
}

func (f *FieldList) Values() []string {
	out := make([]string, len(f.Fields))
	for i, fld := range f.Fields {
		out[i] = fld.Value
	}
	return out
}

// Render draws one line per field: "Label........: value_", protected
// fields dim, the active input field bright/underlined with a block cursor.
func (f *FieldList) Render(t Theme) string {
	var b strings.Builder
	labelWidth := 0
	for _, fld := range f.Fields {
		if len(fld.Label) > labelWidth {
			labelWidth = len(fld.Label)
		}
	}
	for i, fld := range f.Fields {
		label := padRight(fld.Label+":", labelWidth+1)
		display := fld.Value
		if fld.Password {
			display = strings.Repeat("*", len(display))
		}
		var valueStyled string
		switch {
		case fld.Protected:
			valueStyled = t.Protected.Render(display)
		case i == f.Active:
			valueStyled = t.Accent.Render(display + "_")
		default:
			valueStyled = t.Text.Render(display + " ")
		}
		b.WriteString(t.Text.Render(label) + " " + valueStyled + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderClassic draws the Python TUI's inline prompts instead of an aligned
// field table: each field is "Label: value" with the label in bold green,
// only fields up to the furthest one reached so far are shown (the next
// prompt appears once the previous is answered, and stays visible after),
// and the active field ends in a block cursor. Password values echo as *.
func (f *FieldList) RenderClassic(t Theme) string {
	var lines []string
	for i := 0; i <= f.Reached && i < len(f.Fields); i++ {
		fld := f.Fields[i]
		label := fld.Label
		if !strings.HasSuffix(label, ":") {
			label += ":"
		}
		display := fld.Value
		if fld.Password {
			display = strings.Repeat("*", len(display))
		}
		if fld.Protected {
			display = t.Protected.Render(display)
		}
		lines = append(lines, RenderPrompt(t, label, display, i == f.Active && !fld.Protected))
	}
	return strings.Join(lines, "\n")
}
