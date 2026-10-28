package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (r *rig) press(x, y int) result {
	return r.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}
func (r *rig) move(x, y int) result {
	return r.send(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
}
func (r *rig) release(x, y int) result {
	return r.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// click is press and release on the same cell.
func (r *rig) click(x, y int) result {
	r.press(x, y)
	return r.release(x, y)
}

// drag presses at (x1,y1), moves to (x2,y2) and releases there.
func (r *rig) drag(x1, y1, x2, y2 int) result {
	r.press(x1, y1)
	r.move(x2, y2)
	return r.release(x2, y2)
}

// blocks adds n finished read_file blocks, each with `lines` output lines.
func (r *rig) blocks(n, lines int) {
	r.events(started("q"))
	for i := 1; i <= n; i++ {
		id := fmt.Sprint("c", i)
		r.events(toolStart(id, "read_file", fmt.Sprint("f", i)), toolEnd(id, "read_file", fmt.Sprint("f", i), "ok", numbered(lines), nil))
	}
}

func (r *rig) isOpen(item int) bool { return r.m.cv.at(item).open }

// ---------------------------------------------------------------------------
// Click
// ---------------------------------------------------------------------------

func TestMouse_ClickTogglesAnyBlock(t *testing.T) {
	r := newRig(t)
	r.blocks(3, 2) // items 1..3 (item 0 is the user message)
	for _, n := range []int{1, 2, 3} {
		y := rowOf(r, fmt.Sprintf("read_file(f%d)", n))
		r.click(4, y)
		if !r.isOpen(n) {
			t.Fatalf("block %d did not open", n)
		}
		if r.press(4, rowOf(r, fmt.Sprintf("read_file(f%d)", n))); !r.isOpen(n) {
			t.Fatalf("a press alone must not toggle block %d", n)
		}
		r.release(4, rowOf(r, fmt.Sprintf("read_file(f%d)", n)))
		if r.isOpen(n) {
			t.Fatalf("block %d did not close", n)
		}
	}
}

func TestMouse_ClickOnABlockScrolledIntoView(t *testing.T) {
	r := newRig(t)
	r.blocks(30, 3)
	r.send(key(tea.KeyPgUp, 0))
	r.send(key(tea.KeyPgUp, 0))
	if r.m.sc.follow {
		t.Fatal("PgUp should leave the tail")
	}
	y := -1
	for i, l := range r.rows()[:r.m.listHeight()] {
		if strings.Contains(l, "read_file(f") {
			y = i
			break
		}
	}
	if y < 0 {
		t.Fatalf("no block head in view:\n%s", r.view())
	}
	var n int
	fmt.Sscanf(strings.SplitN(r.rows()[y], "read_file(f", 2)[1], "%d", &n)
	r.click(3, y)
	if !r.isOpen(n) {
		t.Errorf("block %d under the click did not open", n)
	}
}

func TestMouse_DragNeverToggles(t *testing.T) {
	r := newRig(t)
	r.blocks(2, 2)
	y := rowOf(r, "read_file(f1)")
	res := r.drag(4, y, 9, y+1)
	if r.isOpen(1) {
		t.Error("a drag that began on a head toggled the block")
	}
	if !res.clipped {
		t.Error("a drag must copy")
	}
	r.press(4, y)
	r.move(4, y) // motion on the press cell is not a drag
	r.release(4, y)
	if !r.isOpen(1) {
		t.Error("motion on the press cell must still be a click")
	}
}

func TestMouse_ClickOnOtherRowsDoesNothing(t *testing.T) {
	r := newRig(t)
	r.blocks(1, 2)
	before := r.view()
	rows := r.rows()
	for y, l := range rows {
		if strings.Contains(l, "read_file(f1)") || y == r.panelRow() {
			continue // the head toggles; the panel's items have their own clicks (TestPanel_*)
		}
		r.click(2, y)
	}
	if r.isOpen(1) || r.view() != before {
		t.Errorf("a click away from the head changed something:\n%s", r.view())
	}
	if r.m.sel.active {
		t.Error("a click must not select")
	}
}

func TestMouse_ClickBelowTheListDoesNothing(t *testing.T) {
	r := newRig(t)
	r.blocks(1, 2)
	listH := r.m.listHeight()
	for _, y := range []int{listH, listH + 1, 39, 100, -1} {
		r.click(2, y)
	}
	if r.isOpen(1) {
		t.Error("a click outside the list toggled a block")
	}
	// the empty part of a short list
	y := len(plainRows(r.m.cv.rows(0, r.m.cv.total()))) + 1
	r.click(2, y)
	if r.isOpen(1) {
		t.Error("a click on empty list space toggled a block")
	}
}

func TestMouse_RightClickAndWheelIgnored(t *testing.T) {
	r := newRig(t)
	r.blocks(1, 2)
	y := rowOf(r, "read_file(f1)")
	r.send(tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseRight})
	r.send(tea.MouseReleaseMsg{X: 2, Y: y, Button: tea.MouseRight})
	if r.isOpen(1) {
		t.Error("right click toggled")
	}
}

func TestMouse_FollowingStaysAtTheBottomAfterAClick(t *testing.T) {
	r := newRig(t)
	r.blocks(30, 3)
	y := rowOf(r, "read_file(f30)")
	r.click(3, y)
	if !r.isOpen(30) || !r.m.sc.follow {
		t.Errorf("open %v follow %v", r.isOpen(30), r.m.sc.follow)
	}
	if !strings.Contains(r.view(), "line 3") {
		t.Errorf("the bottom must show the opened output:\n%s", r.view())
	}
}

func TestMouse_OpeningABlockAboveKeepsWhatIsRead(t *testing.T) {
	r := newRig(t)
	r.blocks(30, 3)
	r.send(key(tea.KeyPgUp, 0))
	first := r.firstListRow()
	r.m.cv.setOpen(2, true) // far above the view, as ctrl+o or /tool would
	r.send(wheel(tea.MouseWheelDown))
	r.send(wheel(tea.MouseWheelUp))
	if got := r.firstListRow(); got != first {
		t.Errorf("the top row jumped: %q -> %q", first, got)
	}
	// and by click: the block on the top row opens, the top row stays
	y := 0
	for i, l := range r.rows()[:r.m.listHeight()] {
		if strings.Contains(l, "read_file(f") {
			y = i
			break
		}
	}
	top := r.m.viewTop()
	r.click(3, y)
	if r.m.viewTop() != top {
		t.Errorf("viewTop %d -> %d", top, r.m.viewTop())
	}
}

func TestMouse_ClicksDuringAnAskDoNotAnswerIt(t *testing.T) {
	r := newRig(t)
	r.blocks(1, 2)
	ch := r.ask(context.Background(), "shell", `{"command":"ls"}`)
	r.click(2, rowOf(r, "read_file(f1)"))
	r.click(2, r.m.listHeight()+1)
	if !r.isOpen(1) {
		t.Error("a click on a block during an ask must toggle it")
	}
	if r.m.ask == nil {
		t.Fatal("a click answered the ask")
	}
	select {
	case ap := <-ch:
		t.Fatalf("answered: %+v", ap)
	default:
	}
}

// ---------------------------------------------------------------------------
// Selection and copy
// ---------------------------------------------------------------------------

func TestSelect_AcrossItemsCopiesExactText(t *testing.T) {
	r := newRig(t)
	r.m.say("alpha")
	r.m.say("beta gamma")
	r.m.say("delta")
	// rows: alpha / beta gamma / delta
	res := r.drag(2, 0, 3, 2)
	if !res.clipped || res.clip != "pha\nbeta gamma\ndelt" {
		t.Errorf("copied %q", res.clip)
	}
	if !strings.Contains(r.view(), "copied 3 lines") {
		t.Errorf("no copied note in the status:\n%s", r.view())
	}
	if r.m.cv.count() != 3 {
		t.Error("the note must not be a list item")
	}
	if !strings.Contains(r.m.View().Content, reverseOn) {
		t.Error("the selection is not highlighted after the release")
	}
}

func TestSelect_BackwardsDragIsTheSame(t *testing.T) {
	r := newRig(t)
	r.m.say("alpha")
	r.m.say("beta")
	if res := r.drag(3, 1, 1, 0); res.clip != "lpha\nbeta" {
		t.Errorf("copied %q", res.clip)
	}
}

func TestSelect_SoftWrappedRowsJoinWithoutNewline(t *testing.T) {
	r := newRig(t)
	words := strings.Fields(strings.Repeat("lorem ipsum dolor sit amet ", 8))
	text := strings.Join(words, " ")
	r.m.say(text) // word-wrapped at 80 cells into 3 rows
	if h := r.m.cv.height(0); h < 2 {
		t.Fatalf("not wrapped: %d rows", h)
	}
	last := r.m.cv.height(0) - 1
	res := r.drag(0, 0, 79, last)
	if res.clip != text {
		t.Errorf("copied %q, want %q", res.clip, text)
	}
	if strings.Contains(res.clip, "\n") || r.m.copyNote != "copied 1 line" {
		t.Errorf("note %q", r.m.copyNote)
	}
}

func TestSelect_HardWrappedToolOutputAndGutters(t *testing.T) {
	r := newRig(t)
	long := strings.Repeat("x", 200)
	r.events(started("q"), toolStart("a", "read_file", "f"), toolEnd("a", "read_file", "f", "ok", "first\n"+long+"\nlast\n", nil))
	r.m.cv.setOpen(1, true)
	// head, then output rows (first, 3 pieces of x, last), then the result line
	rows := r.rows()
	head := rowOf(r, "read_file(f)")
	end := head
	for i, l := range rows {
		if strings.Contains(l, "└ ok") {
			end = i
		}
	}
	res := r.drag(0, head, 79, end)
	want := "● read_file(f)  #1\nfirst\n" + long + "\nlast\nok"
	if res.clip != want {
		t.Errorf("copied %q\nwant   %q", res.clip, want)
	}
}

func TestSelect_WideCharsMapCellsToGraphemes(t *testing.T) {
	r := newRig(t)
	r.m.say("a日本b😀c") // cells: a0 日1-2 本3-4 b5 😀6-7 c8
	for _, tc := range []struct {
		x1, x2 int
		want   string
	}{
		{0, 0, "a"},
		{2, 3, "日本"}, // the second half of 日 and the first of 本
		{1, 1, "日"},
		{4, 5, "本b"},
		{7, 8, "😀c"},
		{6, 6, "😀"},
	} {
		r.m.sel = selection{active: true, a: selPoint{0, 0, tc.x1}, b: selPoint{0, 0, tc.x2}}
		if got, _ := r.m.selectionText(); got != tc.want {
			t.Errorf("cells %d-%d: %q, want %q", tc.x1, tc.x2, got, tc.want)
		}
	}
	if got := ansi.Strip(reverseSpan("a日b", 1, 2)); got != "a日b" {
		t.Errorf("reverseSpan changed the text: %q", got)
	}
}

func TestSelect_HighlightIsReverseVideoOnTheSelectedCellsOnly(t *testing.T) {
	r := newRig(t)
	r.m.say("abcdef")
	r.press(1, 0)
	r.move(3, 0)
	row := strings.Split(r.m.View().Content, "\n")[0]
	if !strings.Contains(row, reverseOn) || ansi.Strip(row)[:6] != "abcdef" {
		t.Errorf("row %q", row)
	}
	i := strings.Index(row, reverseOn)
	j := strings.Index(row, reverseOff)
	if got := ansi.Strip(row[i:j]); got != "bcd" {
		t.Errorf("highlighted %q, want bcd", got)
	}
}

func TestSelect_EdgeDragAutoScrolls(t *testing.T) {
	r := newRig(t)
	r.fillRows(200)
	top := r.m.viewTop()
	r.press(2, 10)
	r.move(2, 0)
	if got := r.m.viewTop(); got != top-1 {
		t.Errorf("one motion at the top edge: viewTop %d, want %d", got, top-1)
	}
	r.move(3, 0)
	if got := r.m.viewTop(); got != top-2 {
		t.Errorf("second motion: viewTop %d, want %d", got, top-2)
	}
	bottom := r.m.listHeight() - 1
	r.move(3, bottom)
	if got := r.m.viewTop(); got != top-1 {
		t.Errorf("motion at the bottom edge: viewTop %d, want %d", got, top-1)
	}
	r.move(3, bottom+5) // below the list: still the bottom edge
	if got := r.m.viewTop(); got != top {
		t.Errorf("motion below the list: viewTop %d, want %d", got, top)
	}
	if !r.m.sel.active {
		t.Error("no selection")
	}
}

func TestSelect_SurvivesScrollingBecauseItIsInContentCoordinates(t *testing.T) {
	r := newRig(t)
	r.m.say("alpha")
	r.m.say("beta")
	r.drag(0, 0, 3, 1)
	r.fillRows(100)
	r.send(key(tea.KeyHome, 0))
	if text, _ := r.m.selectionText(); text != "alpha\nbeta" {
		t.Errorf("selection moved: %q", text)
	}
}

func TestSelect_ResizeAndClearDropIt(t *testing.T) {
	r := newRig(t)
	r.m.say("alpha")
	r.drag(0, 0, 3, 0)
	r.send(tea.WindowSizeMsg{Width: 60, Height: 40})
	if r.m.sel.active || strings.Contains(r.m.View().Content, reverseOn) {
		t.Error("resize kept the selection")
	}
	r.m.say("beta")
	r.drag(0, 1, 3, 1)
	r.send(tea.WindowSizeMsg{Width: 60, Height: 40}) // same size: nothing to reflow
	if !r.m.sel.active {
		t.Error("a repeated size message dropped the selection")
	}
	r.m.ta.SetValue("/clear")
	r.send(key(tea.KeyEnter, 0))
	if r.m.sel.active || r.m.copyNote != "" {
		t.Error("/clear kept the selection")
	}
}

func TestSelect_EscClearsSelectionBeforeInterrupting(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	r.m.say("alpha")
	r.drag(0, 1, 3, 1)
	if !r.m.sel.active {
		t.Fatal("no selection")
	}
	r.send(key(tea.KeyEscape, 0))
	if r.m.sel.active || r.ag.interrupts != 0 {
		t.Errorf("first Esc: selection %v, interrupts %d", r.m.sel.active, r.ag.interrupts)
	}
	r.send(key(tea.KeyEscape, 0))
	if r.ag.interrupts != 1 {
		t.Errorf("second Esc must interrupt, interrupts %d", r.ag.interrupts)
	}
}

func TestSelect_NextClickClearsSelectionAndNote(t *testing.T) {
	r := newRig(t)
	r.m.say("alpha")
	r.drag(0, 0, 3, 0)
	r.click(0, 5)
	if r.m.sel.active || strings.Contains(r.view(), "copied") {
		t.Error("a click must clear the selection and the note")
	}
}

func TestSelect_PressOutsideTheListNeverSelects(t *testing.T) {
	r := newRig(t)
	r.m.say("alpha")
	r.press(2, r.m.listHeight()+1) // the input box
	r.move(5, 0)
	res := r.release(5, 0)
	if r.m.sel.active || res.clipped {
		t.Error("a drag that began in the input box selected list text")
	}
}

func TestSelect_BlankSelectionCopiesNothing(t *testing.T) {
	r := newRig(t)
	r.m.say("alpha")
	if res := r.drag(10, 0, 20, 0); res.clipped || r.m.sel.active {
		t.Errorf("clipped %v active %v", res.clipped, r.m.sel.active)
	}
}

func TestSoftWrap_MarksNeverReachTheScreen(t *testing.T) {
	r := newRig(t)
	r.m.say(strings.Repeat("word ", 40))
	r.m.say(strings.Repeat("z", 300))
	if strings.Contains(r.m.View().Content, "\x1b_") || strings.Contains(r.convText(), "\x1b_") {
		t.Error("a soft-wrap mark leaked into the rows")
	}
	if c := r.m.cv.itemRows(0).cont; len(c) < 2 || c[0] != contNone || c[1] != contWord {
		t.Errorf("word wrap flags %v", c)
	}
	if c := r.m.cv.itemRows(1).cont; len(c) < 2 || c[1] != contHard {
		t.Errorf("hard wrap flags %v", c)
	}
}
