// Package menu is the one list engine behind every picker of the app (R-99).
package menu

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"swarg/internal/ui/hover"
	"swarg/internal/ui/mdstream"
)

// ---------------------------------------------------------------------------
// Menu: one list engine for every picker of the app (R-99). Rows, a cursor, a filter typed over
// the list, hover and click, the wheel, Esc, a footer with key hints or a confirmation. The host
// formats the rows and wires what happens: onChoose (Enter / click), onRight (the right-aligned
// label), extra hotkeys, the save scope (Tab) and a parent menu to return to. Standalone it is a
// small full-screen program (PickSession); embedded it takes the list area of the main UI.
// ---------------------------------------------------------------------------

// Row is one line of a menu.
type Row struct {
	ID    string
	Text  string // the row as drawn; the host lays out the columns
	Match string // what the filter searches; "" = Text
	Right string // a right-aligned clickable label (✕, on, off, approve); "" = none
	Dim   bool   // shown dimmed, skipped by the cursor, never chosen
}

// Action is what a key or mouse message made the menu decide.
type Action int

const (
	ActNone   Action = iota
	ActClose         // Esc / q / Ctrl+C: the menu closes (back to its parent), nothing chosen
	ActChosen        // a row was chosen: Chosen() holds its id
)

// WheelRows is the number of rows per wheel notch.
const WheelRows = 3

// BtnCancel is the cancel button of a confirmation.
const BtnCancel = "[n] cancel"

const (
	pickChrome   = 2 // header row + footer row
	menuKeysHint = "↑/↓ move   Enter choose   Esc close   type to filter"
	menuNoMatch  = "nothing matches   Backspace edit   Esc close"
)

var (
	pickHead = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.Accent))
	pickDim  = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.Dim))
	// Sel styles the selected row (the host's command suggestions use it too).
	Sel = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.UserFg)).
		Background(lipgloss.Color(mdstream.Colors.UserBg)).Bold(true)
)

// Options configures a menu; New copies what it needs. Everything but Title is optional.
type Options struct {
	Title string
	Rows  []Row
	Hint  string // the footer key hints; "" = the default
	Empty string // the footer when the menu has no rows at all; "" = the default

	OnChoose func(id string) bool       // Enter or a click on a row; true closes the menu (nil = close)
	OnRight  func(id string)            // a click on the row's Right label
	Keys     map[string]func(id string) // extra hotkeys ("ctrl+d", "space", "r", "left"); id = the selected row
	OnScope  func(scope string)         // the save scope changed (Tab); its presence marks a scope-aware menu

	// Clean makes typed text safe to draw (nil = as is); Fit adapts the standalone screen to the
	// terminal's colour profile (nil = as is). The host passes its own.
	Clean func(string) string
	Fit   func(string, colorprofile.Profile) string
}

// Menu is the list; it is also the Bubble Tea model of a standalone picker. Its working state
// (rows, cursor, filter, footer) is private: hosts act through the methods.
type Menu struct {
	items   []Row
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
	hv      *hover.Hover // where rows and buttons register; the host's when embedded
	own     hover.Hover

	onChoose func(id string) bool
	onRight  func(id string)
	keys     map[string]func(id string)
	scopes   []string           // the save scopes Tab cycles through; nil = none shown
	scope    string             // the current one, shown in the footer
	onScope  func(scope string) //
	parent   *Menu              // the menu Esc returns to; nil closes
	clean    func(string) string
	fit      func(string, colorprofile.Profile) string
}

// New builds a menu (80x24 until sized) from o.
func New(o Options) *Menu {
	m := &Menu{w: 80, h: 24, title: o.Title, hint: o.Hint, empty: o.Empty, keys: o.Keys,
		onChoose: o.OnChoose, onRight: o.OnRight, onScope: o.OnScope, clean: o.Clean, fit: o.Fit}
	if m.hint == "" {
		m.hint = menuKeysHint
	}
	if m.empty == "" {
		m.empty = "nothing here   Esc close"
	}
	if m.keys == nil {
		m.keys = map[string]func(string){}
	}
	m.hv = &m.own
	m.SetRows(o.Rows)
	return m
}

// ---------------------------------------------------------------------------
// Host-facing state
// ---------------------------------------------------------------------------

// Chosen is the id of the row the last ActChosen picked; TakeChosen reads and clears it.
func (m *Menu) Chosen() string { return m.chosen }
func (m *Menu) TakeChosen() string {
	id := m.chosen
	m.chosen = ""
	return id
}

// Choose runs the OnChoose callback for id; true (also without a callback) means close the menu.
func (m *Menu) Choose(id string) bool { return m.onChoose == nil || m.onChoose(id) }

// Note is the one-line result in the footer; SetNote shows text until the next key.
func (m *Menu) Note() string     { return m.note }
func (m *Menu) SetNote(s string) { m.note = s }

// Confirming reports a pending confirmation; Filter is the typed filter text.
func (m *Menu) Confirming() bool { return m.confirm }
func (m *Menu) Filter() string   { return m.filter }

// Parent is the menu Esc returns to (nil = close the menu bar); SetParent sets it.
func (m *Menu) Parent() *Menu     { return m.parent }
func (m *Menu) SetParent(p *Menu) { m.parent = p }

// SpotAt is the hover spot id under a cell of the last draw ("row:<id>", "right:<id>", "btn:yes").
func (m *Menu) SpotAt(x, y int) (string, bool) { return m.hv.At(x, y) }

// Attach makes rows and buttons register in the host's hover instead of the menu's own.
func (m *Menu) Attach(h *hover.Hover) { m.hv = h }

// Resize sets the drawing area and keeps the cursor inside the window.
func (m *Menu) Resize(w, h int) {
	m.w, m.h = w, h
	m.scroll()
}

// ScopeAware reports a menu that wants save scopes (OnScope set) and has none yet.
func (m *Menu) ScopeAware() bool { return m.scopes == nil && m.onScope != nil }

// SetScopes gives the scopes Tab cycles through and the current one.
func (m *Menu) SetScopes(scopes []string, current string) { m.scopes, m.scope = scopes, current }

// Scope is the current save scope ("" = none).
func (m *Menu) Scope() string { return m.scope }

// VisibleIDs are the ids of the rows the filter shows, in order; Top is the first visible position.
func (m *Menu) VisibleIDs() []string {
	ids := make([]string, len(m.vis))
	for i, ri := range m.vis {
		ids[i] = m.items[ri].ID
	}
	return ids
}
func (m *Menu) Top() int { return m.top }

// Cursor is the position of the selected row among the visible ones (-1 = none); Page is the
// number of list rows on screen.
func (m *Menu) Cursor() int { return m.cur }
func (m *Menu) Page() int   { return m.page() }

// SetRows replaces the rows and keeps the cursor on the same id when it is still there.
func (m *Menu) SetRows(items []Row) {
	keep := m.SelectedID()
	m.items = items
	m.refilter()
	if i := m.VisIndex(keep); i >= 0 {
		m.cur = i
		m.scroll()
	}
}

// SelectedID is the id of the row under the cursor, "" when none.
func (m *Menu) SelectedID() string {
	if m.cur < 0 || m.cur >= len(m.vis) {
		return ""
	}
	return m.items[m.vis[m.cur]].ID
}

// Confirm asks a yes/no question in the footer; y or the button runs yes.
func (m *Menu) Confirm(ask, yesLabel string, yes func()) {
	m.confirm, m.ask, m.yes, m.onYes, m.note = true, ask, yesLabel, yes, ""
}

// Init clears the screen, see model.Init.
func (m *Menu) Init() tea.Cmd { return tea.ClearScreen }

// ---------------------------------------------------------------------------
// Update (standalone): a decision ends the program
// ---------------------------------------------------------------------------

func (m *Menu) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.ColorProfileMsg:
		m.profile = msg.Profile
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.scroll()
	case tea.KeyPressMsg:
		return m, quitOn(m.Key(msg))
	case tea.MouseWheelMsg:
		m.Wheel(msg)
	case tea.MouseMsg:
		return m, quitOn(m.Mouse(msg))
	}
	return m, nil
}

// quitOn ends the standalone program on any decision.
func quitOn(a Action) tea.Cmd {
	if a == ActNone {
		return nil
	}
	return tea.Quit
}

// Key handles one key. A pending confirmation takes y (run) or any other key (cancel) first.
func (m *Menu) Key(msg tea.KeyPressMsg) Action {
	if m.confirm {
		m.confirm = false
		if t := msg.Key().Text; t == "y" || t == "Y" {
			m.onYes()
		}
		return ActNone
	}
	m.note = "" // the last result stays only until the next key
	s := msg.String()
	switch s {
	case "ctrl+c", "esc":
		return ActClose
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
		typed := t != "" && msg.Key().Mod&(tea.ModCtrl|tea.ModAlt) == 0
		command := !typed || (!m.typing && m.filter == "")
		if command {
			if f := m.keys[s]; f != nil {
				f(m.SelectedID())
				return ActNone
			}
			if t != "" && !m.typing && m.filter == "" {
				switch t {
				case "q":
					return ActClose
				case "k":
					m.move(-1)
					return ActNone
				case "j":
					m.move(1)
					return ActNone
				case "/":
					m.typing = true
					return ActNone
				}
			}
		}
		if !typed {
			break
		}
		m.typing = true
		m.filter += t
		m.refilter()
	}
	return ActNone
}

// cycleScope moves to the next save scope (Tab) and tells the host.
func (m *Menu) cycleScope() {
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

// Mouse handles motion (hover), clicks on the spots of the last draw (a row chooses, its Right
// label runs onRight, the confirm buttons answer) and ignores the rest.
func (m *Menu) Mouse(msg tea.MouseMsg) Action {
	mm := msg.Mouse()
	switch msg.(type) {
	case tea.MouseMotionMsg:
		if mm.Button == tea.MouseNone {
			m.hv.Move(mm.X, mm.Y)
		}
	case tea.MouseClickMsg:
		m.hv.Move(mm.X, mm.Y)
		if mm.Button != tea.MouseLeft {
			return ActNone
		}
		id, ok := m.hv.At(mm.X, mm.Y)
		if !ok {
			return ActNone
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
			if i := m.VisIndex(arg); i >= 0 {
				m.cur = i
				m.scroll()
				if m.onRight != nil {
					m.onRight(arg)
				}
			}
		case "row":
			m.confirm = false
			if i := m.VisIndex(arg); i >= 0 && m.selectable(i) {
				m.cur = i
				return m.choose()
			}
		}
	}
	return ActNone
}

// Wheel scrolls the window by three rows per notch.
func (m *Menu) Wheel(msg tea.MouseWheelMsg) {
	switch msg.Button {
	case tea.MouseWheelUp:
		m.top = max(m.top-WheelRows, 0)
	case tea.MouseWheelDown:
		m.top = max(min(m.top+WheelRows, len(m.vis)-m.page()), 0)
	}
}

// choose records the selected row and asks the host; nothing happens without a selectable row.
func (m *Menu) choose() Action {
	if m.cur < 0 || !m.selectable(m.cur) {
		return ActNone
	}
	m.chosen = m.items[m.vis[m.cur]].ID
	return ActChosen
}

// ---------------------------------------------------------------------------
// Selection and filter
// ---------------------------------------------------------------------------

// page is the number of list rows on screen.
func (m *Menu) page() int { return max(m.h-pickChrome, 1) }

func (m *Menu) selectable(i int) bool {
	return i >= 0 && i < len(m.vis) && !m.items[m.vis[i]].Dim
}

// VisIndex is the vis position of the row with id, -1 when it is not shown.
func (m *Menu) VisIndex(id string) int {
	for i, ri := range m.vis {
		if m.items[ri].ID == id {
			return i
		}
	}
	return -1
}

// move steps to the next selectable row in direction dir (+1/-1); at the end it stays.
func (m *Menu) move(dir int) {
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
func (m *Menu) jump(n int) {
	if len(m.vis) == 0 {
		return
	}
	dir := 1
	if n < 0 {
		dir = -1
	}
	m.selectNearest(max(min(m.cur+n, len(m.vis)-1), 0), dir, -dir)
}

// SelectNear puts the cursor on the selectable row nearest to vis position pos (at or before it
// first, then after).
func (m *Menu) SelectNear(pos int) {
	m.selectNearest(min(pos, len(m.vis)-1), -1, 1)
}

// selectNearest selects the first selectable row scanning from t in each direction of dirs in turn.
func (m *Menu) selectNearest(t int, dirs ...int) {
	for _, d := range dirs {
		for i := t; i >= 0 && i < len(m.vis); i += d {
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
func (m *Menu) refilter() {
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
func (m *Menu) scroll() {
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
func (m *Menu) View() tea.View {
	m.hv.Begin()
	lines := m.Lines()
	m.hv.Apply(lines)
	v := tea.NewView(m.fitted(strings.Join(lines, "\n")))
	v.AltScreen, v.MouseMode = true, tea.MouseModeAllMotion
	return v
}

// Lines draws the menu into at most m.h rows of width m.w: header, rows, filler, footer. Every
// selectable row registers the hover spot row:<id> (its text), a row with a Right label also
// right:<id> (the label at the end), and a pending confirmation registers btn:yes / btn:no on
// the footer - only for rows that fit (a cut row must not leave a click target behind). The
// host runs hover.begin before and applyHover after.
func (m *Menu) Lines() []string {
	lines := make([]string, 0, m.h)
	var spots []hover.Spot
	head := m.title
	if m.filter != "" || m.typing {
		head += "   filter: " + m.cleanText(m.filter) + "_"
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
		spots = append(spots, hover.Spot{ID: "row:" + r.ID, Y: y, X0: 0, X1: lipgloss.Width(line)})
		if rw > 0 {
			line += strings.Repeat(" ", max(bw-lipgloss.Width(line), 0)) + "  " + r.Right
			spots = append(spots, hover.Spot{ID: "right:" + r.ID, Y: y, X0: max(m.w-rw-2, 0), X1: m.w}) // the label and its two blanks
		}
		if i == m.cur {
			line = Sel.Render(line + strings.Repeat(" ", max(m.w-lipgloss.Width(line), 0)))
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
		if s.Y < len(lines) {
			m.hv.Add(s.ID, s.Y, s.X0, s.X1)
		}
	}
	return lines
}

// footer is the last row: the key hints (and the scope), a pending confirmation with its
// buttons, or the last note. y is its screen row; the button spots are returned, not registered.
func (m *Menu) footer(y int) (string, []hover.Spot) {
	var btns []hover.Spot
	foot := m.hint
	if len(m.scopes) > 0 {
		foot += "   Tab scope: " + m.scope
	}
	switch {
	case m.confirm:
		foot = m.ask + "   " + m.yes + "   " + BtnCancel
		x := lipgloss.Width(m.ask) + 3
		btns = append(btns, hover.Spot{ID: "btn:yes", Y: y, X0: x, X1: x + lipgloss.Width(m.yes)})
		x += lipgloss.Width(m.yes) + 3
		btns = append(btns, hover.Spot{ID: "btn:no", Y: y, X0: x, X1: x + lipgloss.Width(BtnCancel)})
	case m.note != "":
		foot = m.note + "   (" + m.hint + ")"
	case len(m.vis) == 0 && len(m.items) == 0:
		foot = m.empty
	case len(m.vis) == 0:
		foot = menuNoMatch
	}
	return pickDim.Render(ansi.Truncate(foot, m.w, "…")), btns
}

// cleanText runs the host's cleaner over typed text.
func (m *Menu) cleanText(s string) string {
	if m.clean == nil {
		return s
	}
	return m.clean(s)
}

// fitted adapts the standalone screen to the colour profile.
func (m *Menu) fitted(s string) string {
	if m.fit == nil {
		return s
	}
	return m.fit(s, m.profile)
}
