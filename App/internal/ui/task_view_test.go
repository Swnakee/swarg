package ui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// taskCall is the `task` call as the agent reports it in a Task event.
func taskCall(id string) chat.ToolCall {
	return chat.ToolCall{ID: id, Name: taskToolName, Args: `{"goal":"x"}`}
}

// taskOpen is the pair of events that begins a Task: the call's block, then the Task itself.
func taskOpen(callID, taskID, goal string, depth int) []agent.Event {
	return []agent.Event{
		toolStart(callID, taskToolName, goal),
		{Kind: agent.EvTaskStarted, Task: taskID, Depth: depth, Text: goal, Call: taskCall(callID)},
	}
}

// taskClose is the pair of events that ends a Task: the Task, then the call's result.
func taskClose(callID, taskID, goal, report string) []agent.Event {
	return []agent.Event{
		{Kind: agent.EvTaskEnded, Task: taskID, Brief: firstLine(report), Output: report},
		toolEnd(callID, taskToolName, goal, firstLine(report), report, nil),
	}
}

// inTask tags events as emitted inside the Task.
func inTask(task string, evs ...agent.Event) []agent.Event {
	for i := range evs {
		evs[i].Task = task
	}
	return evs
}

func cat(parts ...[]agent.Event) []agent.Event {
	var out []agent.Event
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// taskRig runs a whole Task: call #1 is the task block, #2 the shell call inside it, the Session
// answers "final answer" afterwards. All blocks are closed, nothing is scrolled.
func taskRig(t *testing.T) *rig {
	r := newRig(t)
	r.events(cat(
		[]agent.Event{started("do it")},
		taskOpen("tk", "t1", "fix tests", 1),
		inTask("t1", delta("child says hi")),
		inTask("t1", toolStart("c1", "shell", "go test"), toolEnd("c1", "shell", "go test", "", "ok\n[exit 0]\n", nil)),
		taskClose("tk", "t1", "fix tests", "fixed it\nsecond line"),
		[]agent.Event{delta("final answer"), ended(chat.FinishStop, nil)},
	)...)
	return r
}

// gutterOf is the Task gutter n levels deep, as plain text.
func gutterOf(n int) string { return strings.Repeat(taskGutter, n) }

// ---------------------------------------------------------------------------
// Folding
// ---------------------------------------------------------------------------

func TestTaskBlock_ClosedHidesChildren(t *testing.T) {
	r := taskRig(t)
	text := r.convText()
	for _, want := range []string{"● task(fix tests)  #1", "└ fixed it", "final answer"} {
		if !strings.Contains(text, want) {
			t.Errorf("closed Task lacks %q:\n%s", want, text)
		}
	}
	for _, hidden := range []string{"child says hi", "shell(go test)", "second line"} {
		if strings.Contains(text, hidden) {
			t.Errorf("closed Task shows %q:\n%s", hidden, text)
		}
	}
	task := r.m.rec(1).item
	for i, want := range []struct{ depth, parent int }{{0, -1}, {0, -1}, {1, task}, {1, task}, {0, -1}} {
		if it := r.m.cv.at(i); it.depth != want.depth || it.parent != want.parent {
			t.Errorf("item %d: depth %d parent %d, want %d %d", i, it.depth, it.parent, want.depth, want.parent)
		}
	}
	if h := r.m.cv.height(task + 1); h != 0 {
		t.Errorf("hidden child has %d rows", h)
	}
}

func TestTaskBlock_OpenShowsChildrenWithGutter(t *testing.T) {
	r := taskRig(t)
	r.open(1)
	rows := plainRows(r.m.cv.rows(0, r.m.cv.total()))
	at := func(part string) int {
		for i, row := range rows {
			if strings.Contains(row, part) {
				return i
			}
		}
		t.Fatalf("no row with %q in:\n%s", part, strings.Join(rows, "\n"))
		return -1
	}
	if !strings.HasPrefix(rows[at("child says hi")], gutterOf(1)) || !strings.HasPrefix(rows[at("shell(go test)")], gutterOf(1)+"● ") {
		t.Errorf("children lack the gutter:\n%s", strings.Join(rows, "\n"))
	}
	if strings.HasPrefix(rows[at("final answer")], taskGutter) || strings.HasPrefix(rows[at("task(fix tests)")], taskGutter) {
		t.Errorf("the Session's rows got a gutter:\n%s", strings.Join(rows, "\n"))
	}
	if !(at("task(fix tests)") < at("child says hi") && at("shell(go test)") < at("final answer")) {
		t.Errorf("children are not between the block and the answer:\n%s", strings.Join(rows, "\n"))
	}
	for _, row := range rows {
		if w := len([]rune(row)); w > 80 {
			t.Errorf("row wider than the screen (%d): %q", w, row)
		}
	}
	r.m.cv.setOpen(1, false)
	if strings.Contains(r.convText(), "child says hi") {
		t.Error("closing the block did not hide the children again")
	}
}

func TestTaskBlock_ResultLineUnderHead(t *testing.T) {
	r := taskRig(t)
	for _, open := range []bool{false, true} {
		r.m.cv.setOpen(r.m.rec(1).item, open)
		rows := plainRows(r.m.cv.rows(0, r.m.cv.total()))
		head := -1
		for i, row := range rows {
			if strings.Contains(row, "● task(fix tests)") {
				head = i
			}
		}
		if head < 0 || !strings.HasPrefix(rows[head+1], "  └ fixed it") {
			t.Fatalf("open=%v: the result line is not right under the head:\n%s", open, strings.Join(rows, "\n"))
		}
		if open && !strings.Contains(rows[head+2], "fixed it") { // the report follows the result line
			t.Errorf("open block: output after the result line missing:\n%s", strings.Join(rows, "\n"))
		}
	}
}

func TestTaskBlock_ClickTogglesByHead(t *testing.T) {
	r := taskRig(t)
	r.open(1)
	y := r.rowIndex("shell(go test)")
	if y < 0 {
		t.Fatalf("no child head:\n%s", r.view())
	}
	r.hoverTo(gw-1, y) // on the gutter
	if got := underlined(r.rawRows()); len(got) != 0 {
		t.Errorf("the gutter is underlined: %v", got)
	}
	r.hoverTo(gw, y)
	if got := underlined(r.rawRows()); len(got) != 1 || got[0] != y {
		t.Errorf("underlined rows %v, want [%d]", got, y)
	}
	child := r.m.rec(2).item
	r.click(gw+3, y)
	if !r.isOpen(child) {
		t.Error("a click on the child's head did not open it")
	}
	if !strings.Contains(r.view(), gutterOf(1)+outputPrefix+"ok") {
		t.Errorf("the opened child shows no output under the gutter:\n%s", r.view())
	}
}

func TestTaskBlock_NestedTaskClosedByDefault(t *testing.T) {
	r := newRig(t)
	r.events(cat(
		[]agent.Event{started("go")},
		taskOpen("tk1", "t1", "outer", 1),
		taskOpen("tk2", "t2", "inner", 2),
		inTask("t2", delta("deep text")),
		taskClose("tk2", "t2", "inner", "inner done"),
		taskClose("tk1", "t1", "outer", "outer done"),
	)...)
	outer, inner := r.m.rec(1).item, r.m.rec(2).item
	if it := r.m.cv.at(inner); it.depth != 1 || it.parent != outer {
		t.Fatalf("inner block depth %d parent %d, want 1 %d", it.depth, it.parent, outer)
	}
	if text := r.convText(); strings.Contains(text, "inner") || strings.Contains(text, "deep text") {
		t.Fatalf("a closed outer Task shows its work:\n%s", text)
	}
	r.open(1)
	text := r.convText()
	if !strings.Contains(text, gutterOf(1)+"● task(inner)") || strings.Contains(text, "deep text") {
		t.Errorf("outer open, inner closed:\n%s", text)
	}
	r.open(2)
	if text := r.convText(); !strings.Contains(text, gutterOf(2)) || !strings.Contains(text, gutterOf(2)+"  deep text") { // the text keeps its own margin
		t.Errorf("both open, the deep text lacks two gutters:\n%s", text)
	}
	r.m.cv.setOpen(outer, false) // the inner state is kept, but it is hidden again
	if text := r.convText(); strings.Contains(text, "inner") || !r.isOpen(inner) {
		t.Errorf("closing the outer block:\n%s", text)
	}
}

// ---------------------------------------------------------------------------
// Scrolling and selection
// ---------------------------------------------------------------------------

func TestTaskBlock_HiddenItemsAtListEndScroll(t *testing.T) {
	r := newRig(t)
	r.size(80, 20)
	r.fillRows(60)
	r.events(append([]agent.Event{started("q")}, taskOpen("tk", "t1", "work", 1)...)...)
	total := r.m.cv.total()
	r.events(inTask("t1", delta("hidden one"), toolStart("c1", "shell", "ls"), delta("hidden two"))...)
	if got := r.m.cv.total(); got != total {
		t.Fatalf("hidden items changed the height: %d -> %d", total, got)
	}
	rows := r.rows()
	last := rows[r.m.listHeight()-1]
	if !strings.Contains(last, "Running") || !strings.Contains(rows[r.m.listHeight()-2], "● task(work)  #1") {
		t.Errorf("the view does not end with the Task head and the Progress line:\n%s", r.view())
	}
	if strings.Contains(r.view(), "hidden") {
		t.Errorf("hidden text on screen:\n%s", r.view())
	}
	r.m.scrollBy(-10) // not following: hidden arrivals are not "new rows"
	r.events(inTask("t1", delta(" more"))...)
	if n := r.m.newRows(); n != 0 {
		t.Errorf("hidden text counted as %d new rows", n)
	}
	r.send(key(tea.KeyEnd, tea.ModCtrl))
	if rows := r.rows(); !strings.Contains(rows[r.m.listHeight()-1], "Running") {
		t.Errorf("ctrl+end does not reach the end:\n%s", r.view())
	}
}

func TestTaskBlock_SelectionAcrossClosedTask(t *testing.T) {
	r := taskRig(t)
	n := r.m.cv.count()
	r.m.sel = selection{active: true, a: selPoint{1, 0, 0}, b: selPoint{n - 1, 0, 40}}
	got, _ := r.m.selectionText()
	for _, want := range []string{"● task(fix tests)  #1", "\nfixed it\n", "final answer"} { // a copy drops the result gutter
		if !strings.Contains(got, want) {
			t.Errorf("selection lacks %q: %q", want, got)
		}
	}
	for _, hidden := range []string{"child says hi", "go test", "ok"} {
		if strings.Contains(got, hidden) {
			t.Errorf("selection holds the hidden %q: %q", hidden, got)
		}
	}
	r.send(tea.WindowSizeMsg{Width: 80, Height: 40})
	if v := r.m.View().Content; !strings.Contains(v, reverseOn) {
		t.Error("the selection is not highlighted")
	}
}

func TestTaskBlock_CopyDropsGutterAndToolGutter(t *testing.T) {
	r := taskRig(t)
	r.open(1)
	r.open(2)
	child := r.m.rec(2).item
	rows := plainRows(r.m.cv.itemRows(child).rows)
	if len(rows) < 2 || !strings.HasPrefix(rows[1], gutterOf(1)+outputPrefix+"ok") {
		t.Fatalf("child rows: %q", rows)
	}
	r.m.sel = selection{active: true, a: selPoint{child, 1, 0}, b: selPoint{child, 1, 79}}
	if got, _ := r.m.selectionText(); got != "ok" {
		t.Errorf("copy of an output row of a child block = %q, want %q", got, "ok")
	}
	text := r.m.rec(2).item - 1 // the child's assistant text
	r.m.sel = selection{active: true, a: selPoint{text, 0, 0}, b: selPoint{text, 0, 79}}
	if got, _ := r.m.selectionText(); got != "child says hi" {
		t.Errorf("copy of a child text row = %q, want %q", got, "child says hi")
	}
	r.m.sel = selection{active: true, a: selPoint{child, 0, 0}, b: selPoint{child, 0, 79}}
	if got, _ := r.m.selectionText(); !strings.HasPrefix(got, "● shell(go test)") {
		t.Errorf("copy of a child head = %q: the head glyph stays, the gutter goes", got)
	}
}

// ---------------------------------------------------------------------------
// Progress line, approval, panel
// ---------------------------------------------------------------------------

func TestTaskBlock_ProgressLineShowsPath(t *testing.T) {
	r := newRig(t)
	r.events(cat([]agent.Event{started("q")}, taskOpen("tk", "t1", "fix tests", 1))...)
	if v := r.view(); !strings.Contains(v, "Running task(fix tests)…") {
		t.Errorf("only the Task runs:\n%s", v)
	}
	r.events(inTask("t1", toolStart("c1", "shell", "go test"))...)
	if v := r.view(); !strings.Contains(v, "Running task(fix tests) › shell(go test)…") {
		t.Errorf("one call in the Task:\n%s", v)
	}
	r.events(inTask("t1", toolStart("c2", "grep", "x"))...)
	if v := r.view(); !strings.Contains(v, "Running task(fix tests) › 2 tools…") {
		t.Errorf("two calls in the Task:\n%s", v)
	}
	r.events(taskOpen("tk2", "t2", "inner", 2)[0]) // a nested task call, not yet a Task
	r.events(inTask("t1", toolEnd("c1", "shell", "go test", "", "", nil), toolEnd("c2", "grep", "x", "", "", nil))...)
	r.events(agent.Event{Kind: agent.EvTaskStarted, Task: "t2", Depth: 2, Text: "inner", Call: taskCall("tk2")})
	r.events(inTask("t2", toolStart("c3", "read_file", "a.go"))...)
	if v := r.view(); !strings.Contains(v, "Running task(fix tests) › task(inner) › read_file(a.go)…") {
		t.Errorf("nested path:\n%s", v)
	}
}

func TestTaskBlock_AskPromptNamesTask(t *testing.T) {
	if got := askPrompt(chat.ToolCall{Name: "shell", Args: `{"command":"ls"}`}, nil, ""); strings.Contains(got, "task") {
		t.Errorf("a Session prompt names a task: %q", got)
	}
	r := newRig(t)
	r.events(cat([]agent.Event{started("q")}, taskOpen("tk", "t1", "fix tests", 1))...)
	r.ask(context.Background(), "shell", `{"command":"rm x"}`)
	if v := r.view(); !strings.Contains(v, "Allow shell (task: fix tests): rm x?") {
		t.Errorf("the prompt does not name the Task:\n%s", v)
	}
}

func TestTaskBlock_ChildMeterIgnoredByPanel(t *testing.T) {
	r := newRig(t)
	r.events(agent.Event{Kind: agent.EvMeter, Used: 5600, Limit: 16000})
	before := r.panelText()
	r.events(cat([]agent.Event{started("q")}, taskOpen("tk", "t1", "w", 1))...)
	r.events(inTask("t1", agent.Event{Kind: agent.EvMeter, Used: 900, Limit: 32000})...)
	if got := r.panelText(); got != before || !strings.Contains(got, "5.6k/16k") {
		t.Errorf("panel changed by a Task's meter: %q -> %q", before, got)
	}
	r.events(agent.Event{Kind: agent.EvMeter, Used: 6000, Limit: 16000})
	if got := r.panelText(); !strings.Contains(got, "6k/16k") {
		t.Errorf("the Session's meter is ignored: %q", got)
	}
}

// ---------------------------------------------------------------------------
// Plain mode
// ---------------------------------------------------------------------------

// scriptAgent answers a submitted text with scripted events (plain mode needs no real agent).
type scriptAgent struct {
	fakeAgent
	events chan agent.Event
	script []agent.Event
}

func (s *scriptAgent) Submit(string) {
	for _, ev := range s.script {
		s.events <- ev
	}
}

func runPlainScript(t *testing.T, script []agent.Event) string {
	t.Helper()
	ev := make(chan agent.Event, 64)
	ag := &scriptAgent{events: ev, script: append(script, agent.Event{Kind: agent.EvTurnEnded, Finish: chat.FinishStop})}
	var out bytes.Buffer
	if err := RunPlain(context.Background(), Deps{Agent: ag, Events: ev}, strings.NewReader("go"), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func plainTaskScript() []agent.Event {
	return cat(
		taskOpen("tk", "t1", "fix tests", 1),
		inTask("t1", delta("helper chatter\n")),
		inTask("t1", toolStart("c1", "shell", "go test"), toolEnd("c1", "shell", "go test", "ok", "", nil)),
		taskClose("tk", "t1", "fix tests", "fixed it\nmore"),
		[]agent.Event{delta("final\n")},
	)
}

func TestTaskBlock_PlainModeNoChildDeltas(t *testing.T) {
	out := runPlainScript(t, plainTaskScript())
	if strings.Contains(out, "helper chatter") {
		t.Errorf("a Task's text reached stdout:\n%s", out)
	}
	if !strings.Contains(out, "final\n") {
		t.Errorf("the Session's text is missing:\n%s", out)
	}
}

func TestTaskBlock_PlainModeTaskLines(t *testing.T) {
	lines := strings.Split(runPlainScript(t, plainTaskScript()), "\n")
	want := []string{"[task: fix tests]", "  ● shell(go test)  #1", "    └ ok", "[task fixed it]",
		"● task(fix tests)  #2", "  └ fixed it", "final"}
	if strings.Join(lines[:len(want)], "\n") != strings.Join(want, "\n") {
		t.Errorf("plain output:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

// ---------------------------------------------------------------------------
// /task
// ---------------------------------------------------------------------------

// taskAgent is a fake agent that also takes Tasks.
type taskAgent struct {
	fakeAgent
	goals []string
	err   error
}

func (a *taskAgent) SubmitTask(goal string) error {
	a.goals = append(a.goals, goal)
	return a.err
}

func TestTaskBlock_SlashTaskSubmits(t *testing.T) {
	r := newRig(t)
	ag := &taskAgent{}
	r.m.d.Agent = ag
	r.enter("/task  fix   the tests ")
	if len(ag.goals) != 1 || ag.goals[0] != "fix the tests" || len(ag.fakeAgent.submitted) != 0 {
		t.Fatalf("goals %q, plain submits %q", ag.goals, ag.fakeAgent.submitted)
	}
	if !strings.Contains(r.view(), "▸ fix the tests  (waiting)") {
		t.Errorf("the goal is not shown as waiting:\n%s", r.view())
	}
	r.events(started("fix the tests"))
	if len(r.m.waiting) != 0 || strings.Contains(r.view(), "(waiting)") {
		t.Errorf("the Turn did not take the waiting goal:\n%s", r.view())
	}
}

func TestTaskBlock_SlashTaskOff(t *testing.T) {
	r := newRig(t)
	ag := &taskAgent{err: agent.ErrTasksOff}
	r.m.d.Agent = ag
	r.enter("/task do it")
	if v := r.view(); !strings.Contains(v, "tasks are off (tasks.max_depth)") || len(r.m.waiting) != 0 {
		t.Errorf("ErrTasksOff:\n%s", v)
	}
	r2 := newRig(t) // an agent without SubmitTask at all
	r2.enter("/task do it")
	if v := r2.view(); !strings.Contains(v, "tasks are off (tasks.max_depth)") || len(r2.ag.submitted) != 0 {
		t.Errorf("no SubmitTask:\n%s", v)
	}
	ag.err = errors.New("boom")
	r.enter("/task again")
	if !strings.Contains(r.view(), "task: boom") {
		t.Errorf("another error:\n%s", r.view())
	}
}

func TestSlashTask_Usage(t *testing.T) {
	if _, err := ParseCommand("/task"); err == nil || err.Error() != "usage: /task <goal>" {
		t.Errorf("err = %v", err)
	}
	if c, err := ParseCommand("/task a  b"); err != nil || c.Kind != KindTask || c.Text != "a b" {
		t.Errorf("got %+v %v", c, err)
	}
	if !strings.Contains(helpText(), "/task <goal>") {
		t.Error("/help lacks /task")
	}
}

// ---------------------------------------------------------------------------
// Ctrl+O, /tool N, the end of the Turn, refused tasks
// ---------------------------------------------------------------------------

func TestTaskBlock_ShowCallOpensAncestors(t *testing.T) {
	r := taskRig(t)
	r.enter("/tool 2")
	if !r.isOpen(r.m.rec(1).item) || !r.isOpen(r.m.rec(2).item) {
		t.Fatalf("/tool 2 left the ancestor or the block closed")
	}
	if !strings.Contains(r.view(), "shell(go test)") {
		t.Errorf("the child is not on screen:\n%s", r.view())
	}
	r2 := taskRig(t) // Ctrl+O flips the last call, a hidden child
	r2.send(key('o', tea.ModCtrl))
	if !r2.isOpen(r2.m.rec(1).item) || !r2.isOpen(r2.m.rec(2).item) || !strings.Contains(r2.view(), "shell(go test)") {
		t.Errorf("ctrl+o on a hidden call:\n%s", r2.view())
	}
}

func TestTaskBlock_AbandonOnTurnEnd(t *testing.T) {
	r := newRig(t)
	r.events(cat([]agent.Event{started("q")}, taskOpen("tk", "t1", "w", 1), inTask("t1", delta("partial")))...)
	r.events(ended(chat.FinishInterrupted, nil))
	if len(r.m.tasks) != 0 {
		t.Fatalf("stack after the Turn: %v", r.m.tasks)
	}
	r.events(started("next"), delta("fresh"))
	if it := r.m.cv.at(r.m.cv.count() - 1); it.depth != 0 || it.parent != -1 {
		t.Errorf("the next Turn's text sits inside a dead Task: depth %d parent %d", it.depth, it.parent)
	}
	if rec := r.m.rec(1); !rec.done || rec.brief != cancelledOnEnd {
		t.Errorf("the Task block was not closed off: %+v", rec)
	}
}

func TestTaskBlock_RefusedTaskIsPlainBlock(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), toolStart("tk", taskToolName, "too deep"),
		toolEnd("tk", taskToolName, "too deep", "", "", errors.New("tasks nest at most 1 deep")), delta("after"))
	if len(r.m.tasks) != 0 {
		t.Fatalf("a refused task pushed the stack: %v", r.m.tasks)
	}
	for i := 0; i < r.m.cv.count(); i++ {
		if it := r.m.cv.at(i); it.depth != 0 || r.m.cv.hidden(i) {
			t.Errorf("item %d: depth %d hidden %v", i, it.depth, r.m.cv.hidden(i))
		}
	}
	text := r.convText()
	if !strings.Contains(text, "● task(too deep)  #1") || !strings.Contains(text, "✘ error: tasks nest") || !strings.Contains(text, "after") {
		t.Errorf("refused block:\n%s", text)
	}
}

// ---------------------------------------------------------------------------
// The live item while hidden
// ---------------------------------------------------------------------------

func TestTaskBlock_LiveAssistantKeepsBlocksWhileHidden(t *testing.T) {
	n := countMarkdown(t)
	r := newRig(t)
	r.events(cat([]agent.Event{started("q")}, taskOpen("tk", "t1", "w", 1))...)
	r.events(inTask("t1", delta("one\n\ntwo\n\n"))...)
	r.events(inTask("t1", delta("three\n\n"))...)
	if *n != 0 {
		t.Fatalf("hidden live text was rendered %d times", *n)
	}
	live := r.m.cv.at(r.m.cur)
	tail := func() int { // the open tail is rendered on every version
		if strings.TrimSpace(live.cm.Tail()) != "" {
			return 1
		}
		return 0
	}
	first := len(live.blocks)
	r.open(1)
	r.convText()
	opened := *n
	if opened != first+tail() {
		t.Fatalf("opening rendered %d blocks, want the %d committed ones and the tail", opened, first)
	}
	r.m.cv.setOpen(r.m.rec(1).item, false)
	r.events(inTask("t1", delta("four\n\n"))...)
	if *n != opened {
		t.Fatalf("hidden delta rendered markdown: %d -> %d", opened, *n)
	}
	r.open(1)
	r.convText()
	if got, want := *n-opened, len(live.blocks)-first+tail(); got != want || want >= opened {
		t.Errorf("reopening rendered %d blocks, want only the %d new ones", got, want)
	}
	if text := r.convText(); !strings.Contains(text, "one") || !strings.Contains(text, "four") {
		t.Errorf("text lost:\n%s", text)
	}
}

// ---------------------------------------------------------------------------
// Readability
// ---------------------------------------------------------------------------

func TestReadable_TaskBlockInEveryProfile(t *testing.T) {
	for _, p := range profiles {
		t.Run(p.String(), func(t *testing.T) {
			r := taskRig(t)
			r.send(tea.ColorProfileMsg{Profile: p})
			r.send(tea.WindowSizeMsg{Width: 80, Height: 60})
			r.open(1)
			r.open(2)
			r.m.sel = selection{active: true, a: selPoint{1, 0, 0}, b: selPoint{r.m.cv.count() - 1, 5, 79}}
			if bad := unreadable(downsample(r.m.View().Content, p), p); len(bad) > 0 {
				t.Errorf("unreadable cells:\n  %s", strings.Join(bad, "\n  "))
			}
			r.m.sel = selection{}
			y := r.rowIndex("shell(go test)")
			r.hoverTo(gw+2, y)
			raw := r.m.View().Content
			if !strings.Contains(strings.Split(raw, "\n")[y], underlineOn) {
				t.Errorf("child head not underlined on hover")
			}
			if bad := unreadable(downsample(raw, p), p); len(bad) > 0 {
				t.Errorf("unreadable cells with the hover:\n  %s", strings.Join(bad, "\n  "))
			}
		})
	}
}
