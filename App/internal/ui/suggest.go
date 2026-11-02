package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"swarg/internal/ui/command"
	"swarg/internal/ui/hover"
	"swarg/internal/ui/mdstream"
	"swarg/internal/ui/menu"
)

// ---------------------------------------------------------------------------
// Command suggestions: while the input is one "/token" the matching commands show above
// the input box; Tab or a click completes, Up/Down move, Enter submits the line as typed.
// The input text is highlighted while it is a command (T3 ui item 6, R-35).
// ---------------------------------------------------------------------------

// cmdStyle is the look of the input text while it is a command.
var cmdStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.Accent))

// suggestions returns the commands whose name starts with line, in table order; nil unless
// line is a single "/token" (no blank, not "//"). bottom() cuts the rows to the room above
// the input box.
func suggestions(line string) []command.Spec {
	line = strings.TrimLeft(line, " \t") // command.Parse trims too
	if !strings.HasPrefix(line, "/") || strings.HasPrefix(line, "//") || strings.ContainsAny(line, " \t\n\r") {
		return nil
	}
	var out []command.Spec
	for _, c := range command.All() {
		if strings.HasPrefix(c.Name, line) {
			out = append(out, c)
		}
	}
	return out
}

// isCommandLine says whether the input is a command being typed or typed: a "/token" with
// matches, or a known command name followed by its arguments.
func isCommandLine(line string) bool {
	line = strings.TrimLeft(line, " \t")
	if len(suggestions(line)) > 0 {
		return true
	}
	f := strings.Fields(line)
	return len(f) > 0 && strings.HasPrefix(line, "/") && command.Lookup(f[0]) != nil
}

// suggestRoom is how many extra rows fit above the input box (bottom() keeps one list row).
func (m *model) suggestRoom() int {
	return max(m.rowsHeight()-(m.ta.Height()+2)-2, 0)
}

// current returns the suggestions that fit on screen and the selected one, clamped (a row the
// user cannot see is never selected or completed).
func (m *model) current() ([]command.Spec, int) {
	s := suggestions(m.ta.Value())
	if n := m.suggestRoom(); len(s) > n {
		s = s[:n]
	}
	if len(s) == 0 {
		return nil, 0
	}
	return s, min(max(m.suggSel, 0), len(s)-1)
}

// suggesting says whether suggestion rows are shown: the picker and a confirmation take
// the keys, so none then.
func (m *model) suggesting() bool {
	if m.pick != nil || m.ask != nil {
		return false
	}
	s, _ := m.current()
	return len(s) > 0
}

// suggestRows renders the suggestion rows (name and arguments, then the description), the
// selected one highlighted, and the hotspots cmd:<i> of each row (y relative to the first).
func (m *model) suggestRows(w int) (rows []string, hot []hover.Spot) {
	s, sel := m.current()
	names := make([]string, len(s))
	nw := 0
	for i, c := range s {
		names[i] = strings.TrimSpace(c.Name + " " + c.Args)
		nw = max(nw, lipgloss.Width(names[i]))
	}
	for i, c := range s {
		text := ansi.Truncate(fmt.Sprintf("  %-*s  %s", nw, names[i], c.Desc), w, "…")
		hot = append(hot, hover.Spot{ID: fmt.Sprintf("cmd:%d", i), Y: i, X0: 0, X1: lipgloss.Width(text)})
		if i == sel {
			text = menu.Sel.Render(text + strings.Repeat(" ", max(w-lipgloss.Width(text), 0)))
		}
		rows = append(rows, text)
	}
	return rows, hot
}

// moveSuggestion moves the selection by d (wrapping).
func (m *model) moveSuggestion(d int) {
	s, sel := m.current()
	if len(s) == 0 {
		return
	}
	m.suggSel = (sel + d + len(s)) % len(s)
}

// completeSuggestion puts suggestion i into the input: the command name, plus a space when
// it takes arguments. The cursor ends at the end.
func (m *model) completeSuggestion(i int) {
	s, _ := m.current()
	if i < 0 || i >= len(s) {
		return
	}
	v := s[i].Name
	if s[i].Args != "" {
		v += " "
	}
	m.ta.SetValue(v)
	m.suggSel = 0
}

// styleInput runs after every textarea update: an edited input resets the suggestion
// selection, and the text is coloured while it is a command (the cursor line has its own
// style in the textarea, so both are set).
func (m *model) styleInput() {
	if v := m.ta.Value(); v != m.typed { // edited: the selection starts over
		m.typed, m.suggSel = v, 0
	}
	st := m.ta.Styles()
	want := lipgloss.NewStyle()
	if isCommandLine(m.ta.Value()) {
		want = cmdStyle
	}
	if st.Focused.CursorLine.GetForeground() == want.GetForeground() && st.Focused.Text.GetForeground() == want.GetForeground() {
		return
	}
	st.Focused.CursorLine, st.Focused.Text = want, want
	m.ta.SetStyles(st)
}
