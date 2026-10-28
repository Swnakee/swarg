package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func wheel(b tea.MouseButton) tea.MouseWheelMsg { return tea.MouseWheelMsg{X: 5, Y: 5, Button: b} }

// firstListRow is the text of the first list row on screen.
func (r *rig) firstListRow() string { return strings.TrimSpace(r.rows()[0]) }

// lastListRow is the text of the last list row on screen.
func (r *rig) lastListRow() string { return strings.TrimSpace(r.rows()[r.m.listHeight()-1]) }

// ---------------------------------------------------------------------------
// Wheel, keys
// ---------------------------------------------------------------------------

func TestScroll_WheelThreeRows(t *testing.T) {
	r := newRig(t)
	r.fillRows(100)
	bottom := r.m.viewTop()
	if bottom != 100-r.m.listHeight() || !r.m.sc.follow {
		t.Fatalf("start: top %d follow %v", bottom, r.m.sc.follow)
	}
	r.send(wheel(tea.MouseWheelUp))
	if got := r.m.viewTop(); got != bottom-3 || r.m.sc.follow {
		t.Errorf("up: top %d, want %d (follow %v)", got, bottom-3, r.m.sc.follow)
	}
	r.send(wheel(tea.MouseWheelUp))
	r.send(wheel(tea.MouseWheelDown))
	if got := r.m.viewTop(); got != bottom-3 {
		t.Errorf("up, up, down: top %d, want %d", got, bottom-3)
	}
	r.send(wheel(tea.MouseWheelDown))
	if !r.m.sc.follow || r.m.viewTop() != bottom {
		t.Errorf("down to the bottom must follow again: top %d follow %v", r.m.viewTop(), r.m.sc.follow)
	}
	r.send(wheel(tea.MouseWheelDown)) // clamped
	if r.m.viewTop() != bottom {
		t.Errorf("clamped at the bottom: %d", r.m.viewTop())
	}
	for i := 0; i < 100; i++ {
		r.send(wheel(tea.MouseWheelUp))
	}
	if r.m.viewTop() != 0 || r.firstListRow() != "▸ q" && !strings.Contains(r.firstListRow(), "row 1") {
		t.Errorf("clamped at the top: %d %q", r.m.viewTop(), r.firstListRow())
	}
	r.send(wheel(tea.MouseWheelLeft)) // sideways wheel does nothing
	if r.m.viewTop() != 0 {
		t.Error("a sideways wheel moved the view")
	}
}

func TestScroll_PgUpPgDnHomeEnd(t *testing.T) {
	r := newRig(t)
	r.fillRows(200)
	page := r.m.listHeight() - 1
	bottom := r.m.viewTop()
	r.send(key(tea.KeyPgUp, 0))
	if got := r.m.viewTop(); got != bottom-page {
		t.Errorf("PgUp: top %d, want %d", got, bottom-page)
	}
	r.send(key(tea.KeyPgDown, 0))
	if got := r.m.viewTop(); got != bottom || !r.m.sc.follow {
		t.Errorf("PgDn: top %d follow %v", got, r.m.sc.follow)
	}
	r.send(key(tea.KeyHome, 0)) // empty input: Home scrolls
	if r.m.viewTop() != 0 {
		t.Errorf("Home on an empty input: top %d", r.m.viewTop())
	}
	r.send(key(tea.KeyEnd, 0))
	if !r.m.sc.follow || r.m.viewTop() != bottom {
		t.Errorf("End on an empty input: top %d follow %v", r.m.viewTop(), r.m.sc.follow)
	}
	// with text, Home and End belong to the textarea; Ctrl+Home/End still scroll
	r.m.ta.SetValue("hello")
	r.send(key(tea.KeyHome, 0))
	if !r.m.sc.follow || r.m.ta.Value() != "hello" {
		t.Errorf("Home with text must not scroll: follow %v", r.m.sc.follow)
	}
	r.send(tea.KeyPressMsg{Code: 'X', Text: "X"})
	if r.m.ta.Value() != "Xhello" {
		t.Errorf("Home must move the text cursor: %q", r.m.ta.Value())
	}
	r.send(key(tea.KeyHome, tea.ModCtrl))
	if r.m.viewTop() != 0 || r.m.ta.Value() != "Xhello" {
		t.Errorf("ctrl+Home: top %d input %q", r.m.viewTop(), r.m.ta.Value())
	}
	r.send(key(tea.KeyEnd, tea.ModCtrl))
	if !r.m.sc.follow {
		t.Error("ctrl+End must follow")
	}
	// PgUp/PgDn scroll even while typing
	r.send(key(tea.KeyPgUp, 0))
	if r.m.sc.follow || r.m.ta.Value() != "Xhello" {
		t.Errorf("PgUp with text: follow %v input %q", r.m.sc.follow, r.m.ta.Value())
	}
}

func TestScroll_EmptyList(t *testing.T) {
	r := newRig(t)
	for _, m := range []tea.Msg{wheel(tea.MouseWheelUp), wheel(tea.MouseWheelDown), key(tea.KeyPgUp, 0), key(tea.KeyPgDown, 0),
		key(tea.KeyHome, tea.ModCtrl), key(tea.KeyEnd, tea.ModCtrl)} {
		r.send(m)
		if !r.m.sc.follow || r.m.viewTop() != 0 {
			t.Fatalf("%v moved an empty list", m)
		}
	}
	wantFrame(t, r, 80, 40)
}

// ---------------------------------------------------------------------------
// Follow-tail
// ---------------------------------------------------------------------------

func TestScroll_FollowTailAtBottom(t *testing.T) {
	r := newRig(t)
	r.fillRows(100)
	r.fillRows(10)
	if !r.m.sc.follow || r.lastListRow() != "row 10" || strings.Contains(r.view(), "new rows") {
		t.Errorf("following: last row %q\n%s", r.lastListRow(), r.view())
	}
}

func TestScroll_NotFollowingKeepsViewAndShowsMarker(t *testing.T) {
	r := newRig(t)
	r.fillRows(100)
	for i := 0; i < 2; i++ {
		r.send(wheel(tea.MouseWheelUp))
	}
	r.send(key(tea.KeyPgUp, 0))
	before := r.rows()[:r.m.listHeight()-1]
	if strings.Contains(r.view(), "new rows") {
		t.Fatalf("marker without new rows:\n%s", r.view())
	}
	r.events(delta("A long streamed answer\n\nsecond paragraph\n\n"))
	r.fillRows(4)
	after := r.rows()[:r.m.listHeight()-1]
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("new rows moved the view:\n%s\n---\n%s", strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
	n := r.m.cv.total() - 100
	want := fmt.Sprintf("↓ %d new rows", n)
	if got := r.lastListRow(); got != want {
		t.Errorf("marker %q, want %q", got, want)
	}
	r.send(key(tea.KeyEnd, tea.ModCtrl))
	if !r.m.sc.follow || strings.Contains(r.view(), "new rows") || r.lastListRow() != "row 4" {
		t.Errorf("ctrl+End must resume following:\n%s", r.view())
	}
	// the wheel back to the bottom resumes too
	for i := 0; i < 3; i++ {
		r.send(wheel(tea.MouseWheelUp))
	}
	r.fillRows(1)
	for i := 0; i < 5; i++ {
		r.send(wheel(tea.MouseWheelDown))
	}
	if !r.m.sc.follow {
		t.Error("wheel down to the bottom must follow")
	}
}

func TestScroll_StreamingItemGrowsAtBottomWhileFollowing(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	for i := 1; i <= 120; i++ {
		r.events(delta(fmt.Sprintf("paragraph %d\n\n", i)))
	}
	r.events(delta("last partial"))
	rows := r.rows()
	if got := strings.TrimSpace(rows[r.m.listHeight()-2]); got != "last partial" {
		t.Errorf("row above the Progress line %q", got)
	}
	if got := r.lastListRow(); !strings.Contains(got, "Thinking…") {
		t.Errorf("last list row %q, want the Progress line (R-28)", got)
	}
	if !strings.Contains(r.view(), "paragraph 120") {
		t.Errorf("tail not visible:\n%s", r.view())
	}
}

func TestScroll_ClampAfterShrink(t *testing.T) {
	r := newRig(t)
	r.fillRows(300)
	for i := 0; i < 50; i++ {
		r.send(wheel(tea.MouseWheelUp))
	}
	r.m.ta.SetValue("/clear")
	r.send(key(tea.KeyEnter, 0))
	if !r.m.sc.follow || r.m.viewTop() != 0 {
		t.Errorf("after /clear: follow %v top %d", r.m.sc.follow, r.m.viewTop())
	}
	r.fillRows(5)
	if !strings.Contains(r.view(), "row 1") || !strings.Contains(r.view(), "row 5") {
		t.Errorf("new list after /clear:\n%s", r.view())
	}
}

func TestScroll_ClosingABigBlockClamps(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), toolStart("a", "read_file", "f"), toolEnd("a", "read_file", "f", "ok", numbered(300), nil))
	r.open(1)
	for i := 0; i < 80; i++ {
		r.send(wheel(tea.MouseWheelUp))
	}
	r.m.cv.setOpen(r.m.rec(1).item, false)
	r.send(tickMsg(r.now)) // any Update settles the view
	if !r.m.sc.follow {
		t.Errorf("the view is past the end after closing: top %d, total %d", r.m.viewTop(), r.m.cv.total())
	}
	wantFrame(t, r, 80, 40)
}

// ---------------------------------------------------------------------------
// Resize
// ---------------------------------------------------------------------------

func TestResize_RewrapsAndKeepsAnchor(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	for i := 1; i <= 60; i++ {
		r.events(warn(fmt.Sprintf("note %d: %s", i, strings.Repeat("word ", 12))))
	}
	for i := 0; i < 3; i++ {
		r.send(wheel(tea.MouseWheelUp))
	}
	anchor := r.m.sc.aItem
	r.size(40, 30)
	if r.m.sc.aItem != anchor || r.m.sc.follow {
		t.Errorf("anchor item %d -> %d, follow %v", anchor, r.m.sc.aItem, r.m.sc.follow)
	}
	for _, l := range r.m.cv.rows(0, r.m.cv.total()) {
		if w := len([]rune(l)); w > 400 {
			t.Fatalf("stale wide row: %q", l)
		}
	}
	wantFrame(t, r, 40, 30)
	if !strings.Contains(r.view(), fmt.Sprintf("note %d", anchor)) && !strings.Contains(r.view(), fmt.Sprintf("note %d", anchor-1)) {
		t.Errorf("the anchored note %d left the screen:\n%s", anchor, r.view())
	}
	r.size(120, 20)
	wantFrame(t, r, 120, 20)
	// following stays at the bottom through resizes
	r2 := newRig(t)
	r2.fillRows(100)
	r2.size(50, 15)
	if !r2.m.sc.follow || r2.lastListRow() != "row 100" {
		t.Errorf("following lost the bottom: %q", r2.lastListRow())
	}
	r2.size(90, 35)
	if r2.lastListRow() != "row 100" {
		t.Errorf("following lost the bottom after growing: %q", r2.lastListRow())
	}
}

func TestResize_NoGarbageDuringStream(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	var sent strings.Builder
	for i := 0; i < 30; i++ {
		s := fmt.Sprintf("chunk %d with some words\n", i)
		sent.WriteString(s)
		r.events(delta(s))
		r.size(30+i*5, 10+i%25)
	}
	r.events(ended(chat.FinishStop, nil))
	got := strings.Join(strings.Fields(r.convText()), " ")
	for i := 0; i < 30; i++ {
		if !strings.Contains(got, fmt.Sprintf("chunk %d with some words", i)) {
			t.Fatalf("chunk %d lost:\n%s", i, got)
		}
	}
}

// ---------------------------------------------------------------------------
// /tool N and Ctrl+O scroll into view
// ---------------------------------------------------------------------------

func TestToolCommand_OpensAndScrollsIntoView(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	for i := 1; i <= 30; i++ {
		id := fmt.Sprint("c", i)
		r.events(toolStart(id, "read_file", fmt.Sprint("file", i)), toolEnd(id, "read_file", fmt.Sprint("file", i), "ok", numbered(10), nil))
		r.fillRows(3)
	}
	if strings.Contains(r.view(), "read_file(file3)") {
		t.Fatal("block 3 should be far above the viewport")
	}
	r.m.ta.SetValue("/tool 3")
	r.send(key(tea.KeyEnter, 0))
	if !strings.Contains(r.view(), "● read_file(file3)  #3") || !strings.Contains(r.view(), "│ line 10") {
		t.Errorf("/tool 3 must open block 3 and show its head:\n%s", r.view())
	}
	if r.m.sc.follow {
		t.Error("the view should have left the tail")
	}
	r.m.ta.SetValue("/tool 1")
	r.send(key(tea.KeyEnter, 0))
	if !strings.Contains(r.view(), "● read_file(file1)  #1") {
		t.Errorf("/tool 1 far above:\n%s", r.view())
	}
	r.m.ta.SetValue("/tool 30") // the last block: back at the tail
	r.send(key(tea.KeyEnter, 0))
	if !strings.Contains(r.view(), "● read_file(file30)  #30") || !strings.Contains(r.view(), "│ line 10") {
		t.Errorf("/tool 30:\n%s", r.view())
	}
	r.m.ta.SetValue("/tool 99")
	r.send(key(tea.KeyEnter, 0))
	r.wantLine(result{}, "no tool call #99")
}

func TestCtrlO_TogglesLastBlockAndScrollsOnlyIfHidden(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), toolStart("a", "read_file", "f"), toolEnd("a", "read_file", "f", "ok", numbered(80), nil))
	r.send(key('o', tea.ModCtrl))
	if !strings.Contains(r.view(), "● read_file(f)  #1") {
		t.Errorf("the head must be on screen after opening:\n%s", r.view())
	}
	r.send(key('o', tea.ModCtrl))
	if strings.Contains(r.convText(), "line 80") {
		t.Error("second ctrl+o must close the block")
	}
}
