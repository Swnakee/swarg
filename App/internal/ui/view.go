package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"swarg/internal/chat"
	"swarg/internal/ui/hover"
	"swarg/internal/ui/mdstream"
)

// ---------------------------------------------------------------------------
// Styles and constants
// ---------------------------------------------------------------------------

const (
	spinText       = "Thinking…"
	minCols        = 20 // below this width or minRows height the screen only says it is too small
	minRows        = 5
	defaultRows    = 24 // screen height until the first WindowSizeMsg
	tooSmallText   = "terminal too small"
	compactingText = "compacting…"
)

// Every colour comes from the one project palette (mdstream.Colors).
var (
	userStyle = lipgloss.NewStyle().Background(lipgloss.Color(mdstream.Colors.UserBg)).
			Foreground(lipgloss.Color(mdstream.Colors.UserFg))
	boxColor    = lipgloss.Color(mdstream.Colors.Dim)
	noticeStyle = lipgloss.NewStyle().Faint(true)
	headStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.Accent))
	outStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.Dim))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.Error))
	askStyle    = lipgloss.NewStyle().Bold(true)
)

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// screen is one screen: exactly height rows, the cursor in absolute coordinates.
type screen struct {
	rows   []string
	cursor *tea.Cursor
}

// bottomArea is what is pinned under the list: optional extra rows (the ask rows while a
// confirmation waits, or the command suggestions), the input box and the status line. The
// Progress line is not pinned: it is the last row of the list content (footer). hot are the
// clickable spans of the extra rows, y relative to the first extra row; screen() registers them.
type bottomArea struct {
	extra  []string
	box    []string
	status string
	hot    []hover.Spot
}

func (b bottomArea) height() int { return len(b.extra) + len(b.box) + 1 }

// rowsHeight is the screen height in rows.
func (m *model) rowsHeight() int {
	if m.height > 0 {
		return m.height
	}
	return defaultRows
}

// tooSmall says whether the screen is below the minimum the layout works at.
func (m *model) tooSmall() bool { return m.width < minCols || m.rowsHeight() < minRows }

// bottom builds the pinned rows. The ask rows give way before the list loses its last row;
// every row is cut to the width.
func (m *model) bottom() bottomArea {
	w, h := m.width, m.rowsHeight()
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(boxColor).
		Width(w).Render(m.ta.View())
	b := bottomArea{box: strings.Split(box, "\n"), status: m.panelLine()}
	var extra []string
	switch {
	case m.ask != nil:
		prompt := askPrompt(m.ask.call, m.d.Jobs, m.taskGoal())
		if m.ta.Value() != "" { // the keys type into the input now: say how to answer
			prompt += "\n" + askHint
		}
		for _, r := range wrapText(prompt, w) {
			extra = append(extra, askStyle.Render(r))
		}
	case m.suggesting():
		extra, b.hot = m.suggestRows(w)
	}
	room := h - len(b.box) - 1 - 1 // one list row stays
	switch {
	case room <= 0:
		extra = nil
	case len(extra) > room && m.ask != nil: // keep the first rows and the key line
		extra = append(extra[:room-1:room-1], extra[len(extra)-1])
	case len(extra) > room:
		extra = extra[:room]
	}
	b.extra = extra
	b.hot = slices.DeleteFunc(b.hot, func(s hover.Spot) bool { return s.Y >= len(extra) })
	return b
}

// listHeight is the number of rows the conversation list gets.
func (m *model) listHeight() int {
	if m.tooSmall() {
		return 0
	}
	return max(m.rowsHeight()-m.bottom().height(), 0)
}

// View draws the whole screen: the conversation content (list and footer), then the pinned ask
// prompt, input box and status line. The alt screen and the mouse are always on; all motion is
// reported so clickable elements can show the hover. KeyboardEnhancements stays zero: Bubble Tea
// always requests key disambiguation, which is what makes shift+enter distinguishable.
func (m *model) View() tea.View {
	f := m.screen()
	v := tea.NewView(fitProfile(strings.Join(f.rows, "\n"), m.profile))
	v.AltScreen, v.MouseMode, v.Cursor = true, tea.MouseModeAllMotion, f.cursor
	return v
}

// screen builds the screen from the model. It changes nothing but the hover spots: every
// clickable element registers its cells on the way (hover.add) and the spots under the
// pointer are underlined at the end (not while the button is held: a drag is a selection).
func (m *model) screen() screen {
	m.hv.Begin()
	h := m.rowsHeight()
	if m.tooSmall() {
		rows := make([]string, h)
		rows[0] = ansi.Truncate(tooSmallText, max(m.width, 0), "")
		return screen{rows: rows}
	}
	b := m.bottom()
	listH := h - b.height()
	var rows []string
	if m.pick != nil { // the session picker takes the list area
		m.pick.Resize(m.width, listH)
		rows = m.pick.Lines()
	} else {
		rows = m.listRows(listH)
	}
	for len(rows) < listH {
		rows = append(rows, "")
	}
	for _, s := range b.hot {
		m.hv.Add(s.ID, listH+s.Y, s.X0, s.X1)
	}
	boxTop := len(rows) + len(b.extra)
	rows = append(append(append(rows, b.extra...), b.box...), b.status)
	c := tea.NewCursor(1, boxTop+1)
	if tc := m.ta.Cursor(); tc != nil {
		c.X, c.Y, c.Shape, c.Blink, c.Color = tc.X+1, tc.Y+boxTop+1, tc.Shape, tc.Blink, tc.Color
	}
	m.hotPanel(len(rows) - 1)
	if !m.pr.down {
		m.hv.Apply(rows)
	}
	return screen{rows: rows, cursor: c}
}

// footer is the content right after the last list item (R-27, R-28): the Waiting Messages, then
// the Progress line while the agent works or a quit waits. During an ask prompt the prompt
// replaces the Progress line. Footer rows are not list items: no click, no selection.
func (m *model) footer() []string {
	rows := m.waitingRows()
	if m.ask == nil && (m.working() || m.quitting) {
		rows = append(rows, ansi.Truncate(m.progressLine(), m.width, "…"))
	}
	return rows
}

// contentTotal is the height of the whole content: the list rows and the footer rows.
func (m *model) contentTotal() int { return m.cv.total() + len(m.footer()) }

// listRows returns the visible rows of the content (fewer than h when it is short): list rows,
// then the footer where the window reaches it. While following, the footer is always shown
// under the last list rows. While not following, the last row is the "↓ N new rows" marker
// once rows have arrived. Head rows of tool blocks are registered as hover spots.
func (m *model) listRows(h int) []string {
	if h <= 0 {
		return nil
	}
	f := m.footer()
	if m.sc.follow {
		n := min(len(f), h)
		rows, _, _ := m.cv.tailRows(h - n)
		y0 := m.cv.total() - len(rows)
		m.highlight(rows, y0, -1)
		m.hotHeads(rows, y0, -1)
		return append(rows, f[len(f)-n:]...)
	}
	rows := m.cv.rows(m.viewTop(), h)
	if len(rows) < h { // the window reaches past the last item
		rows = append(rows, f[:min(len(f), h-len(rows))]...)
	}
	skip := -1
	if n := m.newRows(); n > 0 && len(rows) > 0 {
		rows[len(rows)-1] = noticeStyle.Render(newRowsMarker(n))
		skip = len(rows) - 1
	}
	m.highlight(rows, m.viewTop(), skip)
	m.hotHeads(rows, m.viewTop(), skip)
	return rows
}

// hotHeads registers every head row of a tool block among rows (list rows y0, y0+1, ...) as a
// hover spot "tool:<item>" (after the Task gutters of the block); the list starts at screen row 0. The marker row (skip) is none.
func (m *model) hotHeads(rows []string, y0 int, skip int) {
	for i, row := range rows {
		if i == skip {
			continue
		}
		if idx, _, head, ok := m.cv.rowAt(y0 + i); ok && head {
			m.hv.Add(fmt.Sprintf("tool:%d", idx), i, m.cv.items[idx].depth*gw, ansi.StringWidth(row))
		}
	}
}

// progressLine is the Progress line: "Thinking… (12s · ...)", the compaction state, or the quit hint.
func (m *model) progressLine() string {
	frame := glyphFrames[m.frame%len(glyphFrames)]
	switch {
	case m.quitting:
		return noticeStyle.Render(forceHint)
	case m.stopping:
		return frame + " Stopping… (esc again to abort)"
	}
	parts := []string{fmt.Sprintf("%ds", int(m.now.Sub(m.started).Seconds()))}
	if m.compacting && !m.busy {
		return fmt.Sprintf("%s %s (%s · esc to stop)", frame, compactingText, parts[0])
	}
	if m.progTotal > 0 && !m.gotText {
		parts = append(parts, fmt.Sprintf("reading prompt %d/%d", m.progDone, m.progTotal))
	}
	parts = append(parts, fmt.Sprintf("↓ ~%d tokens", chat.EstimateTokens(m.turn.String())))
	if v, ok := m.liveSpeed(); ok {
		parts = append(parts, "~"+fmtSpeed(v))
	}
	parts = append(parts, "esc to stop")
	text := spinText
	if w := m.spinnerWork(); w != "" {
		text = w
	}
	return fmt.Sprintf("%s %s (%s)", frame, text, strings.Join(parts, " · "))
}
