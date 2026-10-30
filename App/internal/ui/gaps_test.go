package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Gap tests found by the T2_4 coverage audit
// ---------------------------------------------------------------------------

// The frame property (exactly h rows, none wider than w, input box intact) with the
// compaction spinner and its notices on screen.
func TestLayout_FrameIsExactlyHeightRowsDuringCompaction(t *testing.T) {
	long := strings.Repeat("W", 300)
	scenes := map[string]func(r *rig){
		"spinner":       func(r *rig) { r.events(compactStarted()) },
		"spinner+text":  func(r *rig) { r.fillRows(80); r.events(compactStarted()) },
		"done":          func(r *rig) { r.events(compactStarted(), compacted(3, 9000, 4000)) },
		"typing":        func(r *rig) { r.events(compactStarted()); r.m.ta.SetValue("1\n2\n3\n4\n5\n6\n7") },
		"esc":           func(r *rig) { r.events(compactStarted()); r.send(key(tea.KeyEscape, 0)) },
		"notice+stream": func(r *rig) { r.events(started(long), delta(long), compactStarted()) },
	}
	for name, scene := range scenes {
		for h := 5; h <= 60; h++ {
			w := []int{20, 33, 80, 200}[h%4]
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

// The session picker is its own full-screen model: every size gives exactly h rows, none wider than w.
func TestPicker_FrameIsExactlyHeightRowsAtEverySize(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var rows []PickRow
	for i := 0; i < 40; i++ {
		switch {
		case i%7 == 3:
			rows = append(rows, PickRow{ID: fmt.Sprintf("bad%d", i), Err: "bad json"})
		case i%5 == 0:
			rows = append(rows, PickRow{ID: fmt.Sprint(i), Title: strings.Repeat("длинный заголовок 漢字 ", 20), Model: "mlx-community/Qwen3.5-2B-4bit", Time: t0, Messages: i})
		default:
			rows = append(rows, PickRow{ID: fmt.Sprint(i), Title: fmt.Sprint("t", i), Model: "m", Time: t0.Add(time.Duration(i) * time.Minute), Messages: i})
		}
	}
	for _, filter := range []string{"", "t1", "zzz-no-match"} {
		for h := 3; h <= 50; h++ {
			for _, w := range []int{1, 5, 20, 33, 80, 200} {
				p := newPicker(rows, "", nil)
				p.Update(tea.WindowSizeMsg{Width: w, Height: h})
				if filter != "" {
					drive(p, ptype("/"))
					for _, c := range filter {
						drive(p, ptype(string(c)))
					}
				}
				for i := 0; i < 3; i++ {
					drive(p, pkey(tea.KeyDown))
				}
				got := strings.Split(p.View().Content, "\n")
				if len(got) != h {
					t.Fatalf("%dx%d filter %q: %d rows, want %d", w, h, filter, len(got), h)
				}
				for i, l := range got {
					if ansi.StringWidth(l) > w {
						t.Fatalf("%dx%d filter %q: row %d is %d cells wide", w, h, filter, i, ansi.StringWidth(l))
					}
				}
			}
		}
	}
}

// A resize while an answer streams drops the selection (the rows are re-wrapped); a new
// selection after the resize copies every word once, in order, and the stream goes on.
func TestSelect_CopyAfterResizeDuringStreaming(t *testing.T) {
	r := newRig(t)
	words := strings.Fields(strings.Repeat("lorem ipsum dolor sit amet ", 12))
	first := strings.Join(words[:30], " ")
	r.events(started("q"), delta(first))
	r.drag(0, 0, 10, 0)
	if !r.m.sel.active {
		t.Fatal("no selection before the resize")
	}
	r.size(30, 24)
	if r.m.sel.active || strings.Contains(r.m.View().Content, reverseOn) {
		t.Error("the resize kept a selection over re-wrapped rows")
	}
	rest := " " + strings.Join(words[30:], " ")
	r.events(delta(rest))
	all := first + rest
	rowsN := r.m.cv.height(1) // item 0 is the user echo
	if rowsN < 3 {
		t.Fatalf("not wrapped at 30 columns: %d rows", rowsN)
	}
	// the answer may be scrolled; select by content rows through the model's own coordinates
	r.send(key(tea.KeyHome, 0))
	var res result
	top := rowOf(r, "lorem")
	res = r.drag(0, top, 29, top+rowsN-1)
	// markdown rows are real lines (Docs/Modules/ui.md: glamour breaks are not soft), so compare words
	if got := strings.Join(strings.Fields(res.clip), " "); got != all {
		t.Errorf("copied %q\nwant   %q", got, all)
	}
	r.events(delta(" tail"))
	if !strings.Contains(r.convText(), "tail") {
		t.Error("the stream stopped after the copy")
	}
}

// Releasing the button after a resize that happened mid-drag must not panic or copy stale rows.
func TestSelect_ResizeBetweenPressAndRelease(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), delta(strings.Repeat("abc ", 60)))
	r.press(0, 0)
	r.move(20, 2)
	r.size(25, 12)
	res := r.release(20, 2)
	if strings.Contains(res.clip, "\x1b") {
		t.Errorf("clip %q holds escapes", res.clip)
	}
	wantFrame(t, r, 25, 12)
}
