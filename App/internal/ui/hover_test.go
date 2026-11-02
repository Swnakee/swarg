package ui

import (
	"fmt"
	"strings"
	"swarg/internal/ui/hover"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// hoverTo moves the pointer (no button held) to screen cell (x, y).
func (r *rig) hoverTo(x, y int) result {
	return r.send(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseNone})
}

// rawRows are the screen rows with their escape sequences (to see the underline).
func (r *rig) rawRows() []string { return strings.Split(r.m.View().Content, "\n") }

// underlined lists the screen rows that carry the hover underline.
func underlined(rows []string) []int {
	var out []int
	for i, row := range rows {
		if strings.Contains(row, hover.UnderlineOn) {
			out = append(out, i)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Tool block heads
// ---------------------------------------------------------------------------

func TestHover_HeadRowUnderlinedWhilePointerOverIt(t *testing.T) {
	r := newRig(t)
	r.blocks(2, 2)
	y1, y2 := rowOf(r, "read_file(f1)"), rowOf(r, "read_file(f2)")
	r.hoverTo(3, y1)
	if got := underlined(r.rawRows()); len(got) != 1 || got[0] != y1 {
		t.Fatalf("underlined rows %v, want [%d]", got, y1)
	}
	r.hoverTo(3, y2)
	if got := underlined(r.rawRows()); len(got) != 1 || got[0] != y2 {
		t.Fatalf("underlined rows %v, want [%d]", got, y2)
	}
	r.hoverTo(3, y2+1) // the result line is not clickable
	if got := underlined(r.rawRows()); len(got) != 0 {
		t.Fatalf("a result row is underlined: %v", got)
	}
	r.hoverTo(60, y1) // past the end of the head text
	if got := underlined(r.rawRows()); len(got) != 0 {
		t.Fatalf("cells past the head text are not the button: %v", got)
	}
}

func TestHover_WrappedHeadHighlightsAllItsRows(t *testing.T) {
	r := newRig(t)
	r.send(tea.WindowSizeMsg{Width: 24, Height: 40})
	r.events(started("q"), toolStart("c1", "read_file", strings.Repeat("x", 40)),
		toolEnd("c1", "read_file", strings.Repeat("x", 40), "ok", "a\n", nil))
	y := rowOf(r, "read_file(xxx")
	n := r.m.cv.at(1).cache.headRows
	if n < 2 {
		t.Fatalf("the head did not wrap: %d rows", n)
	}
	r.hoverTo(2, y+1) // a continuation row of the head
	var want []int
	for i := range n {
		want = append(want, y+i)
	}
	if got := underlined(r.rawRows()); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("underlined rows %v, want %v", got, want)
	}
	r.click(2, y+1)
	if !r.isOpen(1) {
		t.Error("a click on the continuation row of the head must toggle")
	}
}

// I-10: moving the pointer changes nothing but the look.
func TestHover_MotionChangesNoState(t *testing.T) {
	r := newRig(t)
	r.blocks(30, 3)
	r.open(5)
	r.send(key(tea.KeyPgUp, 0))
	sc, sel, plain := r.m.sc, r.m.sel, r.view()
	open := map[int]bool{}
	for i := 0; i < r.m.cv.count(); i++ {
		open[i] = r.m.cv.at(i).open
	}
	ag := fmt.Sprint(len(r.ag.submitted), r.ag.interrupts, r.ag.aborts, r.ag.clears, r.ag.backgrounds)
	for y := 0; y < 40; y++ {
		for _, x := range []int{0, 3, 10, 79} {
			r.hoverTo(x, y)
			if r.m.sc != sc || r.m.sel != sel {
				t.Fatalf("motion to (%d,%d) changed scroll or selection", x, y)
			}
			if r.view() != plain {
				t.Fatalf("motion to (%d,%d) changed the text on screen:\n%s", x, y, r.view())
			}
		}
	}
	for i, o := range open {
		if r.m.cv.at(i).open != o {
			t.Errorf("block %d toggled by motion", i)
		}
	}
	if fmt.Sprint(len(r.ag.submitted), r.ag.interrupts, r.ag.aborts, r.ag.clears, r.ag.backgrounds) != ag {
		t.Error("motion reached the agent")
	}
}

func TestHover_DragIsASelectionNotAHover(t *testing.T) {
	r := newRig(t)
	r.blocks(2, 2)
	y := rowOf(r, "read_file(f1)")
	r.press(2, y)
	r.move(8, y+1)
	if !r.m.sel.active {
		t.Fatal("motion with the button held must select")
	}
	if got := underlined(r.rawRows()); len(got) != 0 {
		t.Errorf("hover shown during a drag: rows %v", got)
	}
	r.release(8, y+1)
}

func TestHover_SpotsFollowTheScroll(t *testing.T) {
	r := newRig(t)
	r.blocks(30, 3)
	r.send(key(tea.KeyPgUp, 0))
	r.send(key(tea.KeyPgUp, 0))
	y := -1
	for i, l := range r.rows()[:r.m.listHeight()] {
		if strings.Contains(l, "read_file(f") {
			y = i
			break
		}
	}
	if y < 0 {
		t.Fatal("no head in view")
	}
	r.hoverTo(3, y)
	if got := underlined(r.rawRows()); len(got) != 1 || got[0] != y {
		t.Fatalf("underlined rows %v, want [%d]", got, y)
	}
	id, ok := r.m.hv.At(3, y)
	if !ok || !strings.HasPrefix(id, "tool:") {
		t.Errorf("spot under the pointer = %q %v", id, ok)
	}
}

func TestView_MouseModeAllMotion(t *testing.T) {
	r := newRig(t)
	if got := r.m.View().MouseMode; got != tea.MouseModeAllMotion {
		t.Errorf("main view mouse mode %v", got)
	}
	if got := newPicker(pickRows(), "", nil).View().MouseMode; got != tea.MouseModeAllMotion {
		t.Errorf("picker mouse mode %v", got)
	}
}

// ---------------------------------------------------------------------------
// Session picker rows
// ---------------------------------------------------------------------------

func TestHover_PickerRowUnderlined(t *testing.T) {
	p := newPicker(pickRows(), "", nil) // rows: c3 (y=1), b2 unreadable (y=2), a1 (y=3)
	drive(p, tea.WindowSizeMsg{Width: 80, Height: 10})
	rows := func() []string { return strings.Split(p.View().Content, "\n") }
	drive(p, tea.MouseMotionMsg{X: 4, Y: 3, Button: tea.MouseNone})
	if got := underlined(rows()); len(got) != 1 || got[0] != 3 {
		t.Fatalf("underlined rows %v, want [3]", got)
	}
	drive(p, tea.MouseMotionMsg{X: 4, Y: 2, Button: tea.MouseNone})
	if got := underlined(rows()); len(got) != 0 {
		t.Fatalf("an unreadable row is not clickable: %v", got)
	}
	drive(p, tea.MouseMotionMsg{X: 4, Y: 1, Button: tea.MouseNone})
	if got := underlined(rows()); len(got) != 1 || got[0] != 1 {
		t.Fatalf("the selected row must show the hover too: %v", got)
	}
	if id, ok := p.SpotAt(4, 1); !ok || id != "row:c3" {
		t.Errorf("spot = %q %v, want row:c3", id, ok)
	}
	drive(p, tea.MouseMotionMsg{X: 4, Y: 0, Button: tea.MouseNone})
	if got := underlined(rows()); len(got) != 0 {
		t.Fatalf("the header is not clickable: %v", got)
	}
	if fmt.Sprint(p.Cursor()) != "0" {
		t.Errorf("motion moved the cursor to %d", p.Cursor())
	}
}
