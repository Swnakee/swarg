package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"swarg/internal/ui/mdstream"
)

// ---------------------------------------------------------------------------
// Menu: one list engine for every picker of the app (R-99). Rows, a cursor, a filter typed over
// the list, hover and click, the wheel, Esc, a footer with key hints or a confirmation. The host
// formats the rows and wires what happens: onChoose (Enter / click), onRight (the right-aligned
// label), extra hotkeys, the save scope (Tab) and a parent menu to return to. Standalone it is a
// small full-screen program (PickSession); embedded it takes the list area of the main UI.
// ---------------------------------------------------------------------------

// menuRow is one line of a menu.
type menuRow struct {
	ID    string
	Text  string // the row as drawn; the host lays out the columns
	Match string // what the filter searches; "" = Text
	Right string // a right-aligned clickable label (✕, on, off, approve); "" = none
	Dim   bool   // shown dimmed, skipped by the cursor, never chosen
}

// pickAction is what a key or mouse message made the menu decide.
type pickAction int

const (
	pickNone   pickAction = iota
	pickClose             // Esc / q / Ctrl+C: the menu closes (back to its parent), nothing chosen
	pickChosen            // a row was chosen: menu.chosen holds its id
)

const (
	pickChrome   = 2 // header row + footer row
	btnCancel    = "[n] cancel"
	menuKeysHint = "↑/↓ move   Enter choose   Esc close   type to filter"
	menuNoMatch  = "nothing matches   Backspace edit   Esc close"
)

var (
	pickHead = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.Accent))
	pickDim  = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.Dim))
	pickSel  = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.UserFg)).
			Background(lipgloss.Color(mdstream.Colors.UserBg)).Bold(true)
)

// menu is the list; it is also the Bubble Tea model of a standalone picker.
type menu struct {
	items   []menuRow
	vis     []int // indexes into items that match the filter, in order
	cur     int   // index into vis, -1 when nothing is selectable
	top     int   // first visible position in vis
	filter  string
	typing  bool // all printable keys go to the filter (hotkey letters are commands until then)
	w, h    int
	title   string
	hint    string // the footer key hints
	empty   string // the footer when the menu has no rows at all
	chosen  string
	confirm bool   // a confirmation is pending: ask + yesLabel in the footer, y / the button runs onYes
	ask     string //
	yes     string //
	onYes   func() //
	note    string // a one-line result of the last action (error or "saved")
	profile colorprofile.Profile
	hv      *hover // where rows and buttons register; the host's when embedded
	own     hover

	onChoose func(id string) bool       // Enter or a click on a row; true closes the menu
	onRight  func(id string)            // a click on the row's Right label
	keys     map[string]func(id string) // extra hotkeys ("ctrl+d", "space", "r", "left"); id = the selected row
	scopes   []string                   // the save scopes Tab cycles through; nil = none shown
	scope    string                     // the current one, shown in the footer
	onScope  func(scope string)         //
	parent   *menu                      // the menu Esc returns to; nil closes
}

func newMenu(title string, items []menuRow) *menu {
	m := &menu{w: 80, h: 24, title: title, hint: menuKeysHint, empty: "nothing here   Esc close", keys: map[string]func(string){}}
	m.hv = &m.own
	m.setItems(items)
	return m
}

// setItems replaces the rows and keeps the cursor on the same id when it is still there.
func (m *menu) setItems(items []menuRow) {
	keep := m.selectedID()
	m.items = items
	m.refilter()
	if i := m.visIndex(keep); i >= 0 {
		m.cur = i
		m.scroll()
	}
}

// selectedID is the id of the row under the cursor, "" when none.
func (m *menu) selectedID() string {
	if m.cur < 0 || m.cur >= len(m.vis) {
		return ""
	}
	return m.items[m.vis[m.cur]].ID
}

// confirmWith asks a yes/no question in the footer; y or the button runs yes.
func (m *menu) confirmWith(ask, yesLabel string, yes func()) {
	m.confirm, m.ask, m.yes, m.onYes, m.note = true, ask, yesLabel, yes, ""
}

// Init clears the screen, see model.Init.
func (m *menu) Init() tea.Cmd { return tea.ClearScreen }

// ---------------------------------------------------------------------------
// Update (standalone): a decision ends the program
// ---------------------------------------------------------------------------

func (m *menu) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.ColorProfileMsg:
		m.profile = msg.Profile
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.scroll()
	case tea.KeyPressMsg:
		return m, quitOn(m.key(msg))
	case tea.MouseWheelMsg:
		m.wheel(msg)
	case tea.MouseMsg:
		return m, quitOn(m.mouse(msg))
	}
	return m, nil
}

// quitOn ends the standalone program on any decision.
func quitOn(a pickAction) tea.Cmd {
	if a == pickNone {
		return nil
	}
	return tea.Quit
}

// key handles one key. A pending confirmation takes y (run) or any other key (cancel) first.
func (m *menu) key(msg tea.KeyPressMsg) pickAction {
	if m.confirm {
		m.confirm = false
		if t := msg.Key().Text; t == "y" || t == "Y" {
			m.onYes()
		}
		return pickNone
	}
	m.note = "" // the last result stays only until the next key
	s := msg.String()
	switch s {
	case "ctrl+c", "esc":
		return pickClose
	case "enter":
		return m.choose()
	case "up", "ctrl+p":
		m.move(-1)
	case "down", "ctrl+n":
		m.move(1)
	case "pgup":
		m.jump(-m.page())
	case "pgdown":
		m.jump(m.page())
	case "home":
		m.cur = -1
		m.move(1)
	case "end":
		m.cur = len(m.vis)
		m.move(-1)
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
			m.refilter()
		}
	case "ctrl+u":
		m.filter, m.typing = "", false
		m.refilter()
	case "tab":
		m.cycleScope()
	default:
		t := msg.Key().Text
		command := t == "" || msg.Key().Mod&(tea.ModCtrl|tea.ModAlt) != 0 || (!m.typing && m.filter == "")
		if command {
			if f := m.keys[s]; f != nil {
				f(m.selectedID())
				return pickNone
			}
			if t != "" && !m.typing && m.filter == "" {
				switch t {
				case "q":
					return pickClose
				case "k":
					m.move(-1)
					return pickNone
				case "j":
					m.move(1)
					return pickNone
				case "/":
					m.typing = true
					return pickNone
				}
			}
		}
		if t == "" || msg.Key().Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
			break
		}
		m.typing = true
		m.filter += t
		m.refilter()
	}
	return pickNone
}

// cycleScope moves to the next save scope (Tab) and tells the host.
func (m *menu) cycleScope() {
	if len(m.scopes) == 0 {
		return
	}
	for i, s := range m.scopes {
		if s == m.scope {
			m.scope = m.scopes[(i+1)%len(m.scopes)]
			if m.onScope != nil {
				m.onScope(m.scope)
			}
			return
		}
	}
	m.scope = m.scopes[0]
}

// mouse handles motion (hover), clicks on the spots of the last draw (a row chooses, its Right
// label runs onRight, the confirm buttons answer) and ignores the rest.
func (m *menu) mouse(msg tea.MouseMsg) pickAction {
	mm := msg.Mouse()
	switch msg.(type) {
	case tea.MouseMotionMsg:
		if mm.Button == tea.MouseNone {
			m.hv.move(mm.X, mm.Y)
		}
	case tea.MouseClickMsg:
		m.hv.move(mm.X, mm.Y)
		if mm.Button != tea.MouseLeft {
			return pickNone
		}
		id, ok := m.hv.at(mm.X, mm.Y)
		if !ok {
			return pickNone
		}
		kind, arg, _ := strings.Cut(id, ":")
		switch kind {
		case "btn":
			if m.confirm {
				m.confirm = false
				if arg == "yes" {
					m.onYes()
				}
			}
		case "right":
			if i := m.visIndex(arg); i >= 0 {
				m.cur = i
				m.scroll()
				if m.onRight != nil {
					m.onRight(arg)
				}
			}
		case "row":
			m.confirm = false
			if i := m.visIndex(arg); i >= 0 && m.selectable(i) {
				m.cur = i
				return m.choose()
			}
		}
	}
	return pickNone
}

// wheel scrolls the window by three rows per notch.
func (m *menu) wheel(msg tea.MouseWheelMsg) {
	switch msg.Button {
	case tea.MouseWheelUp:
		m.top = max(m.top-wheelRows, 0)
	case tea.MouseWheelDown:
		m.top = max(min(m.top+wheelRows, len(m.vis)-m.page()), 0)
	}
}

// choose records the selected row and asks the host; nothing happens without a selectable row.
func (m *menu) choose() pickAction {
	if m.cur < 0 || !m.selectable(m.cur) {
		return pickNone
	}
	m.chosen = m.items[m.vis[m.cur]].ID
	return pickChosen
}

// ---------------------------------------------------------------------------
// Selection and filter
// ---------------------------------------------------------------------------

// page is the number of list rows on screen.
func (m *menu) page() int { return max(m.h-pickChrome, 1) }

func (m *menu) selectable(i int) bool {
	return i >= 0 && i < len(m.vis) && !m.items[m.vis[i]].Dim
}

// visIndex is the vis position of the row with id, -1 when it is not shown.
func (m *menu) visIndex(id string) int {
	for i, ri := range m.vis {
		if m.items[ri].ID == id {
			return i
		}
	}
	return -1
}

// move steps to the next selectable row in direction dir (+1/-1); at the end it stays.
func (m *menu) move(dir int) {
	for i := m.cur + dir; i >= 0 && i < len(m.vis); i += dir {
		if m.selectable(i) {
			m.cur = i
			break
		}
	}
	m.scroll()
}

// jump moves by n rows, then to the nearest selectable row (in the move direction first,
// else back); without any it stays.
func (m *menu) jump(n int) {
	if len(m.vis) == 0 {
		return
	}
	dir := 1
	if n < 0 {
		dir = -1
	}
	t := max(min(m.cur+n, len(m.vis)-1), 0)
	for _, d := range []int{dir, -dir} {
		for i := t; i >= 0 && i < len(m.vis); i += d {
			if m.selectable(i) {
				m.cur = i
				m.scroll()
				return
			}
		}
	}
}

// selectNear puts the cursor on the selectable row nearest to vis position pos (at or before it
// first, then after).
func (m *menu) selectNear(pos int) {
	pos = min(pos, len(m.vis)-1)
	for _, d := range []int{-1, 1} {
		for i := pos; i >= 0 && i < len(m.vis); i += d {
			if m.selectable(i) {
				m.cur = i
				m.scroll()
				return
			}
		}
	}
}

// refilter rebuilds vis for the current filter and selects its first selectable row. Dim rows
// match nothing but the empty filter.
func (m *menu) refilter() {
	q := strings.ToLower(m.filter)
	m.vis = m.vis[:0]
	for i, r := range m.items {
		match := r.Match
		if match == "" && !r.Dim {
			match = r.Text
		}
		if q == "" || strings.Contains(strings.ToLower(match), q) {
			m.vis = append(m.vis, i)
		}
	}
	m.cur, m.top = -1, 0
	m.move(1)
}

// scroll keeps the selected row inside the window.
func (m *menu) scroll() {
	if m.cur >= 0 {
		if m.cur < m.top {
			m.top = m.cur
		}
		if m.cur >= m.top+m.page() {
			m.top = m.cur - m.page() + 1
		}
	}
	m.top = max(min(m.top, len(m.vis)-m.page()), 0)
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// View is the standalone screen: the lines with the hover applied.
func (m *menu) View() tea.View {
	m.hv.begin()
	lines := m.lines()
	applyHover(m.hv, lines)
	v := tea.NewView(fitProfile(strings.Join(lines, "\n"), m.profile))
	v.AltScreen, v.MouseMode = true, tea.MouseModeAllMotion
	return v
}

// lines draws the menu into at most m.h rows of width m.w: header, rows, filler, footer. Every
// selectable row registers the hover spot row:<id> (its text), a row with a Right label also
// right:<id> (the label at the end), and a pending confirmation registers btn:yes / btn:no on
// the footer - only for rows that fit (a cut row must not leave a click target behind). The
// host runs hover.begin before and applyHover after.
func (m *menu) lines() []string {
	lines := make([]string, 0, m.h)
	var spots []hotspot
	head := m.title
	if m.filter != "" || m.typing {
		head += "   filter: " + Clean(m.filter) + "_"
	}
	lines = append(lines, pickHead.Render(ansi.Truncate(head, m.w, "")))

	end := min(m.top+m.page(), len(m.vis))
	for i := m.top; i < end; i++ {
		r := m.items[m.vis[i]]
		y := len(lines)
		if r.Dim {
			lines = append(lines, pickDim.Render(ansi.Truncate(r.Text, m.w, "…")))
			continue
		}
		bw := m.w // cells for the text; a Right label keeps its width plus two blanks at the end
		rw := lipgloss.Width(r.Right)
		if rw > 0 {
			bw = max(m.w-rw-2, 1)
		}
		line := ansi.Truncate(r.Text, bw, "…")
		spots = append(spots, hotspot{"row:" + r.ID, y, 0, lipgloss.Width(line)})
		if rw > 0 {
			line += strings.Repeat(" ", max(bw-lipgloss.Width(line), 0)) + "  " + r.Right
			spots = append(spots, hotspot{"right:" + r.ID, y, max(m.w-rw-2, 0), m.w}) // the label and its two blanks
		}
		if i == m.cur {
			line = pickSel.Render(line + strings.Repeat(" ", max(m.w-lipgloss.Width(line), 0)))
		}
		lines = append(lines, line)
	}
	for len(lines) < m.h-1 {
		lines = append(lines, "")
	}
	foot, btns := m.footer(len(lines))
	lines = append(lines, foot)
	spots = append(spots, btns...)
	if len(lines) > m.h {
		lines = lines[:max(m.h, 0)]
	}
	for _, s := range spots {
		if s.y < len(lines) {
			m.hv.add(s.id, s.y, s.x0, s.x1)
		}
	}
	return lines
}

// footer is the last row: the key hints (and the scope), a pending confirmation with its
// buttons, or the last note. y is its screen row; the button spots are returned, not registered.
func (m *menu) footer(y int) (string, []hotspot) {
	var btns []hotspot
	foot := m.hint
	if len(m.scopes) > 0 {
		foot += "   Tab scope: " + m.scope
	}
	switch {
	case m.confirm:
		foot = m.ask + "   " + m.yes + "   " + btnCancel
		x := lipgloss.Width(m.ask) + 3
		btns = append(btns, hotspot{"btn:yes", y, x, x + lipgloss.Width(m.yes)})
		x += lipgloss.Width(m.yes) + 3
		btns = append(btns, hotspot{"btn:no", y, x, x + lipgloss.Width(btnCancel)})
	case m.note != "":
		foot = m.note + "   (" + m.hint + ")"
	case len(m.vis) == 0 && len(m.items) == 0:
		foot = m.empty
	case len(m.vis) == 0:
		foot = menuNoMatch
	}
	return pickDim.Render(ansi.Truncate(foot, m.w, "…")), btns
}
