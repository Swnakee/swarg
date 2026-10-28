package ui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func toolOutput(id, text string) agent.Event {
	return agent.Event{Kind: agent.EvToolOutput, Call: chat.ToolCall{ID: id}, Text: text}
}

// shellStart is a running shell call.
func shellStart(id, command string) agent.Event {
	return agent.Event{Kind: agent.EvToolStart, Call: chat.ToolCall{ID: id, Name: "shell", Args: `{"command":"` + command + `"}`}, Label: command}
}

// numbered returns n output lines "line 1\n" ... "line n\n".
func numbered(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// rowOf is the index of the screen row containing head, -1 when there is none.
func rowOf(r *rig, head string) int {
	for i, l := range r.rows() {
		if strings.Contains(l, head) {
			return i
		}
	}
	return -1
}

// open expands the block numbered n.
func (r *rig) open(n int) { r.m.cv.setOpen(r.m.rec(n).item, true) }

// ---------------------------------------------------------------------------
// Blocks in the list
// ---------------------------------------------------------------------------

func TestConv_ToolBlocksAreListRowsNotPrinted(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	res := r.events(toolStart("a", "read_file", "README.md"), toolEnd("a", "read_file", "README.md", "42 lines", "out", nil))
	if len(res.raws) != 0 || res.cleared {
		t.Errorf("full screen prints nothing: %q", res.raws)
	}
	v := r.view()
	if !strings.Contains(v, "● read_file(README.md)  #1") || !strings.Contains(v, "└ 42 lines") {
		t.Errorf("block not in the view:\n%s", v)
	}
}

func TestConv_OrderFollowsEventOrder(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), delta("before\n\n"), toolStart("a", "read_file", "f"), toolEnd("a", "read_file", "f", "1 line", "x", nil),
		delta("after\n\n"))
	got := r.convText()
	last := -1
	for _, want := range []string{"before", "● read_file(f)", "└ 1 line", "after"} {
		i := strings.Index(got, want)
		if i < 0 || i < last {
			t.Fatalf("%q out of order in:\n%s", want, got)
		}
		last = i
	}
}

func TestConv_ItemKinds(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), delta("answer\n"), toolStart("a", "grep", "x"), toolEnd("a", "grep", "x", "ok", "", nil),
		agent.Event{Kind: agent.EvWarning, Text: "careful"}, agent.Event{Kind: agent.EvUserText, Text: "also"})
	var kinds []itemKind
	for i := 0; i < r.m.cv.count(); i++ {
		kinds = append(kinds, r.m.cv.at(i).kind)
	}
	want := []itemKind{itemUser, itemAssistant, itemTool, itemNotice, itemUser}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Errorf("kinds %v, want %v", kinds, want)
	}
}

func TestConv_RunningShellShowsLastFiveLines(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), shellStart("a", "build"))
	// chunks arrive raw: mid-line, mid-UTF-8 (the two bytes of "б"), with CR progress and CRLF
	r.events(toolOutput("a", "l1\nl2\r\nl3\nprogress 10%\rprogress 100%\nl5\nl6 \xd0"))
	r.events(toolOutput("a", "\xb1\nl7"))
	v := r.view()
	for _, want := range []string{"│ l3", "│ progress 100%", "│ l5", "│ l6 б", "│ l7"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	for _, bad := range []string{"l1", "l2", "10%", "\r"} {
		if strings.Contains(v, bad) {
			t.Errorf("view contains %q:\n%s", bad, v)
		}
	}
	if !strings.Contains(v, "● shell(build)  #1 ") || strings.Contains(v, "└") {
		t.Errorf("running marker or result line wrong:\n%s", v)
	}
}

func TestConv_OutputReplacedByFinalOutputOnEnd(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), shellStart("a", "build"), toolOutput("a", "streamed"))
	r.events(toolEnd("a", "shell", "build", "exit 0", "final output", nil))
	r.open(1)
	if v := r.view(); !strings.Contains(v, "final output") || strings.Contains(v, "streamed") {
		t.Errorf("final output must replace the stream:\n%s", v)
	}
}

func TestConv_OutputCap256KB(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), shellStart("a", "big"))
	chunk := strings.Repeat("é", 1000) // 2 bytes per rune
	for i := 0; i < 300; i++ {
		r.events(toolOutput("a", chunk))
	}
	r.events(toolOutput("a", "\nTAIL"))
	out := r.m.calls[0].output
	if maxLiveOutput != 256<<10 || len(out) > maxLiveOutput || len(out) < maxLiveOutput-2 || !utf8.ValidString(out) || !strings.HasSuffix(out, "\nTAIL") {
		t.Errorf("output %d bytes (cap %d), valid %v", len(out), maxLiveOutput, utf8.ValidString(out))
	}
	end := strings.Repeat("x", maxLiveOutput+10)
	r.events(toolEnd("a", "shell", "big", "ok", end, nil))
	if got := len(r.m.calls[0].output); got != maxLiveOutput {
		t.Errorf("final output kept %d bytes, want %d", got, maxLiveOutput)
	}
}

func TestConv_ManyChunksDoNotCopyQuadratically(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), shellStart("a", "big"))
	chunk := strings.Repeat("x", 100) + "\n"
	evs := make([]agent.Event, 5000)
	for i := range evs {
		evs[i] = toolOutput("a", chunk)
	}
	r.events(evs...) // one batch: the buffer is appended to, the record is synced once
	if got := len(r.m.outBuf[1]); got > 2*maxLiveOutput {
		t.Errorf("buffer grew to %d bytes", got)
	}
	if got := len(r.m.calls[0].output); got != maxLiveOutput && got != maxLiveOutput-1 {
		// 5000 * 101 = 505 KB: capped to the last 256 KB, cut at a rune start
		t.Errorf("record kept %d bytes", got)
	}
}

func TestConv_ProgressIsSanitized(t *testing.T) {
	if got := outputLines("a\tb\x1b[31mred\x1b[0m\nbar 1%\rbar 99%\r\n"); len(got) != 2 || got[0] != "a    b[31mred[0m" || got[1] != "bar 99%" {
		t.Errorf("outputLines = %q", got)
	}
	r := newRig(t)
	r.events(started("q"), shellStart("a", "x"), toolOutput("a", "ok\n"))
	r.events(toolOutput("a", strings.Repeat("W", 500)+"\n\x1b]0;title\x07\x1b[2J\n"))
	for _, l := range strings.Split(r.m.View().Content, "\n") {
		if ansi.StringWidth(l) > r.m.width {
			t.Errorf("row wider than the screen (%d): %q", ansi.StringWidth(l), l)
		}
	}
	if strings.Contains(r.m.View().Content, "\x1b]0;title") || strings.Contains(r.m.View().Content, "\x1b[2J") {
		t.Error("terminal escapes from tool output reached the view")
	}
}

func TestConv_UnknownOrLateOutputIgnored(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), shellStart("a", "x"))
	r.events(toolOutput("nope", "ghost"))
	r.events(toolEnd("a", "shell", "x", "exit 0", "done", nil))
	r.events(toolOutput("a", "late"))
	if got := r.m.calls[0].output; got != "done" {
		t.Errorf("output = %q, want %q", got, "done")
	}
	if strings.Contains(r.view(), "ghost") || strings.Contains(r.view(), "late") {
		t.Error("stray output reached the view")
	}
}

func TestConv_TurnEndEndsRunningBlock(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), shellStart("a", "sleep 99"), toolOutput("a", "partial\n"))
	r.events(ended(chat.FinishInterrupted, nil))
	got := r.convText()
	if !strings.Contains(got, "● shell(sleep 99)") || !strings.Contains(got, "◼ cancelled") {
		t.Errorf("aborted call must end as cancelled:\n%s", got)
	}
	if len(r.m.running()) != 0 || !r.m.calls[0].done {
		t.Errorf("running %d, done %v", len(r.m.running()), r.m.calls[0].done)
	}
}

func TestConv_ClearEmptiesTheList(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), toolStart("a", "grep", "x"), toolEnd("a", "grep", "x", "ok", "", nil), ended(chat.FinishStop, nil))
	r.events(started("again"), toolStart("b", "grep", "y"), toolEnd("b", "grep", "y", "ok", "", nil))
	r.m.ta.SetValue("/clear")
	res := r.send(key(tea.KeyEnter, 0))
	if r.m.cv.count() != 0 || strings.Contains(r.view(), "grep(y)") || len(res.raws) != 0 || res.cleared {
		t.Errorf("items %d raws %q cleared %v:\n%s", r.m.cv.count(), res.raws, res.cleared, r.view())
	}
	r.events(started("fresh"), toolStart("c", "grep", "z"))
	if !strings.Contains(r.view(), "grep(z)  #1") {
		t.Errorf("numbering must restart after /clear:\n%s", r.view())
	}
}

// ---------------------------------------------------------------------------
// Output preview of a closed, done shell block
// ---------------------------------------------------------------------------

func TestLive_DoneShellShowsOutputPreview(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), shellStart("a", "python3 hello.py"))
	r.events(toolEnd("a", "shell", "python3 hello.py", "exit 0", "Здравствуйте! Я Python.\n[exit 0]", nil))
	v := r.view()
	if rowOf(r, "│ Здравствуйте") != rowOf(r, "└ exit 0")+1 {
		t.Errorf("preview must sit right under the result line:\n%s", v)
	}
	if strings.Contains(v, "[exit 0]") {
		t.Errorf("status line leaked into the preview:\n%s", v)
	}
}

func TestLive_PreviewCapsAtThreeLinesWithMore(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), shellStart("a", "seq"), toolEnd("a", "shell", "seq", "exit 0", numbered(5)+"[exit 0]", nil))
	v := r.view()
	for _, want := range []string{"│ line 1", "│ line 2", "│ line 3", "… +2 lines (/tool 1)"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "line 4") {
		t.Errorf("more than 3 lines previewed:\n%s", v)
	}
}

func TestLive_PreviewSkipsStatusLine(t *testing.T) {
	cases := []struct{ name, out string }{
		{"exit", "only\n[exit 0]"},
		{"killed", "only\n[killed: timeout]"},
	}
	for _, tc := range cases {
		r := newRig(t)
		r.events(started("q"), shellStart("a", "x"), toolEnd("a", "shell", "x", "exit 0", tc.out, nil))
		v := r.view()
		if !strings.Contains(v, "│ only") || strings.Contains(v, "[") || strings.Contains(v, "… +") {
			t.Errorf("%s: status line not skipped:\n%s", tc.name, v)
		}
	}
	// no output besides the status line: no preview rows at all
	r := newRig(t)
	r.events(started("q"), shellStart("a", "true"), toolEnd("a", "shell", "true", "exit 0", "\n[exit 0]", nil))
	if v := r.view(); strings.Contains(v, "  │ ") {
		t.Errorf("empty output must have no preview:\n%s", v)
	}
}

func TestLive_NonShellHasNoPreview(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), toolStart("a", "read_file", "f"), toolEnd("a", "read_file", "f", "3 lines", "x\ny\nz\n", nil))
	if v := r.view(); strings.Contains(v, "│ x") {
		t.Errorf("read_file got a preview:\n%s", v)
	}
	r = newRig(t)
	r.events(started("q"), shellStart("a", "bad"), toolEnd("a", "shell", "bad", "", "oops\n", fmt.Errorf("boom")))
	if v := r.view(); strings.Contains(v, "│ oops") {
		t.Errorf("failed shell got a preview:\n%s", v)
	}
}

func TestConv_OpenBlockShowsAllOutput(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), shellStart("a", "seq"), toolEnd("a", "shell", "seq", "exit 0", numbered(100), nil))
	r.open(1)
	got := r.convText()
	if !strings.Contains(got, "│ line 1\n") || !strings.Contains(got, "│ line 100") || strings.Contains(got, "more lines") {
		t.Errorf("an open block shows every output line:\n%s", got)
	}
}
