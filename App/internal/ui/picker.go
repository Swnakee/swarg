package ui

import (
	"fmt"
	"strings"
	"swarg/internal/ui/menu"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------------------
// Session picker: the saved Sessions in a menu. Standalone it is the small full-screen program
// shown BEFORE the main UI (swarg -r); embedded it takes the list area of the main UI
// (/sessions, /resume). The host owns the size, the hover and what happens when a row is chosen.
// ---------------------------------------------------------------------------

// PickRow is one saved session in the picker. A non-empty Err marks an unreadable file:
// it is shown dimmed and cannot be chosen.
type PickRow struct {
	ID, Title, Model string
	Time             time.Time
	Messages         int
	Err              string
}

const (
	pickTimeFmt       = "2006-01-02 15:04"
	pickModelMax      = 28
	pickDelete        = "✕"
	pickCurrent       = "(current)"
	btnDelete         = "[y] delete"
	pickKeysHint      = "↑/↓ move   Enter open   Ctrl+D delete   Esc cancel   type to filter"
	errCurrentSession = "the current session cannot be deleted" // the same text as main's SessionsAPI.Delete
)

// PickSession shows the picker and returns the chosen session id. ok is false when the user
// cancelled (or there was nothing to choose).
func PickSession(rows []PickRow, del func(id string) error) (id string, ok bool, err error) {
	if len(rows) == 0 {
		return "", false, nil
	}
	p := newPicker(rows, "", del)
	final, err := tea.NewProgram(p).Run()
	if err != nil {
		return "", false, err
	}
	pk := final.(*menu.Menu)
	return pk.Chosen(), pk.Chosen() != "", nil
}

// sessionPick is the session host of a menu: the rows, the Session being written (marked, never
// deleted) and the deleter (nil disables deletion).
type sessionPick struct {
	m       *menu.Menu
	rows    []PickRow
	current string
	del     func(id string) error
}

// newPicker builds the Session menu: rows in the given order (newest first), current marked,
// Ctrl+D or the ✕ asks to delete through del. Each edit gets a last look at the menu options (the
// embedded picker sets its title and its Enter action there).
func newPicker(rows []PickRow, current string, del func(id string) error, edit ...func(*menu.Options)) *menu.Menu {
	s := &sessionPick{rows: rows, current: current, del: del}
	o := menuOpts("Resume a session", s.items())
	o.Hint, o.Empty = pickKeysHint, "no sessions left   Esc cancel"
	o.Keys["ctrl+d"] = func(string) { s.askDelete() }
	o.OnRight = func(string) { s.askDelete() }
	for _, e := range edit {
		e(&o)
	}
	s.m = menu.New(o)
	return s.m
}

// items lays the rows out: `started (local)  model  N msgs  title`, (current), ✕ when deletable.
func (s *sessionPick) items() []menu.Row {
	mw := 0
	for _, r := range s.rows {
		mw = min(max(mw, lipgloss.Width(Clean(r.Model))), pickModelMax)
	}
	var items []menu.Row
	for _, r := range s.rows {
		if r.Err != "" {
			items = append(items, menu.Row{ID: r.ID, Text: fmt.Sprintf("  %s  (unreadable)", r.ID), Dim: true})
			continue
		}
		model := ansi.Truncate(Clean(r.Model), mw, "…")
		text := fmt.Sprintf("  %s  %s  %4d msgs  %s", r.Time.Local().Format(pickTimeFmt),
			model+strings.Repeat(" ", mw-lipgloss.Width(model)), r.Messages, firstLine(Clean(r.Title)))
		if r.ID == s.current {
			text += "  " + pickCurrent
		}
		row := menu.Row{ID: r.ID, Text: text, Match: r.Title}
		if s.deletable(r) {
			row.Right = pickDelete
		}
		items = append(items, row)
	}
	return items
}

// row is the PickRow with id, nil when gone.
func (s *sessionPick) row(id string) *PickRow {
	for i := range s.rows {
		if s.rows[i].ID == id {
			return &s.rows[i]
		}
	}
	return nil
}

// deletable says whether r may be deleted: readable, a deleter is wired, not the Session being written.
func (s *sessionPick) deletable(r PickRow) bool {
	return s.del != nil && r.Err == "" && r.ID != s.current
}

// askDelete starts the delete confirmation for the selected row.
func (s *sessionPick) askDelete() {
	r := s.row(s.m.SelectedID())
	if r == nil || !s.deletable(*r) {
		if r != nil && r.Err == "" && s.del != nil {
			s.m.SetNote(errCurrentSession)
		}
		return
	}
	id := r.ID
	s.m.Confirm("delete session "+firstLine(Clean(r.Title))+"?", btnDelete, func() { s.deleteRow(id) })
}

// deleteRow deletes the session through del, drops its row and keeps the cursor near where it
// was. A delete error is shown in the footer and the row stays.
func (s *sessionPick) deleteRow(id string) {
	if err := s.del(id); err != nil {
		s.m.SetNote("delete failed: " + err.Error())
		return
	}
	pos := s.m.VisIndex(id)
	for i := range s.rows {
		if s.rows[i].ID == id {
			s.rows = append(s.rows[:i:i], s.rows[i+1:]...)
			break
		}
	}
	s.m.SetRows(s.items())
	s.m.SelectNear(pos)
	s.m.SetNote("session deleted")
}
