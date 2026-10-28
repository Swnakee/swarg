package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"swarg/internal/ui/mdstream"
)

// ---------------------------------------------------------------------------
// Session picker: a list of saved Sessions. Standalone it is the small full-screen program
// shown BEFORE the main UI (swarg -r); embedded it takes the list area of the main UI
// (/sessions, /resume). One component, two hosts: the host owns the size, the hover and
// what happens when a row is chosen.
// ---------------------------------------------------------------------------

// PickRow is one saved session in the picker. A non-empty Err marks an unreadable file:
// it is shown dimmed and cannot be chosen.
type PickRow struct {
	ID, Title, Model string
	Time             time.Time
	Messages         int
	Err              string
}

// pickAction is what a key or mouse message made the picker decide.
type pickAction int

const (
	pickNone   pickAction = iota
	pickClose             // Esc / q / Ctrl+C: the picker closes, nothing chosen
	pickChosen            // a row was chosen: picker.chosen holds its id
)

const (
	pickTimeFmt       = "2006-01-02 15:04"
	pickModelMax      = 28
	pickChrome        = 2 // header row + footer row
	pickDelete        = "✕"
	pickCurrent       = "(current)"
	btnDelete         = "[y] delete"
	btnCancel         = "[n] cancel"
	pickKeysHint      = "↑/↓ move   Enter open   Ctrl+D delete   Esc cancel   type to filter"
	errCurrentSession = "the current session cannot be deleted" // the same text as main's SessionsAPI.Delete
)

var (
	pickHead = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.Accent))
	pickDim  = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.Dim))
	pickSel  = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.UserFg)).
			Background(lipgloss.Color(mdstream.Colors.UserBg)).Bold(true)
)

// PickSession shows the picker and returns the chosen session id. ok is false when the user
// cancelled (or there was nothing to choose).
func PickSession(rows []PickRow, del func(id string) error) (id string, ok bool, err error) {
	if len(rows) == 0 {
		return "", false, nil
	}
	p := newPicker(rows)
	p.del = del
	final, err := tea.NewProgram(p).Run()
	if err != nil {
		return "", false, err
	}
	pk := final.(*picker)
	return pk.chosen, pk.chosen != "", nil
}

// picker is the list; it is also the Bubble Tea model of PickSession.
type picker struct {
	rows    []PickRow
	vis     []int // indexes into rows that match the filter, in order
	cur     int   // index into vis, -1 when nothing is selectable
	top     int   // first visible position in vis
	filter  string
	typing  bool // all printable keys go to the filter (j/k/q are commands until then)
	w, h    int
	title   string
	chosen  string
	current string                // id of the Session being written: marked, never deleted
	del     func(id string) error // delete a session by id; nil disables deletion
	confirm bool                  // a delete confirmation is pending for the current row
	note    string                // a one-line result of the last action (error or "deleted")
	profile colorprofile.Profile  // the terminal's colour profile, Unknown until reported
	hv      *hover                // where rows and buttons register; the host's when embedded
	own     hover
}

func newPicker(rows []PickRow) *picker {
	p := &picker{rows: rows, w: 80, h: 24, title: "Resume a session"}
	p.hv = &p.own
	p.refilter()
	return p
}

// Init clears the screen, see model.Init.
func (p *picker) Init() tea.Cmd { return tea.ClearScreen }

// ---------------------------------------------------------------------------
// Update (standalone): a decision ends the program
// ---------------------------------------------------------------------------

func (p *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.ColorProfileMsg:
		p.profile = msg.Profile
	case tea.WindowSizeMsg:
		p.w, p.h = msg.Width, msg.Height
		p.scroll()
	case tea.KeyPressMsg:
		return p, quitOn(p.key(msg))
	case tea.MouseWheelMsg:
		p.wheel(msg)
	case tea.MouseMsg:
		return p, quitOn(p.mouse(msg))
	}
	return p, nil
}

// quitOn ends the standalone program on any decision.
func quitOn(a pickAction) tea.Cmd {
	if a == pickNone {
		return nil
	}
	return tea.Quit
}

// key handles one key. A pending delete confirmation takes y (delete) or any other key
// (cancel) first.
func (p *picker) key(msg tea.KeyPressMsg) pickAction {
	if p.confirm {
		p.confirm = false
		if t := msg.Key().Text; t == "y" || t == "Y" {
			p.deleteCurrent()
		}
		return pickNone
	}
	p.note = "" // the last result stays only until the next key
	switch s := msg.String(); s {
	case "ctrl+c", "esc":
		return pickClose
	case "ctrl+d":
		p.askDelete()
	case "enter":
		return p.choose()
	case "up", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "pgup":
		p.jump(-p.page())
	case "pgdown":
		p.jump(p.page())
	case "home":
		p.cur = -1
		p.move(1)
	case "end":
		p.cur = len(p.vis)
		p.move(-1)
	case "backspace":
		if r := []rune(p.filter); len(r) > 0 {
			p.filter = string(r[:len(r)-1])
			p.refilter()
		}
	case "ctrl+u":
		p.filter, p.typing = "", false
		p.refilter()
	default:
		t := msg.Key().Text
		if t == "" || msg.Key().Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
			break
		}
		if !p.typing && p.filter == "" {
			switch t {
			case "q":
				return pickClose
			case "k":
				p.move(-1)
				return pickNone
			case "j":
				p.move(1)
				return pickNone
			case "/":
				p.typing = true
				return pickNone
			}
		}
		p.typing = true
		p.filter += t
		p.refilter()
	}
	return pickNone
}

// mouse handles motion (hover), clicks on the spots of the last draw (a row opens, its ✕ asks
// to delete, the confirm buttons answer) and ignores the rest.
func (p *picker) mouse(msg tea.MouseMsg) pickAction {
	mm := msg.Mouse()
	switch msg.(type) {
	case tea.MouseMotionMsg:
		if mm.Button == tea.MouseNone {
			p.hv.move(mm.X, mm.Y)
		}
	case tea.MouseClickMsg:
		p.hv.move(mm.X, mm.Y)
		if mm.Button != tea.MouseLeft {
			return pickNone
		}
		id, ok := p.hv.at(mm.X, mm.Y)
		if !ok {
			return pickNone
		}
		kind, arg, _ := strings.Cut(id, ":")
		switch kind {
		case "btn":
			if p.confirm {
				p.confirm = false
				if arg == "yes" {
					p.deleteCurrent()
				}
			}
		case "del":
			if i := p.visIndex(arg); i >= 0 {
				p.cur = i
				p.scroll()
				p.askDelete()
			}
		case "sess":
			p.confirm = false
			if i := p.visIndex(arg); i >= 0 && p.selectable(i) {
				p.cur = i
				return p.choose()
			}
		}
	}
	return pickNone
}

// wheel scrolls the window by three rows per notch.
func (p *picker) wheel(msg tea.MouseWheelMsg) {
	switch msg.Button {
	case tea.MouseWheelUp:
		p.top = max(p.top-wheelRows, 0)
	case tea.MouseWheelDown:
		p.top = max(min(p.top+wheelRows, len(p.vis)-p.page()), 0)
	}
}

// askDelete starts the delete confirmation for the selected row (a deleter must be wired
// and the row must not be the current Session).
func (p *picker) askDelete() {
	if !p.deletable(p.cur) {
		if p.cur >= 0 && p.selectable(p.cur) && p.del != nil {
			p.note = errCurrentSession
		}
		return
	}
	p.confirm, p.note = true, ""
}

// deletable says whether vis row i may be deleted: readable, a deleter is wired and it is
// not the Session being written.
func (p *picker) deletable(i int) bool {
	return p.del != nil && p.selectable(i) && p.rows[p.vis[i]].ID != p.current
}

// deleteCurrent deletes the selected session through p.del, drops its row and keeps the cursor
// near where it was. A delete error is shown in the footer and the row stays.
func (p *picker) deleteCurrent() {
	if !p.deletable(p.cur) {
		return
	}
	ri, pos := p.vis[p.cur], p.cur
	if err := p.del(p.rows[ri].ID); err != nil {
		p.note = "delete failed: " + err.Error()
		return
	}
	p.rows = append(p.rows[:ri:ri], p.rows[ri+1:]...)
	p.refilter()
	p.selectNear(pos)
	p.note = "session deleted"
}

// selectNear puts the cursor on the selectable row nearest to vis position pos (at or before it
// first, then after).
func (p *picker) selectNear(pos int) {
	pos = min(pos, len(p.vis)-1)
	for _, d := range []int{-1, 1} {
		for i := pos; i >= 0 && i < len(p.vis); i += d {
			if p.selectable(i) {
				p.cur = i
				p.scroll()
				return
			}
		}
	}
}

// choose records the selected row; nothing happens without a selectable row.
func (p *picker) choose() pickAction {
	if p.cur < 0 || !p.selectable(p.cur) {
		return pickNone
	}
	p.chosen = p.rows[p.vis[p.cur]].ID
	return pickChosen
}

// ---------------------------------------------------------------------------
// Selection and filter
// ---------------------------------------------------------------------------

// page is the number of list rows on screen.
func (p *picker) page() int { return max(p.h-pickChrome, 1) }

func (p *picker) selectable(i int) bool {
	return i >= 0 && i < len(p.vis) && p.rows[p.vis[i]].Err == ""
}

// visIndex is the vis position of the row with id, -1 when it is not shown.
func (p *picker) visIndex(id string) int {
	for i, ri := range p.vis {
		if p.rows[ri].ID == id {
			return i
		}
	}
	return -1
}

// move steps to the next selectable row in direction dir (+1/-1); at the end it stays.
func (p *picker) move(dir int) {
	for i := p.cur + dir; i >= 0 && i < len(p.vis); i += dir {
		if p.selectable(i) {
			p.cur = i
			break
		}
	}
	p.scroll()
}

// jump moves by n rows, then to the nearest selectable row (in the move direction first,
// else back); without any it stays.
func (p *picker) jump(n int) {
	if len(p.vis) == 0 {
		return
	}
	dir := 1
	if n < 0 {
		dir = -1
	}
	t := max(min(p.cur+n, len(p.vis)-1), 0)
	for _, d := range []int{dir, -dir} {
		for i := t; i >= 0 && i < len(p.vis); i += d {
			if p.selectable(i) {
				p.cur = i
				p.scroll()
				return
			}
		}
	}
}

// refilter rebuilds vis for the current filter and selects its first selectable row.
// Unreadable rows have no title, so a filter hides them.
func (p *picker) refilter() {
	q := strings.ToLower(p.filter)
	p.vis = p.vis[:0]
	for i, r := range p.rows {
		if q == "" || strings.Contains(strings.ToLower(r.Title), q) {
			p.vis = append(p.vis, i)
		}
	}
	p.cur, p.top = -1, 0
	p.move(1)
}

// scroll keeps the selected row inside the window.
func (p *picker) scroll() {
	if p.cur >= 0 {
		if p.cur < p.top {
			p.top = p.cur
		}
		if p.cur >= p.top+p.page() {
			p.top = p.cur - p.page() + 1
		}
	}
	p.top = max(min(p.top, len(p.vis)-p.page()), 0)
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// View is the standalone screen: the lines with the hover applied.
func (p *picker) View() tea.View {
	p.hv.begin()
	lines := p.lines()
	applyHover(p.hv, lines)
	v := tea.NewView(fitProfile(strings.Join(lines, "\n"), p.profile))
	v.AltScreen, v.MouseMode = true, tea.MouseModeAllMotion
	return v
}

// lines draws the picker into at most p.h rows of width p.w: header, rows, filler, footer.
// Every readable row registers the hover spot sess:<id> (its text), a deletable row also
// del:<id> (the "  ✕" at the end), and a pending confirmation registers btn:yes / btn:no on
// the footer - only for rows that fit (a cut row must not leave a click target behind). The
// host runs hover.begin before and applyHover after.
func (p *picker) lines() []string {
	lines := make([]string, 0, p.h)
	var spots []hotspot
	head := p.title
	if p.filter != "" || p.typing {
		head += "   filter: " + Clean(p.filter) + "_"
	}
	lines = append(lines, pickHead.Render(ansi.Truncate(head, p.w, "")))

	mw := 0
	for _, r := range p.rows {
		mw = min(max(mw, lipgloss.Width(Clean(r.Model))), pickModelMax)
	}
	end := min(p.top+p.page(), len(p.vis))
	for i := p.top; i < end; i++ {
		r := p.rows[p.vis[i]]
		y := len(lines)
		if r.Err != "" {
			lines = append(lines, pickDim.Render(ansi.Truncate(fmt.Sprintf("  %s  (unreadable)", r.ID), p.w, "…")))
			continue
		}
		model := ansi.Truncate(Clean(r.Model), mw, "…")
		text := fmt.Sprintf("  %s  %s  %4d msgs  %s", r.Time.Local().Format(pickTimeFmt),
			model+strings.Repeat(" ", mw-lipgloss.Width(model)), r.Messages, firstLine(Clean(r.Title)))
		if r.ID == p.current {
			text += "  " + pickCurrent
		}
		del := p.deletable(i)
		bw := p.w // cells for the text; a deletable row keeps "  ✕" at the end
		if del {
			bw = max(p.w-3, 1)
		}
		line := ansi.Truncate(text, bw, "…")
		spots = append(spots, hotspot{"sess:" + r.ID, y, 0, lipgloss.Width(line)})
		if del {
			line += strings.Repeat(" ", max(bw-lipgloss.Width(line), 0)) + "  " + pickDelete
			spots = append(spots, hotspot{"del:" + r.ID, y, max(p.w-3, 0), p.w})
		}
		if i == p.cur {
			line = pickSel.Render(line + strings.Repeat(" ", max(p.w-lipgloss.Width(line), 0)))
		}
		lines = append(lines, line)
	}
	for len(lines) < p.h-1 {
		lines = append(lines, "")
	}
	foot, btns := p.footer(len(lines))
	lines = append(lines, foot)
	spots = append(spots, btns...)
	if len(lines) > p.h {
		lines = lines[:max(p.h, 0)]
	}
	for _, s := range spots {
		if s.y < len(lines) {
			p.hv.add(s.id, s.y, s.x0, s.x1)
		}
	}
	return lines
}

// footer is the last row: the key hints, a pending confirmation with its buttons, or the
// last note. y is its screen row; the button spots are returned, not registered.
func (p *picker) footer(y int) (string, []hotspot) {
	var btns []hotspot
	foot := pickKeysHint
	switch {
	case p.confirm && p.deletable(p.cur):
		ask := "delete session " + firstLine(Clean(p.rows[p.vis[p.cur]].Title)) + "?   "
		foot = ask + btnDelete + "   " + btnCancel
		x := lipgloss.Width(ask)
		btns = append(btns, hotspot{"btn:yes", y, x, x + lipgloss.Width(btnDelete)})
		x += lipgloss.Width(btnDelete) + 3
		btns = append(btns, hotspot{"btn:no", y, x, x + lipgloss.Width(btnCancel)})
	case p.note != "":
		foot = p.note + "   (" + pickKeysHint + ")"
	case len(p.vis) == 0 && len(p.rows) == 0:
		foot = "no sessions left   Esc cancel"
	case len(p.vis) == 0:
		foot = "no session matches   Backspace edit   Esc cancel"
	}
	return pickDim.Render(ansi.Truncate(foot, p.w, "…")), btns
}
