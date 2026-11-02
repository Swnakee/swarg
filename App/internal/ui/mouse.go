package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Mouse: click on a block head, drag to select, release to copy
// ---------------------------------------------------------------------------

// press is the left button held down: where it went down and what was under it.
type press struct {
	down     bool
	x, y     int      // screen cell of the press
	pt       selPoint // the list cell under it (valid when onList)
	onList   bool
	head     bool   // the press was on a head row of a tool block
	panel    string // hover id of the Bottom panel item under the press ("" = none)
	dragging bool   // the pointer left the press cell while the button was held
}

// onMouse handles clicks, drags, releases and pointer motion (the wheel is handled by onWheel).
// Motion without a button only moves the hover pointer: nothing but the look changes (I-10).
func (m *model) onMouse(msg tea.MouseMsg) tea.Cmd {
	mm := msg.Mouse()
	switch msg.(type) {
	case tea.MouseClickMsg:
		m.hv.Move(mm.X, mm.Y)
		m.onPress(mm)
	case tea.MouseMotionMsg:
		if mm.Button == tea.MouseNone {
			m.hv.Move(mm.X, mm.Y)
			return nil
		}
		m.onDrag(mm)
	case tea.MouseReleaseMsg:
		return m.onRelease(mm)
	}
	return nil
}

// clickSpot dispatches a click on a registered hotspot outside the list (the spots of the
// last draw): a command suggestion completes. Other kinds are added here.
func (m *model) clickSpot(id string) {
	kind, arg, _ := strings.Cut(id, ":")
	if kind == "cmd" {
		if i, err := strconv.Atoi(arg); err == nil {
			m.completeSuggestion(i)
		}
	}
}

// clearSel drops the selection and the "copied" note.
func (m *model) clearSel() {
	m.sel, m.pr, m.copyNote = selection{}, press{}, ""
}

// listPoint maps a screen cell to the list cell under it; ok is false outside the list rows.
func (m *model) listPoint(x, y int) (pt selPoint, head, ok bool) {
	if y < 0 || y >= m.listHeight() || m.tooSmall() {
		return pt, false, false
	}
	if n := m.newRows(); n > 0 && y == m.listHeight()-1 { // the marker row is not content
		return pt, false, false
	}
	idx, row, head, ok := m.cv.rowAt(m.viewTop() + y)
	return selPoint{idx, row, max(x, 0)}, head, ok
}

func (m *model) onPress(mm tea.Mouse) {
	m.clearSel()
	if mm.Button != tea.MouseLeft {
		return
	}
	pt, head, ok := m.listPoint(mm.X, mm.Y)
	m.pr = press{down: true, x: mm.X, y: mm.Y, pt: pt, onList: ok, head: head && ok}
	if id, ok := m.hv.At(mm.X, mm.Y); ok && strings.HasPrefix(id, "panel:") {
		m.pr.panel = id
	}
}

// onDrag extends the selection to the cell under the pointer. At the top or bottom row of the
// list it also scrolls one row per event.
func (m *model) onDrag(mm tea.Mouse) {
	if !m.pr.down || !m.pr.onList || mm.Button != tea.MouseLeft {
		return
	}
	if !m.pr.dragging && mm.X == m.pr.x && mm.Y == m.pr.y {
		return
	}
	m.pr.dragging = true
	m.hv.Move(mm.X, mm.Y)
	h := m.listHeight()
	y := min(max(mm.Y, 0), max(h-1, 0))
	switch {
	case mm.Y <= 0:
		m.scrollBy(-1)
	case mm.Y >= h-1:
		m.scrollBy(1)
	}
	total := m.cv.total()
	if total == 0 {
		return
	}
	idx, row, _, ok := m.cv.rowAt(min(m.viewTop()+y, total-1))
	if !ok {
		return
	}
	m.sel = selection{active: true, a: m.pr.pt, b: selPoint{idx, row, max(mm.X, 0)}}
}

// onRelease ends a press. After a drag the selection is copied; a release on the press cell
// without a drag is a click: it toggles a tool block when its head was hit, else it goes to
// the hotspot under the press (a command suggestion, a panel item).
func (m *model) onRelease(mm tea.Mouse) tea.Cmd {
	p := m.pr
	if !p.down {
		return nil
	}
	m.hv.Move(mm.X, mm.Y)
	if !p.dragging && (mm.X != p.x || mm.Y != p.y) { // the button moved with no motion event
		mm.Button = tea.MouseLeft
		m.onDrag(mm)
	}
	p = m.pr
	m.pr.down = false
	if !p.dragging {
		switch {
		case p.head:
			m.cv.toggle(p.pt.item)
		case !p.onList:
			if id, ok := m.hv.At(p.x, p.y); ok { // the spot under the press, like the panel
				m.clickSpot(id)
			}
		}
		if p.panel != "" && mm.X == p.x && mm.Y == p.y {
			return m.panelClick(p.panel)
		}
		return nil
	}
	text, n := m.selectionText()
	if text == "" {
		m.sel = selection{}
		return nil
	}
	m.copyNote = fmt.Sprintf("copied %d lines", n)
	if n == 1 {
		m.copyNote = "copied 1 line"
	}
	return tea.SetClipboard(text)
}
