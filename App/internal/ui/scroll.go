package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Scrolling the conversation list
// ---------------------------------------------------------------------------

const wheelRows = 3 // rows per wheel notch

// scrollState is the viewport position. While follow is set the view sits at the bottom and
// stays there as rows arrive. Otherwise the top row is (aItem, aRow): a row inside an item,
// so rows added or re-wrapped elsewhere do not move what the user reads. base is the list
// height when following stopped; rows beyond it are "new".
type scrollState struct {
	follow      bool
	aItem, aRow int
	base        int
}

// maxOffset is the largest first row of the viewport (the content ends with the footer).
func (m *model) maxOffset() int { return max(m.contentTotal()-m.listHeight(), 0) }

// viewTop is the list row at the top of the viewport.
func (m *model) viewTop() int {
	mx := m.maxOffset()
	if m.sc.follow {
		return mx
	}
	if m.sc.aItem >= m.cv.count() {
		return mx
	}
	row := min(m.sc.aRow, max(m.cv.height(m.sc.aItem)-1, 0))
	return min(m.cv.top(m.sc.aItem)+row, mx)
}

// rowVisible reports whether list row y is inside the viewport.
func (m *model) rowVisible(y int) bool {
	top := m.viewTop()
	return y >= top && y < top+m.listHeight()
}

// scrollTo puts list row y at the top of the viewport (clamped).
func (m *model) scrollTo(y int) {
	total := m.cv.total()
	mx := m.maxOffset()
	y = max(y, 0)
	if y >= mx {
		m.sc.follow = true
		return
	}
	idx, row, _, ok := m.cv.rowAt(y)
	if !ok {
		m.sc.follow = true
		return
	}
	if m.sc.follow {
		m.sc.base = total
	}
	m.sc.follow, m.sc.aItem, m.sc.aRow = false, idx, row
}

// scrollBy moves the viewport by d rows (negative = up).
func (m *model) scrollBy(d int) { m.scrollTo(m.viewTop() + d) }

// pageRows is the PgUp/PgDn step: a page minus one row of context.
func (m *model) pageRows() int { return max(m.listHeight()-1, 1) }

// settle runs after every Update: a viewport pushed past the end (the list shrank) follows again.
func (m *model) settle() {
	if !m.sc.follow && m.viewTop() >= m.maxOffset() {
		m.sc.follow = true
	}
}

// onWheel scrolls the list by wheelRows per notch.
func (m *model) onWheel(msg tea.MouseWheelMsg) {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		m.scrollBy(-wheelRows)
	case tea.MouseWheelDown:
		m.scrollBy(wheelRows)
	}
}

// scrollKey handles PgUp/PgDn/Home/End. Home and End belong to the textarea unless the
// input is empty; the ctrl variants always scroll. It reports whether the key was taken.
func (m *model) scrollKey(key string) bool {
	switch key {
	case "pgup":
		m.scrollBy(-m.pageRows())
	case "pgdown":
		m.scrollBy(m.pageRows())
	case "ctrl+home":
		m.scrollTo(0)
	case "ctrl+end":
		m.sc.follow = true
	case "home", "end":
		if m.ta.Value() != "" {
			return false
		}
		return m.scrollKey("ctrl+" + key)
	default:
		return false
	}
	return true
}

// newRows is how many rows were added to the list since following stopped.
func (m *model) newRows() int {
	if m.sc.follow {
		return 0
	}
	return max(m.cv.total()-m.sc.base, 0)
}

// newRowsMarker is the text of the marker row shown while not following.
func newRowsMarker(n int) string { return fmt.Sprintf("↓ %d new rows", n) }
