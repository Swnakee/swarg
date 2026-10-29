package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"swarg/internal/chat"
	"swarg/internal/ui/mdstream"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var shellCall = chat.ToolCall{ID: "c1", Name: "shell", Args: `{"command":"x"}`}

// countMarkdown swaps the markdown renderer for a counting one.
func countMarkdown(t *testing.T) *int {
	t.Helper()
	n := 0
	old := renderMarkdown
	renderMarkdown = func(b string, w int) string { n++; return mdstream.Render(b, w) }
	t.Cleanup(func() { renderMarkdown = old })
	return &n
}

// doneTool is a finished shell call with output lines "line 1".. "line n".
func doneTool(n, lines int) callRec {
	var out []string
	for i := 1; i <= lines; i++ {
		out = append(out, fmt.Sprintf("line %d", i))
	}
	return callRec{n: n, call: shellCall, label: "ls", brief: "ok", done: true, output: strings.Join(out, "\n") + "\n[exit 0]"}
}

func text(c *conv) string { return strings.Join(plainRows(c.rows(0, c.total())), "\n") }

// ---------------------------------------------------------------------------
// Items, order, heights, rowAt
// ---------------------------------------------------------------------------

func TestConv_ItemsKeepEventOrder(t *testing.T) {
	c := newConv()
	c.addUser("question")
	c.addNotice("note")
	c.addTool(doneTool(1, 1))
	a := c.addAssistant()
	c.feed(a, "answer\n")
	got := text(c)
	last := -1
	for _, want := range []string{"question", "note", "shell(ls)", "answer"} {
		i := strings.Index(got, want)
		if i <= last {
			t.Fatalf("%q out of order in:\n%s", want, got)
		}
		last = i
	}
}

func TestConv_HeightsAndWindows(t *testing.T) {
	c := newConv()
	for i := 0; i < 30; i++ {
		c.addUser(fmt.Sprintf("message %d %s", i, strings.Repeat("w ", i)))
	}
	all := c.rows(0, c.total())
	if len(all) != c.total() {
		t.Fatalf("rows %d, total %d", len(all), c.total())
	}
	sum := 0
	for i := 0; i < c.count(); i++ {
		if c.top(i) != sum {
			t.Fatalf("top(%d) = %d, want %d", i, c.top(i), sum)
		}
		sum += c.height(i)
	}
	if sum != c.total() {
		t.Fatalf("sum of heights %d != total %d", sum, c.total())
	}
	for _, win := range [][2]int{{0, 5}, {7, 11}, {c.total() - 3, 10}, {c.total(), 4}, {-5, 3}} {
		got := c.rows(win[0], win[1])
		lo := max(win[0], 0)
		want := all[min(lo, len(all)):min(lo+win[1], len(all))]
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("window %v: got %d rows, want %d", win, len(got), len(want))
		}
	}
}

func TestConv_RowAtMapsRowsAndToolHeads(t *testing.T) {
	c := newConv()
	c.setWidth(40)
	c.addUser("u")
	t1 := c.addTool(doneTool(1, 3))
	c.addNotice("n")
	t2 := c.addTool(doneTool(2, 0))
	c.setOpen(t1, true)
	for y := 0; y < c.total(); y++ {
		idx, row, head, ok := c.rowAt(y)
		if !ok || c.top(idx)+row != y || row >= c.height(idx) {
			t.Fatalf("y=%d -> %d/%d ok=%v", y, idx, row, ok)
		}
		if head != (c.at(idx).kind == itemTool && row == 0) {
			t.Fatalf("y=%d head=%v for item %d row %d", y, head, idx, row)
		}
	}
	if _, _, _, ok := c.rowAt(c.total()); ok {
		t.Error("row past the end must not map")
	}
	if idx, _, head, _ := c.rowAt(c.top(t2)); idx != t2 || !head {
		t.Errorf("head of tool 2 = %d %v", idx, head)
	}
}

func TestConv_RowAtWrappedToolHeadIsAllHeadRows(t *testing.T) {
	c := newConv()
	c.setWidth(20)
	rec := doneTool(1, 1)
	rec.label = strings.Repeat("L", 60)
	c.addTool(rec)
	heads := 0
	for y := 0; y < c.total(); y++ {
		if _, _, head, _ := c.rowAt(y); head {
			heads++
		}
	}
	if heads < 3 {
		t.Errorf("a head wrapped over %d rows only", heads)
	}
}

func TestConv_ZeroHeightItemsAreSkipped(t *testing.T) {
	c := newConv()
	a := c.addAssistant()
	c.endAssistant(a, false)
	c.addUser("x")
	if idx, row, _, ok := c.rowAt(0); !ok || idx != 1 || row != 0 {
		t.Errorf("rowAt(0) = %d %d %v", idx, row, ok)
	}
}

// ---------------------------------------------------------------------------
// Cache
// ---------------------------------------------------------------------------

func TestConv_CachedByWidthOpenVersion(t *testing.T) {
	c := newConv()
	for i := 0; i < 10; i++ {
		c.addUser("u")
		c.addTool(doneTool(i+1, 4))
	}
	c.total()
	base := c.renders
	for i := 0; i < 20; i++ {
		c.rows(0, 15)
		c.total()
	}
	if c.renders != base {
		t.Fatalf("re-drawing re-rendered %d items", c.renders-base)
	}
	c.toggle(3)
	c.total()
	if c.renders != base+1 {
		t.Errorf("toggling one block rendered %d items", c.renders-base)
	}
	c.setWidth(50)
	c.total()
	if c.renders != base+1+c.count() {
		t.Errorf("width change rendered %d items, want all %d", c.renders-base-1, c.count())
	}
}

func TestConv_OnlyLiveItemAndRunningToolBumpVersion(t *testing.T) {
	c := newConv()
	u := c.addUser("u")
	a := c.addAssistant()
	run := c.addTool(callRec{n: 1, call: shellCall})
	idle := c.addTool(doneTool(2, 2))
	c.total()
	base := c.renders
	c.feed(a, "hello\n")
	c.total()
	if c.renders != base+1 {
		t.Errorf("a delta rendered %d items", c.renders-base)
	}
	rec := *c.at(run).rec
	rec.output = "progress\n"
	c.setTool(run, rec)
	c.total()
	if c.renders != base+2 {
		t.Errorf("tool output rendered %d items", c.renders-base-1)
	}
	if c.at(u).version != 0 || c.at(idle).version != 0 {
		t.Error("untouched items changed version")
	}
}

func TestConv_SpinnerFrameOnlyRerendersRunningTools(t *testing.T) {
	c := newConv()
	c.addUser("u")
	c.addTool(doneTool(1, 1))
	c.addTool(callRec{n: 2, call: shellCall})
	c.total()
	base := c.renders
	c.setFrame(1)
	c.total()
	if c.renders != base+1 {
		t.Errorf("frame change rendered %d items", c.renders-base)
	}
}

func TestConv_AssistantMarkdownRenderedOncePerWidth(t *testing.T) {
	n := countMarkdown(t)
	c := newConv()
	a := c.addAssistant()
	c.feed(a, "# one\n\nsome *text*\n\n")
	c.endAssistant(a, false)
	for i := 0; i < 20; i++ {
		c.total()
		c.rows(0, 10)
	}
	if *n != 1 {
		t.Fatalf("markdown rendered %d times", *n)
	}
	c.setWidth(33)
	c.total()
	if *n != 2 {
		t.Fatalf("after resize rendered %d times", *n)
	}
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

func TestStream_CommittedBlocksRenderOnce(t *testing.T) {
	n := countMarkdown(t)
	c := newConv()
	a := c.addAssistant()
	for i := 0; i < 30; i++ {
		c.feed(a, fmt.Sprintf("paragraph %d\n\n", i))
		c.total()
	}
	// 60 blocks (30 lines + 30 blanks), each rendered once; no tail is left open
	if *n > 60 {
		t.Fatalf("streaming rendered %d blocks for 60, quadratic", *n)
	}
}

func TestStream_PartialLineIsShown(t *testing.T) {
	c := newConv()
	a := c.addAssistant()
	c.feed(a, "first line\nhalf a li")
	if got := text(c); !strings.Contains(got, "first line") || !strings.Contains(got, "half a li") {
		t.Errorf("rows:\n%s", got)
	}
	c.feed(a, "ne\n")
	if got := text(c); !strings.Contains(got, "half a line") {
		t.Errorf("rows:\n%s", got)
	}
}

func TestStream_UnclosedLongFenceIsVisibleWhileStreaming(t *testing.T) {
	c := newConv()
	a := c.addAssistant()
	var b strings.Builder
	b.WriteString("```\n")
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "code line %d\n", i)
	}
	c.feed(a, b.String())
	got := text(c)
	for _, want := range []string{"code line 1", "code line 60", "code line 100"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q not visible while streaming", want)
		}
	}
}

func TestStream_EndRendersWholeFromRawText(t *testing.T) {
	n := countMarkdown(t)
	c := newConv()
	a := c.addAssistant()
	c.feed(a, "alpha\n\nbeta\n\n")
	before := *n
	c.endAssistant(a, false)
	c.total()
	if *n != before+1 {
		t.Errorf("end rendered %d times, want once as a whole", *n-before)
	}
	if c.at(a).text != "alpha\n\nbeta\n\n" {
		t.Errorf("raw text = %q", c.at(a).text)
	}
	c.feed(a, "late\n")
	if strings.Contains(text(c), "late") {
		t.Error("a finished item must not grow")
	}
}

func TestStream_InterruptClosesFenceAndMarks(t *testing.T) {
	c := newConv()
	a := c.addAssistant()
	c.feed(a, "intro\n\n```go\nfunc main() {\n")
	c.endAssistant(a, true)
	raw := c.at(a).text
	if !strings.HasSuffix(raw, "```\n") {
		t.Errorf("fence not closed: %q", raw)
	}
	rows := plainRows(c.rows(0, c.total()))
	if rows[len(rows)-1] != interruptedText {
		t.Errorf("last row = %q", rows[len(rows)-1])
	}
	if !strings.Contains(strings.Join(rows, "\n"), "func main()") {
		t.Error("text shown before the interrupt is gone")
	}
}

func TestStream_InterruptWithoutFenceKeepsText(t *testing.T) {
	c := newConv()
	a := c.addAssistant()
	c.feed(a, "partial answer without newline")
	c.endAssistant(a, true)
	if c.at(a).text != "partial answer without newline" {
		t.Errorf("raw text changed: %q", c.at(a).text)
	}
	if got := text(c); !strings.Contains(got, "partial answer") || !strings.Contains(got, interruptedText) {
		t.Errorf("rows:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// Tool items
// ---------------------------------------------------------------------------

func TestConv_OpenToolShowsFullOutputWrapped(t *testing.T) {
	c := newConv()
	c.setWidth(30)
	rec := doneTool(1, 300)
	rec.output += "\n" + strings.Repeat("W", 100)
	i := c.addTool(rec)
	closed := c.height(i)
	if closed > 6 {
		t.Errorf("closed block has %d rows", closed)
	}
	c.setOpen(i, true)
	got := text(c)
	if !strings.Contains(got, "line 300") || !strings.Contains(got, "line 16") {
		t.Error("an open block must show all output (no 15-line cap)")
	}
	if strings.Contains(got, "more lines") {
		t.Error("an open block hides nothing")
	}
	wantFit(t, c.rows(0, c.total()), 30)
	if strings.Count(got, "W") != 100 {
		t.Error("long output line lost characters when wrapped")
	}
}

func TestConv_ContinuationRowsKeepOutputPrefix(t *testing.T) {
	c := newConv()
	c.setWidth(20)
	rec := doneTool(1, 0)
	rec.output = strings.Repeat("q", 50)
	c.addTool(rec)
	c.setOpen(0, true)
	n := 0
	for _, r := range plainRows(c.rows(0, c.total())) {
		if strings.Contains(r, "q") {
			n++
			if !strings.HasPrefix(r, outputPrefix) {
				t.Errorf("row %q lacks the output prefix", r)
			}
		}
	}
	if n < 3 {
		t.Errorf("output wrapped into %d rows", n)
	}
}

func TestConv_ToggleOnNonToolIsNoop(t *testing.T) {
	c := newConv()
	u := c.addUser("x")
	if c.toggle(u) || c.toggle(99) {
		t.Error("toggle on a non-tool must report false")
	}
}

// ---------------------------------------------------------------------------
// Resize
// ---------------------------------------------------------------------------

func TestResize_RewrapsWithoutStaleRows(t *testing.T) {
	c := mixedConv()
	c.setWidth(120)
	wide := c.total()
	c.setWidth(40)
	rows := c.rows(0, c.total())
	wantFit(t, rows, 40)
	if len(rows) <= wide {
		t.Errorf("rows at 40 (%d) should exceed rows at 120 (%d)", len(rows), wide)
	}
	c.setWidth(120)
	if c.total() != wide {
		t.Errorf("back at 120 got %d rows, want %d", c.total(), wide)
	}
	wantFit(t, c.rows(0, c.total()), 120)
}

func TestResize_LiveItemKeepsAllText(t *testing.T) {
	c := newConv()
	a := c.addAssistant()
	var want strings.Builder
	for i := 0; i < 12; i++ {
		s := fmt.Sprintf("sentence number %d of the streamed answer\n\n", i)
		want.WriteString(s)
		c.feed(a, s)
		c.setWidth(25 + i*7%50)
		c.total()
	}
	c.endAssistant(a, false)
	if c.at(a).text != want.String() {
		t.Fatal("raw text differs from the text sent")
	}
	c.setWidth(60)
	if !strings.Contains(strings.Join(strings.Fields(text(c)), " "), "sentence number 11 of the streamed answer") {
		t.Error("text lost")
	}
}

// ---------------------------------------------------------------------------
// Scale
// ---------------------------------------------------------------------------

// bigConv has n items of mixed kinds (tool and user, so the test is not dominated by Glamour).
func bigConv(n int) *conv {
	c := newConv()
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			c.addUser(fmt.Sprintf("message %d", i))
		case 1:
			c.addTool(doneTool(i, 3))
		default:
			c.addNotice(fmt.Sprintf("note %d", i))
		}
	}
	return c
}

func TestScale_AppendMeasuresEachItemOnce(t *testing.T) {
	c := newConv()
	for i := 0; i < 2000; i++ {
		c.addUser("m")
		c.total()
	}
	if c.renders != 2000 {
		t.Errorf("%d renders for 2000 appends: quadratic", c.renders)
	}
}

func TestScale_WindowRendersOnlyWhatItNeeds(t *testing.T) {
	c := bigConv(5000)
	c.rows(0, 40)
	if c.renders > 40 {
		t.Errorf("a window of 40 rows at the top rendered %d items", c.renders)
	}
	c.total()
	base := c.renders
	for off := 0; off < c.total(); off += 997 {
		c.rows(off, 40)
	}
	if c.renders != base {
		t.Errorf("scrolling through a measured list rendered %d items", c.renders-base)
	}
}

func TestScale_TailAfterResizeTouchesOnlyTheTail(t *testing.T) {
	c := bigConv(5000)
	c.total()
	c.setWidth(61)
	base := c.renders
	rows, first, row := c.tailRows(30)
	if len(rows) != 30 {
		t.Fatalf("tail has %d rows", len(rows))
	}
	if d := c.renders - base; d > 31 {
		t.Errorf("tail rendered %d items", d)
	}
	if first < 4900 || row < 0 {
		t.Errorf("first=%d row=%d", first, row)
	}
	all := c.rows(0, c.total())
	if strings.Join(rows, "|") != strings.Join(all[len(all)-30:], "|") {
		t.Error("tailRows differs from the last rows of the list")
	}
	if c.top(first)+row != c.total()-30 {
		t.Errorf("first row maps to %d, want %d", c.top(first)+row, c.total()-30)
	}
}

func TestScale_TailOfShortListIsAllRows(t *testing.T) {
	c := newConv()
	c.addUser("only")
	rows, first, row := c.tailRows(50)
	if len(rows) != 1 || first != 0 || row != 0 {
		t.Errorf("rows=%d first=%d row=%d", len(rows), first, row)
	}
	if rows, _, _ := newConv().tailRows(5); len(rows) != 0 {
		t.Error("empty list has no rows")
	}
}

func BenchmarkConv_Window(b *testing.B) {
	c := bigConv(5000)
	c.total()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.rows((i*131)%c.total(), 40)
	}
}

func BenchmarkConv_StreamingFrame(b *testing.B) {
	c := bigConv(5000)
	a := c.addAssistant()
	c.total()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.feed(a, "streamed words ")
		c.tailRows(40)
	}
}

// Bug (found in W2.2 review): the output prefix width was counted in bytes ("│" is 3 bytes),
// so tool output wrapped 2 cells before the edge. A line that fits exactly must not wrap.
func TestBlockRows_OutputUsesTheFullWidth(t *testing.T) {
	const width = 40
	line := strings.Repeat("x", width-ansi.StringWidth(outputPrefix)) // exactly fills the row
	r := callRec{n: 1, call: chat.ToolCall{Name: "shell"}, label: "shell(x)", done: true, brief: "exit 0", output: line + "\n[exit 0]"}
	rows, _ := blockRowsAt(r, true, 100, []string{line}, width, 0)
	n := 0
	for _, row := range rows {
		if strings.Contains(ansi.Strip(row), "│") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("a line of exactly the free width took %d rows, want 1:\n%s", n, strings.Join(rows, "\n"))
	}
}

// ---------------------------------------------------------------------------
// Task background survives the resets inside a row (R-62)
// ---------------------------------------------------------------------------

func TestWithTaskBackground_ReappliedAfterEveryReset(t *testing.T) {
	row := "\x1b[1;38;2;255;0;0mhead\x1b[m plain \x1b[32mgreen\x1b[0m tail"
	got := withTaskBackground(row, 30)
	for _, reset := range []string{"\x1b[m", "\x1b[0m"} {
		for _, part := range strings.Split(got, reset)[1:] {
			if part != "" && !strings.HasPrefix(part, taskBgSeq) { // the final reset ends the row
				t.Errorf("after %q the background is not re-applied: %q", reset, part)
			}
		}
	}
	if !strings.HasPrefix(got, taskBgSeq) || !strings.HasSuffix(got, "\x1b[m") || ansi.StringWidth(got) != 30 {
		t.Errorf("row = %q, want background first, reset last, width 30", got)
	}
	if got := rgbSeq(48, "#121212"); got != "\x1b[48;2;18;18;18m" {
		t.Errorf("rgbSeq = %q", got)
	}
}
