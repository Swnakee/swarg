package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Picker model driven by synthetic messages
// ---------------------------------------------------------------------------

func pickRows() []PickRow {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	return []PickRow{
		{ID: "c3", Title: "fix the parser", Model: "m", Time: t0.Add(2 * time.Hour), Messages: 4},
		{ID: "b2", Err: "bad json"},
		{ID: "a1", Title: "write docs", Model: "m", Time: t0, Messages: 2},
	}
}

func pkey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }
func ptype(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

// drive sends the messages and, like Bubble Tea, draws after each one (clicks hit the spots
// of the last draw).
func drive(p *menu, msgs ...tea.Msg) (quit bool) {
	for _, m := range msgs {
		_, cmd := p.Update(m)
		if cmd != nil {
			if _, ok := cmd().(tea.QuitMsg); ok {
				quit = true
			}
		}
		p.View()
	}
	return quit
}

func TestPicker_EnterChoosesFirstReadable(t *testing.T) {
	p := newPicker(pickRows(), "", nil)
	if !drive(p, pkey(tea.KeyEnter)) || p.chosen != "c3" {
		t.Fatalf("chosen = %q", p.chosen)
	}
}

func TestPicker_NavigationSkipsUnreadable(t *testing.T) {
	p := newPicker(pickRows(), "", nil)
	drive(p, pkey(tea.KeyDown), pkey(tea.KeyEnter))
	if p.chosen != "a1" {
		t.Errorf("down over the unreadable row: chosen %q, want a1", p.chosen)
	}
	p = newPicker(pickRows(), "", nil)
	drive(p, ptype("j"), ptype("j"), ptype("k"), pkey(tea.KeyEnter))
	if p.chosen != "c3" {
		t.Errorf("j j k: chosen %q, want c3", p.chosen)
	}
	p = newPicker(pickRows(), "", nil)
	drive(p, pkey(tea.KeyEnd), pkey(tea.KeyEnter))
	if p.chosen != "a1" {
		t.Errorf("End: chosen %q, want a1", p.chosen)
	}
	p = newPicker(pickRows(), "", nil)
	drive(p, pkey(tea.KeyEnd), pkey(tea.KeyHome), pkey(tea.KeyEnter))
	if p.chosen != "c3" {
		t.Errorf("Home: chosen %q, want c3", p.chosen)
	}
	p = newPicker(pickRows(), "", nil)
	drive(p, pkey(tea.KeyPgDown), pkey(tea.KeyEnter))
	if p.chosen != "a1" {
		t.Errorf("PgDn: chosen %q, want a1", p.chosen)
	}
	drive(p, pkey(tea.KeyPgUp))
	if p.cur != 0 {
		t.Errorf("PgUp: cur %d, want 0", p.cur)
	}
}

func TestPicker_CancelKeys(t *testing.T) {
	for name, k := range map[string]tea.KeyPressMsg{"esc": pkey(tea.KeyEscape), "q": ptype("q"), "ctrl+c": {Code: 'c', Mod: tea.ModCtrl}} {
		p := newPicker(pickRows(), "", nil)
		if !drive(p, k) || p.chosen != "" {
			t.Errorf("%s: quit or chosen %q wrong", name, p.chosen)
		}
	}
}

func TestPicker_FilterByTitle(t *testing.T) {
	p := newPicker(pickRows(), "", nil)
	drive(p, ptype("d"), ptype("o"), ptype("c"))
	if p.filter != "doc" || len(p.vis) != 1 {
		t.Fatalf("filter %q, %d rows", p.filter, len(p.vis))
	}
	if !strings.Contains(p.View().Content, "filter: doc") {
		t.Error("the view does not show the filter")
	}
	drive(p, pkey(tea.KeyEnter))
	if p.chosen != "a1" {
		t.Errorf("chosen %q, want a1", p.chosen)
	}

	// j, k and q type into the filter once filtering has started
	p = newPicker(pickRows(), "", nil)
	drive(p, ptype("/"), ptype("q"))
	if p.filter != "q" || len(p.vis) != 0 {
		t.Errorf("after / q: filter %q, %d rows", p.filter, len(p.vis))
	}
	if drive(p, pkey(tea.KeyEnter)) || p.chosen != "" {
		t.Error("Enter with no match must do nothing")
	}
	drive(p, pkey(tea.KeyBackspace))
	if p.filter != "" || len(p.vis) != 3 {
		t.Errorf("backspace: filter %q, %d rows", p.filter, len(p.vis))
	}
	drive(p, ptype("p"), ptype("a"))
	drive(p, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if p.filter != "" || len(p.vis) != 3 {
		t.Errorf("ctrl+u: filter %q, %d rows", p.filter, len(p.vis))
	}
}

func TestPicker_OnlyUnreadableCannotChoose(t *testing.T) {
	p := newPicker([]PickRow{{ID: "b2", Err: "bad"}}, "", nil)
	if drive(p, pkey(tea.KeyEnter)) || p.chosen != "" {
		t.Error("an unreadable row was chosen")
	}
	if v := p.View().Content; !strings.Contains(v, "(unreadable)") {
		t.Errorf("view lacks (unreadable):\n%s", v)
	}
}

func TestPicker_MouseClickAndWheel(t *testing.T) {
	p := newPicker(pickRows(), "", nil)
	drive(p, tea.MouseClickMsg{X: 3, Y: 2, Button: tea.MouseLeft}) // the unreadable row
	if p.chosen != "" {
		t.Error("click on an unreadable row chose it")
	}
	drive(p, tea.MouseClickMsg{X: 3, Y: 0, Button: tea.MouseLeft}) // header
	drive(p, tea.MouseClickMsg{X: 3, Y: 1, Button: tea.MouseRight})
	if p.chosen != "" {
		t.Error("header or right click chose a row")
	}
	if !drive(p, tea.MouseClickMsg{X: 3, Y: 3, Button: tea.MouseLeft}) || p.chosen != "a1" {
		t.Errorf("click on row 3: chosen %q, want a1", p.chosen)
	}
}

func TestPicker_WheelScrollsAndLongListFollowsCursor(t *testing.T) {
	var rows []PickRow
	for i := 0; i < 50; i++ {
		rows = append(rows, PickRow{ID: fmt.Sprintf("id%02d", i), Title: fmt.Sprintf("t%02d", i), Model: "m", Time: time.Unix(int64(1000-i), 0)})
	}
	p := newPicker(rows, "", nil)
	drive(p, tea.WindowSizeMsg{Width: 60, Height: 12})
	drive(p, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if p.top != 3 || p.cur != 0 {
		t.Errorf("wheel: top %d cur %d", p.top, p.cur)
	}
	drive(p, tea.MouseWheelMsg{Button: tea.MouseWheelUp}, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if p.top != 0 {
		t.Errorf("wheel up clamp: top %d", p.top)
	}
	drive(p, pkey(tea.KeyEnd))
	if p.cur != 49 || p.top != 49-p.page()+1 {
		t.Errorf("End: cur %d top %d", p.cur, p.top)
	}
	if got := strings.Count(p.View().Content, "\n") + 1; got != 12 {
		t.Errorf("view has %d lines, want 12", got)
	}
	if !strings.Contains(p.View().Content, "t49") {
		t.Error("the last row is not visible after End")
	}
	drive(p, pkey(tea.KeyPgUp))
	if p.cur != 49-p.page() {
		t.Errorf("PgUp: cur %d", p.cur)
	}
}

func TestPicker_RowFormat(t *testing.T) {
	v := newPicker(pickRows(), "", nil).View().Content
	for _, want := range []string{"2 msgs", "4 msgs", "fix the parser", "write docs", "Esc cancel"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Index(v, "fix the parser") > strings.Index(v, "write docs") {
		t.Error("rows are not in the given (newest first) order")
	}
}

func TestPickSession_EmptyListNeverStartsProgram(t *testing.T) {
	if id, ok, err := PickSession(nil, nil); id != "" || ok || err != nil {
		t.Errorf("got %q %v %v", id, ok, err)
	}
}

// ---------------------------------------------------------------------------
// Deleting a session from the picker (Ctrl+D, confirm with y)
// ---------------------------------------------------------------------------

func ctrlD() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl} }

// delPicker is a picker over pickRows whose deleter records the ids it was asked to delete.
func delPicker(t *testing.T, err error) (*menu, *[]string) {
	t.Helper()
	var got []string
	p := newPicker(pickRows(), "", func(id string) error { got = append(got, id); return err })
	return p, &got
}

// has reports whether the menu still lists the row id.
func has(p *menu, id string) bool {
	for _, r := range p.items {
		if r.ID == id {
			return true
		}
	}
	return false
}

func TestPicker_DeleteConfirmedRemovesTheRow(t *testing.T) {
	p, got := delPicker(t, nil)
	drive(p, ctrlD())
	if !p.confirm {
		t.Fatal("Ctrl+D did not ask to confirm")
	}
	drive(p, ptype("y"))
	if len(*got) != 1 || (*got)[0] != "c3" {
		t.Fatalf("deleted ids = %v, want [c3]", *got)
	}
	if has(p, "c3") {
		t.Error("the deleted row is still in the picker")
	}
	if p.confirm {
		t.Error("still confirming after delete")
	}
	// the cursor stays on a selectable row and Enter opens it
	drive(p, pkey(tea.KeyEnter))
	if p.chosen != "a1" {
		t.Errorf("after delete chosen = %q, want the next readable a1", p.chosen)
	}
}

func TestPicker_DeleteCancelledKeepsTheRow(t *testing.T) {
	p, got := delPicker(t, nil)
	drive(p, ctrlD(), pkey(tea.KeyEsc)) // esc while confirming cancels, does not quit
	if len(*got) != 0 {
		t.Fatalf("delete ran on cancel: %v", *got)
	}
	if p.confirm {
		t.Error("still confirming after cancel")
	}
	if !has(p, "c3") {
		t.Error("the row was removed although the delete was cancelled")
	}
}

func TestPicker_DeleteErrorKeepsTheRowAndShows(t *testing.T) {
	p, _ := delPicker(t, fmt.Errorf("permission denied"))
	drive(p, ctrlD(), ptype("y"))
	if !strings.Contains(p.note, "permission denied") {
		t.Errorf("note = %q, want the error", p.note)
	}
	if !has(p, "c3") {
		t.Error("a failed delete removed the row")
	}
}

func TestPicker_DeleteDisabledWithoutADeleter(t *testing.T) {
	p := newPicker(pickRows(), "", nil) // no deleter
	drive(p, ctrlD())
	if p.confirm {
		t.Error("Ctrl+D asked to confirm with no deleter wired")
	}
}

func TestPicker_DeleteEveryRowLeavesAnEmptyList(t *testing.T) {
	p, got := delPicker(t, nil)
	drive(p, ctrlD(), ptype("y"), ctrlD(), ptype("y")) // two readable rows (b2 is unreadable)
	if len(*got) != 2 {
		t.Fatalf("deleted %v, want both readable rows", *got)
	}
	// the unreadable row may remain; Enter must not choose it, Esc quits
	if q := drive(p, pkey(tea.KeyEnter)); q || p.chosen != "" {
		t.Errorf("Enter chose something from an all-unreadable list: %q", p.chosen)
	}
	if !drive(p, pkey(tea.KeyEsc)) {
		t.Error("Esc did not quit the emptied picker")
	}
}
