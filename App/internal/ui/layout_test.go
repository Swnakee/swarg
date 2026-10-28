package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (r *rig) size(w, h int) { r.send(tea.WindowSizeMsg{Width: w, Height: h}) }

func warn(s string) agent.Event { return agent.Event{Kind: agent.EvWarning, Text: s} }

// fillRows adds n one-row notices "row 1".."row n".
func (r *rig) fillRows(n int) {
	evs := make([]agent.Event, n)
	for i := range evs {
		evs[i] = warn(fmt.Sprintf("row %d", i+1))
	}
	r.events(evs...)
}

// wantFrame checks the invariants of every frame: exactly h rows, no row wider than w, the
// input box intact just above the status line.
func wantFrame(t *testing.T, r *rig, w, h int) {
	t.Helper()
	rows := strings.Split(r.m.View().Content, "\n")
	if len(rows) != h {
		t.Fatalf("%dx%d: frame has %d rows, want %d:\n%s", w, h, len(rows), h, r.view())
	}
	for i, l := range rows {
		if ansi.StringWidth(l) > w {
			t.Fatalf("%dx%d: row %d is %d cells wide: %q", w, h, i, ansi.StringWidth(l), l)
		}
	}
	plain := r.rows()
	if w < minCols || h < minRows {
		return
	}
	if !strings.HasPrefix(plain[h-2], "╰") || !strings.HasSuffix(plain[h-2], "╯") {
		t.Fatalf("%dx%d: bottom border broken: %q\n%s", w, h, plain[h-2], r.view())
	}
	top := h - 3
	for ; top >= 0 && !strings.HasPrefix(plain[top], "╭"); top-- {
		if !strings.HasPrefix(plain[top], "│") || !strings.HasSuffix(plain[top], "│") {
			t.Fatalf("%dx%d: input box side broken at row %d: %q\n%s", w, h, top, plain[top], r.view())
		}
	}
	if top < 0 || !strings.HasSuffix(plain[top], "╮") {
		t.Fatalf("%dx%d: top border missing\n%s", w, h, r.view())
	}
}

// ---------------------------------------------------------------------------
// View mode
// ---------------------------------------------------------------------------

func TestView_AltScreenAndMouseAlwaysOn(t *testing.T) {
	r := newRig(t)
	check := func(when string) {
		t.Helper()
		if v := r.m.View(); !v.AltScreen || v.MouseMode != tea.MouseModeAllMotion { // all motion: hover
			t.Errorf("%s: alt screen %v, mouse mode %v", when, v.AltScreen, v.MouseMode)
		}
	}
	check("fresh")
	r.events(started("q"), toolStart("a", "grep", "x"))
	check("with a block")
	r.events(toolEnd("a", "grep", "x", "ok", "", nil), ended(chat.FinishStop, nil))
	check("after the turn")
}

// ---------------------------------------------------------------------------
// Layout
// ---------------------------------------------------------------------------

func TestLayout_FrameIsAlwaysExactlyHeightRows(t *testing.T) {
	long := strings.Repeat("W", 500)
	scenes := map[string]func(r *rig){
		"idle":   func(r *rig) {},
		"busy":   func(r *rig) { r.events(started("q")) },
		"stream": func(r *rig) { r.events(started("q"), delta("```\n"+strings.Repeat("code "+long[:40]+"\n", 60))) },
		"blocks": func(r *rig) {
			r.events(started("q"), shellStart("a", "x"), toolOutput("a", numbered(30)+long), toolStart("b", "grep", "y"))
		},
		"ask": func(r *rig) {
			r.events(started("q"))
			r.ask(context.Background(), "edit_file", `{"path":"a","old":"1\n2\n3\n4\n5\n6\n7","new":"x"}`)
		},
		"input":    func(r *rig) { r.m.ta.SetValue("1\n2\n3\n4\n5\n6\n7\n8") },
		"stop":     func(r *rig) { r.events(started("q")); r.send(key(tea.KeyEscape, 0)) },
		"quit":     func(r *rig) { r.events(started("q")); r.send(key('c', tea.ModCtrl)) },
		"long":     func(r *rig) { r.events(started(long), warn(long), delta(long)) },
		"scrolled": func(r *rig) { r.fillRows(200); r.send(tea.MouseWheelMsg{Button: tea.MouseWheelUp}) },
	}
	for name, scene := range scenes {
		for _, shift := range []int{0} { // every height, the width cycles through the list
			for h := 5; h <= 60; h++ {
				w := []int{20, 33, 80, 200}[(h+2*shift)%4]
				r := newRig(t)
				r.size(w, h)
				scene(r)
				wantFrame(t, r, w, h)
				if t.Failed() {
					t.Fatalf("scene %q", name)
				}
			}
		}
	}
}

func TestLayout_InputAndStatusPinnedBottom(t *testing.T) {
	for h := 6; h <= 40; h++ {
		for lines := 1; lines <= 6; lines++ {
			r := newRig(t)
			r.size(80, h)
			r.fillRows(100)
			r.m.ta.SetValue(strings.TrimSuffix(strings.Repeat("x\n", lines), "\n"))
			r.send(tea.KeyPressMsg{Code: tea.KeyRight}) // let the textarea recompute its height
			rows := r.rows()
			if !strings.HasPrefix(rows[h-1], "qwen │ ") {
				t.Fatalf("h=%d lines=%d: last row is not the status: %q", h, lines, rows[h-1])
			}
			boxH := r.m.ta.Height() + 2
			if got := len(rows) - boxH - 1; r.m.listHeight() != got && h-boxH-1 >= 1 {
				t.Fatalf("h=%d lines=%d: list gets %d rows, want %d", h, lines, r.m.listHeight(), got)
			}
			wantFrame(t, r, 80, h)
		}
	}
}

func TestLayout_ListGetsTheRest(t *testing.T) {
	r := newRig(t)
	r.size(80, 20)
	r.fillRows(100)
	if got := r.m.listHeight(); got != 20-3-1 {
		t.Errorf("idle list height %d, want 16", got)
	}
	r.events(started("q"))
	if got := r.m.listHeight(); got != 20-3-1 {
		t.Errorf("busy list height %d, want 16 (the Progress line is content, not pinned; R-28)", got)
	}
	if rows := r.rows(); !strings.Contains(rows[len(rows)-5], "Thinking…") {
		t.Errorf("a full list ends with the Progress line, right above the box:\n%s", r.view())
	}
}

func TestLayout_AskReplacesInputArea(t *testing.T) {
	r := newRig(t)
	r.size(80, 30)
	r.events(started("q"), warn("list stays"))
	r.ask(context.Background(), "edit_file", `{"path":"a.go","old":"1\n2\n3\n4\n5\n6\n7","new":"x"}`)
	v := r.view()
	for _, want := range []string{"list stays", "Allow edit_file:", "a.go", "- 5", "… 2 more lines", "+ x", "[y]es / [n]o / [a]lways"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	r.events(warn("still arriving"))
	if v := r.view(); !strings.Contains(v, "still arriving") || !strings.Contains(v, "[y]es") {
		t.Errorf("list must keep updating above the prompt:\n%s", v)
	}
	r.ask(context.Background(), "write_file", `{"path":"b.txt","content":"a\nb\n"}`)
	if v := r.view(); !strings.Contains(v, "b.txt (2 lines, 4 bytes)") {
		t.Errorf("write size missing:\n%s", v)
	}
	r.size(80, 8) // a tall prompt on a short screen keeps its key line
	if v := r.view(); !strings.Contains(v, "[y]es") {
		t.Errorf("key line cut:\n%s", v)
	}
	wantFrame(t, r, 80, 8)
}

func TestLayout_TinyTerminal(t *testing.T) {
	for _, sz := range [][2]int{{1, 1}, {10, 3}, {19, 40}, {30, 4}, {80, 1}} {
		r := newRig(t)
		r.size(sz[0], sz[1])
		r.events(started("q"), delta("text"), warn("x"))
		r.m.ta.SetValue("abc")
		wantFrame(t, r, sz[0], sz[1])
		if !strings.Contains(r.view(), "terminal") && sz[0] >= 8 {
			t.Errorf("%v: no too-small notice:\n%s", sz, r.view())
		}
		r.send(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
		r.send(key(tea.KeyPgUp, 0))
	}
}

func TestLayout_CursorIsInTheInputBox(t *testing.T) {
	r := newRig(t)
	r.size(80, 24)
	r.m.ta.SetValue("ab")
	c := r.m.View().Cursor
	if c == nil || c.X != 3 || c.Y != 24-3 {
		t.Fatalf("cursor %+v, want (3, 21)", c)
	}
	r.m.ta.SetValue("a\nbcd")
	r.send(tea.KeyPressMsg{Code: tea.KeyRight})
	c = r.m.View().Cursor
	if c == nil || c.Y != 24-3 || c.X != 1+3 {
		t.Errorf("two-line input: cursor %+v, want (4, 21)", c)
	}
	r.events(started("q")) // a spinner row above the box moves nothing below it
	if c2 := r.m.View().Cursor; c2.Y != c.Y {
		t.Errorf("cursor moved with the spinner: %d -> %d", c.Y, c2.Y)
	}
}

// ---------------------------------------------------------------------------
// Wrapping in the list
// ---------------------------------------------------------------------------

func TestWrap_LongUnbrokenLinesNeverExceedTheWidth(t *testing.T) {
	r := newRig(t)
	long := strings.Repeat("abcdefghij", 50)
	cjk := strings.Repeat("宽", 200)
	r.events(started(long), delta(long+"\n\n"+cjk+"\n\n```\n"+long+"\n```\n"),
		toolStart("a", "read_file", "f"), toolEnd("a", "read_file", "f", "ok", long+"\n"+cjk+"\n", nil), warn(cjk))
	r.open(1)
	for _, l := range r.m.cv.rows(0, r.m.cv.total()) {
		if ansi.StringWidth(l) > 80 {
			t.Fatalf("row of %d cells: %q", ansi.StringWidth(l), l)
		}
	}
	if joined := strings.ReplaceAll(strings.ReplaceAll(r.convText(), "\n", ""), " ", ""); !strings.Contains(joined, long[:10]) ||
		strings.Count(joined, "宽") < 600 {
		t.Errorf("text was lost while wrapping (CJK count %d)", strings.Count(joined, "宽"))
	}
	wantFrame(t, r, 80, 40)
}

func TestView_500KBLineDoesNotExplode(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), shellStart("a", "big"), toolEnd("a", "shell", "big", "ok", strings.Repeat("y", 500<<10), nil))
	r.open(1)
	wantFrame(t, r, 80, 40)
	if got, max := r.m.cv.total(), maxLiveOutput/(80-ansi.StringWidth(outputPrefix))+10; got > max {
		t.Errorf("%d rows for one capped line, want at most %d", got, max)
	}
}

// ---------------------------------------------------------------------------
// Start: a stale terminal must not show through
// ---------------------------------------------------------------------------

func TestStart_MainModelClearsTheScreen(t *testing.T) {
	r := newRig(t)
	var res result
	r.walk(r.m.Init(), &res)
	if !res.cleared {
		t.Error("Init of the main model must return tea.ClearScreen")
	}
}

func TestStart_PickerClearsTheScreenAndUsesTheAltScreen(t *testing.T) {
	p := newPicker(pickRows())
	var res result
	(&rig{}).walk(p.Init(), &res)
	if !res.cleared {
		t.Error("Init of the picker must return tea.ClearScreen")
	}
	if !p.View().AltScreen {
		t.Error("the picker must use the alt screen")
	}
}
