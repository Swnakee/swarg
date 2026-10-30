package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// fakeTools: a ToolRunner whose calls can be held and released from the test
// ---------------------------------------------------------------------------

// Tool names of fakeTools: read is read-only, mut mutates, ask mutates and needs approval.
const (
	toolRead = "read"
	toolSkl  = "skill" // the skill Tool: read-only, free (the /skill prelude)
	toolMut  = "mut"
	toolAsk  = "ask"
	toolEdit = "edit" // mutating, not detachable
)

// gate holds one call: started is closed when Run begins, release lets it finish.
type gate struct{ started, release chan struct{} }

type fakeTools struct {
	mu      sync.Mutex
	gates   map[string]*gate
	log     []string
	failing map[string]error
	panics  map[string]bool
	hooks   map[string]ToolHooks // hooks of every call that entered Run, by call id
}

var _ ToolRunner = (*fakeTools)(nil)

func newFakeTools() *fakeTools {
	return &fakeTools{gates: map[string]*gate{}, failing: map[string]error{}, panics: map[string]bool{}, hooks: map[string]ToolHooks{}}
}

// hold makes the calls with these ids block in Run until release.
func (f *fakeTools) hold(ids ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range ids {
		f.gates[id] = &gate{started: make(chan struct{}), release: make(chan struct{})}
	}
}

func (f *fakeTools) gate(id string) *gate {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gates[id]
}

// waitStarted blocks until the held call id is inside Run.
func (f *fakeTools) waitStarted(t *testing.T, id string) {
	t.Helper()
	select {
	case <-f.gate(id).started:
	case <-time.After(deadline):
		t.Fatalf("call %s never started", id)
	}
}

func (f *fakeTools) release(id string) { close(f.gate(id).release) }

// progress calls the Progress hook of call id as the tool goroutine would.
func (f *fakeTools) progress(id, chunk string) {
	f.mu.Lock()
	h := f.hooks[id]
	f.mu.Unlock()
	h.Progress(chunk)
}

// detached reports whether the Detach channel of call id is closed.
func (f *fakeTools) detached(id string) bool {
	f.mu.Lock()
	h := f.hooks[id]
	f.mu.Unlock()
	select {
	case <-h.Detach:
		return true
	default:
		return false
	}
}

func (f *fakeTools) add(s string) {
	f.mu.Lock()
	f.log = append(f.log, s)
	f.mu.Unlock()
}

// events returns the start ("s:id") and end ("e:id") log.
func (f *fakeTools) events() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

func (f *fakeTools) Specs() []chat.ToolSpec {
	return []chat.ToolSpec{{Name: toolRead, Description: "read", Schema: json.RawMessage(`{"type":"object"}`)}}
}

func (f *fakeTools) Kind(name string) (known, readOnly, confirm bool) {
	switch name {
	case toolRead, toolSkl:
		return true, true, false
	case toolMut, toolEdit:
		return true, false, false
	case toolAsk:
		return true, false, true
	}
	return false, false, false
}

func (f *fakeTools) Detachable(name string) bool { return name == toolMut || name == toolAsk }

func (f *fakeTools) Label(c chat.ToolCall) string { return "L:" + c.Name + c.Args }

func (f *fakeTools) Run(ctx context.Context, c chat.ToolCall, hooks ToolHooks) (string, string, error) {
	f.mu.Lock()
	f.hooks[c.ID] = hooks
	f.mu.Unlock()
	f.add("s:" + c.ID)
	if g := f.gate(c.ID); g != nil {
		close(g.started)
		select {
		case <-hooks.Detach: // nil channel for a non-detachable call: never fires
			f.add("d:" + c.ID)
			return "moved to background:" + c.ID, "bg:" + c.ID, nil
		case <-g.release:
		case <-ctx.Done():
			f.add("x:" + c.ID)
			return "", "", ctx.Err()
		}
	}
	f.add("e:" + c.ID)
	f.mu.Lock()
	err, boom := f.failing[c.ID], f.panics[c.ID]
	f.mu.Unlock()
	if boom {
		panic("boom")
	}
	if err != nil {
		return "", "", err
	}
	return "out:" + c.ID, "brief:" + c.ID, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// toolCall builds call "c<i>"; empty args become "{}".
func toolCall(i int, name, args string) chat.ToolCall {
	if args == "" {
		args = "{}"
	}
	return chat.ToolCall{ID: fmt.Sprintf("c%d", i), Name: name, Args: args}
}

// toolStep plays one model step: optional text, then the calls.
func (c *call) toolStep(text string, calls ...chat.ToolCall) {
	if text != "" {
		c.say(text)
	}
	c.endWith(chat.StreamEvent{Kind: chat.EventDone, Finish: chat.FinishToolCalls, ToolCalls: calls})
}

// endWith sends a final event and closes the stream.
func (c *call) endWith(ev chat.StreamEvent) {
	c.ch <- ev
	close(c.ch)
	c.p.mu.Lock()
	c.p.cur--
	c.p.mu.Unlock()
}

// toolRig starts a rig with ft as the tool runner.
func toolRig(t *testing.T, ft *fakeTools, mod ...func(*Deps)) *rig {
	t.Helper()
	return startRig(t, append([]func(*Deps){func(d *Deps) { d.Tools = ft }}, mod...)...)
}

// assertHistoryValid checks the strict-template rules: every assistant tool call is
// followed by exactly one tool message in order; ids are unique per message and
// arguments are JSON objects; no tool message is orphaned.
func assertHistoryValid(t *testing.T, h []chat.Message) {
	t.Helper()
	for i := 0; i < len(h); i++ {
		switch m := h[i]; m.Role {
		case chat.RoleAssistant:
			seen := map[string]bool{}
			for k, c := range m.ToolCalls {
				var obj map[string]any
				if c.ID == "" || seen[c.ID] || json.Unmarshal([]byte(c.Args), &obj) != nil || obj == nil {
					t.Fatalf("bad call %+v in history %+v", c, h)
				}
				seen[c.ID] = true
				if j := i + 1 + k; j >= len(h) || h[j].Role != chat.RoleTool || h[j].ToolCallID != c.ID {
					t.Fatalf("call %s has no result at %d in %+v", c.ID, j, h)
				}
			}
			i += len(m.ToolCalls)
		case chat.RoleTool:
			t.Fatalf("orphan tool message at %d in %+v", i, h)
		}
	}
}

// resultsOf returns the tool message contents of a request, in order.
func resultsOf(req chat.Request) (out []string) {
	for _, m := range req.Messages {
		if m.Role == chat.RoleTool {
			out = append(out, m.Content)
		}
	}
	return out
}

// noMoreStream fails when another Stream call is pending.
func noMoreStream(t *testing.T, p *fakeProvider) {
	t.Helper()
	select {
	case <-p.calls:
		t.Fatal("unexpected Stream call")
	default:
	}
}

// oneStep runs a turn "go" whose first step is the given calls, then answers "done"
// at the second; it returns the second request.
func oneStep(t *testing.T, r *rig, calls ...chat.ToolCall) chat.Request {
	t.Helper()
	r.a.Submit("go")
	r.p.next(t).toolStep("", calls...)
	c2 := r.p.next(t)
	c2.reply("done")
	wait(t, r.a)
	assertHistoryValid(t, r.a.History())
	return c2.req
}

// ---------------------------------------------------------------------------
// Loop
// ---------------------------------------------------------------------------

func TestTools_LoopSendsResultsAndTools(t *testing.T) {
	r := toolRig(t, newFakeTools())
	r.a.Submit("go")
	r.p.next(t).toolStep("thinking", toolCall(1, toolRead, `{"p":1}`))
	c2 := r.p.next(t)
	if len(c2.req.Tools) != 1 || c2.req.Tools[0].Name != toolRead {
		t.Fatalf("Tools = %+v", c2.req.Tools)
	}
	c2.reply("done")
	wait(t, r.a)
	got := c2.req.Messages
	if g := roles(got); len(g) != 4 || g[2] != chat.RoleAssistant || g[3] != chat.RoleTool {
		t.Fatalf("roles = %v", g)
	}
	a, tm := got[2], got[3]
	if a.Content != "thinking" || len(a.ToolCalls) != 1 || a.ToolCalls[0] != toolCall(1, toolRead, `{"p":1}`) {
		t.Fatalf("assistant = %+v", a)
	}
	if tm.ToolCallID != "c1" || tm.Name != toolRead || tm.Content != "out:c1" {
		t.Fatalf("tool message = %+v", tm)
	}
	assertHistoryValid(t, r.a.History())
	if end := r.rec.of(EvTurnEnded); len(end) != 1 || end[0].Message.Content != "done" {
		t.Fatalf("TurnEnded = %+v", end)
	}
}

func TestTools_NilRunnerIsSingleStepWithoutTools(t *testing.T) {
	r := startRig(t)
	r.a.Submit("go")
	c := r.p.next(t)
	if c.req.Tools != nil {
		t.Fatalf("Tools = %+v", c.req.Tools)
	}
	c.reply("ok")
	wait(t, r.a)
	noMoreStream(t, r.p)
}

func TestTools_EmptyTextWithCallsStored(t *testing.T) {
	r := toolRig(t, newFakeTools())
	oneStep(t, r, toolCall(1, toolRead, ""))
	h := r.a.History()
	if h[1].Role != chat.RoleAssistant || h[1].Content != "" || len(h[1].ToolCalls) != 1 {
		t.Fatalf("history = %+v", h)
	}
}

// ---------------------------------------------------------------------------
// Scheduler
// ---------------------------------------------------------------------------

func TestTools_ReadOnlyRunInParallelResultsInCallOrder(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1", "c2", "c3")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolRead, ""), toolCall(2, toolRead, ""), toolCall(3, toolRead, ""))
	for _, id := range []string{"c1", "c2", "c3"} {
		ft.waitStarted(t, id) // all three are inside Run before any release
	}
	for _, id := range []string{"c3", "c2", "c1"} {
		ft.release(id)
	}
	c2 := r.p.next(t)
	c2.reply("done")
	wait(t, r.a)
	if got := resultsOf(c2.req); strings.Join(got, ",") != "out:c1,out:c2,out:c3" {
		t.Fatalf("results = %v", got)
	}
	assertHistoryValid(t, r.a.History())
}

func TestTools_MutatingRunOneAtATimeInOrder(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1", "c2", "c3")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolMut, ""), toolCall(2, toolMut, ""), toolCall(3, toolMut, ""))
	ft.waitStarted(t, "c1")
	if got := ft.events(); strings.Join(got, ",") != "s:c1" {
		t.Fatalf("call 2 started before call 1 ended: %v", got)
	}
	ft.release("c1")
	ft.waitStarted(t, "c2")
	ft.release("c2")
	ft.waitStarted(t, "c3")
	ft.release("c3")
	r.p.next(t).reply("done")
	wait(t, r.a)
	want := "s:c1,e:c1,s:c2,e:c2,s:c3,e:c3"
	if got := strings.Join(ft.events(), ","); got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
	assertHistoryValid(t, r.a.History())
}

func TestTools_MixedBatches(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1", "c2", "c3", "c4")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolRead, ""), toolCall(2, toolRead, ""), toolCall(3, toolMut, ""), toolCall(4, toolRead, ""))
	ft.waitStarted(t, "c1")
	ft.waitStarted(t, "c2")
	if got := len(ft.events()); got != 2 {
		t.Fatalf("mutating call started with the read batch: %v", ft.events())
	}
	ft.release("c1")
	ft.release("c2")
	ft.waitStarted(t, "c3")
	if got := ft.events(); got[len(got)-1] != "s:c3" || len(got) != 5 {
		t.Fatalf("events = %v", got)
	}
	ft.release("c3")
	ft.waitStarted(t, "c4")
	ft.release("c4")
	c2 := r.p.next(t)
	c2.reply("done")
	wait(t, r.a)
	if got := resultsOf(c2.req); strings.Join(got, ",") != "out:c1,out:c2,out:c3,out:c4" {
		t.Fatalf("results = %v", got)
	}
	assertHistoryValid(t, r.a.History())
}

// ---------------------------------------------------------------------------
// Events and errors
// ---------------------------------------------------------------------------

func TestTools_ToolEvents(t *testing.T) {
	ft := newFakeTools()
	ft.failing["c2"] = errors.New("nope")
	r := toolRig(t, ft)
	oneStep(t, r, toolCall(1, toolRead, `{"a":1}`), toolCall(2, toolMut, ""))
	starts, ends := r.rec.of(EvToolStart), r.rec.of(EvToolEnd)
	if len(starts) != 2 || len(ends) != 2 {
		t.Fatalf("starts=%d ends=%d", len(starts), len(ends))
	}
	if s := starts[0]; s.Call.ID != "c1" || s.Label != `L:read{"a":1}` {
		t.Fatalf("start = %+v", s)
	}
	if e := ends[0]; e.Call.ID != "c1" || e.Brief != "brief:c1" || e.Output != "out:c1" || e.Err != nil || e.Label == "" {
		t.Fatalf("end 1 = %+v", e)
	}
	if e := ends[1]; e.Output != "error: nope" || e.Err == nil {
		t.Fatalf("end 2 = %+v", e)
	}
}

func TestTools_ErrorsGoToTheModelAndTurnContinues(t *testing.T) {
	ft := newFakeTools()
	ft.failing["c2"] = errors.New("disk full")
	approved := 0
	r := toolRig(t, ft, func(d *Deps) {
		d.Approve = func(context.Context, chat.ToolCall) Approval { approved++; return Approval{Allow: true} }
	})
	req := oneStep(t, r, toolCall(1, "nosuch", ""), toolCall(2, toolAsk, ""), toolCall(3, toolRead, "not json"))
	got := resultsOf(req)
	if got[0] != "error: unknown tool nosuch" || got[1] != "error: disk full" || !strings.HasPrefix(got[2], "error: ") {
		t.Fatalf("results = %q", got)
	}
	if approved != 1 { // only the known confirm-needing call asks
		t.Fatalf("Approve called %d times", approved)
	}
	if n := len(r.rec.of(EvTurnStarted)); n != 1 {
		t.Fatalf("turns = %d", n)
	}
}

// ---------------------------------------------------------------------------
// Approval
// ---------------------------------------------------------------------------

func TestTools_ApprovalDenyAndAllow(t *testing.T) {
	ft := newFakeTools()
	r := toolRig(t, ft, func(d *Deps) {
		d.Approve = func(_ context.Context, c chat.ToolCall) Approval {
			switch c.ID {
			case "c1":
				return Approval{Note: "not now"}
			}
			return Approval{Allow: true}
		}
	})
	req := oneStep(t, r, toolCall(1, toolAsk, ""), toolCall(2, toolAsk, ""), toolCall(3, toolAsk, ""))
	want := "denied by the user: not now. " + denyAdvice + ",out:c2,out:c3"
	if got := strings.Join(resultsOf(req), ","); got != want {
		t.Fatalf("results = %s", got)
	}
	if ev := ft.events(); len(ev) != 4 || ev[0] != "s:c2" { // c2 and c3 ran
		t.Fatalf("events = %v", ev)
	}
}

func TestTools_ApprovalNotAskedForReadOnlyAndNilAllows(t *testing.T) {
	asked := 0
	r := toolRig(t, newFakeTools(), func(d *Deps) {
		d.Approve = func(context.Context, chat.ToolCall) Approval { asked++; return Approval{Allow: true} }
	})
	oneStep(t, r, toolCall(1, toolRead, ""), toolCall(2, toolMut, ""))
	if asked != 0 {
		t.Fatalf("asked %d times", asked)
	}
	r2 := toolRig(t, newFakeTools()) // nil Approve allows
	req := oneStep(t, r2, toolCall(1, toolAsk, ""))
	if got := resultsOf(req); got[0] != "out:c1" {
		t.Fatalf("results = %v", got)
	}
}

func TestTools_BlockedApproveKeepsBusyAndInterruptCancels(t *testing.T) {
	asking := make(chan struct{})
	r := toolRig(t, newFakeTools(), func(d *Deps) {
		d.Approve = func(ctx context.Context, _ chat.ToolCall) Approval {
			close(asking)
			<-ctx.Done()
			return Approval{Allow: true} // a late "yes" must not win over the cancel
		}
	})
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolAsk, ""))
	<-asking
	if !r.a.Busy() {
		t.Fatal("agent not busy while approval is pending")
	}
	r.a.Interrupt()
	wait(t, r.a)
	h := r.a.History()
	assertHistoryValid(t, h)
	if last := h[len(h)-1]; last.Content != "cancelled by the user" {
		t.Fatalf("result = %q", last.Content)
	}
	noMoreStream(t, r.p)
}

// ---------------------------------------------------------------------------
// Injection and skipping
// ---------------------------------------------------------------------------

func TestTools_QueuedTextInjectedAfterToolBatch(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolRead, ""))
	ft.waitStarted(t, "c1")
	r.a.Submit("hey")
	ft.release("c1")
	c2 := r.p.next(t)
	m := c2.req.Messages
	if n := len(m); n != 5 || m[3].Role != chat.RoleTool || m[4].Role != chat.RoleUser || m[4].Content != "hey" {
		t.Fatalf("messages = %+v", m)
	}
	c2.reply("done")
	wait(t, r.a)
	assertHistoryValid(t, r.a.History())
	if got := r.rec.texts(EvUserText); len(got) != 1 || got[0] != "hey" {
		t.Fatalf("EvUserText = %q", got)
	}
	if q := r.rec.of(EvQueue); q[len(q)-1].Queued != 0 {
		t.Fatalf("queue events = %+v", q)
	}
	if n := len(r.rec.of(EvTurnStarted)); n != 1 {
		t.Fatalf("turns = %d, want one", n)
	}
	noMoreStream(t, r.p)
}

func TestTools_NewTextSkipsMutatingNotYetStarted(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1", "c3")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolMut, ""), toolCall(2, toolMut, ""), toolCall(3, toolRead, ""))
	ft.waitStarted(t, "c1")
	r.a.Submit("stop")
	ft.release("c1") // started calls are not cancelled
	ft.waitStarted(t, "c3")
	ft.release("c3")
	c2 := r.p.next(t)
	want := "out:c1,skipped: the user sent a new message,out:c3"
	if got := strings.Join(resultsOf(c2.req), ","); got != want {
		t.Fatalf("results = %s", got)
	}
	c2.reply("done")
	wait(t, r.a)
	assertHistoryValid(t, r.a.History())
	if ev := strings.Join(ft.events(), ","); strings.Contains(ev, "c2") {
		t.Fatalf("skipped call ran: %s", ev)
	}
}

func TestTools_TextDuringFinalAnswerStartsNewTurn(t *testing.T) {
	r := toolRig(t, newFakeTools())
	r.a.Submit("A")
	c := r.p.next(t)
	c.say("x")
	r.a.Submit("B")
	c.end(chat.FinishStop, nil)
	r.p.next(t).reply("y")
	wait(t, r.a)
	wantHistory(t, r.a, user("A"), asst("x"), user("B"), asst("y"))
	if n := len(r.rec.of(EvTurnStarted)); n != 2 || len(r.rec.of(EvUserText)) != 0 {
		t.Fatalf("turns = %d", n)
	}
}

// ---------------------------------------------------------------------------
// MaxSteps and meter
// ---------------------------------------------------------------------------

func TestTools_MaxStepsEndsTheTurn(t *testing.T) {
	r := toolRig(t, newFakeTools(), func(d *Deps) { d.MaxSteps = 3 })
	r.a.Submit("go")
	for i := 1; i <= 3; i++ {
		r.p.next(t).toolStep("", toolCall(i, toolRead, ""))
	}
	wait(t, r.a)
	noMoreStream(t, r.p)
	if w := r.rec.texts(EvWarning); len(w) == 0 || w[0] != "step limit reached (3)" {
		t.Fatalf("warnings = %q", w)
	}
	assertHistoryValid(t, r.a.History())
}

func TestTools_MeterLastUsageWins(t *testing.T) {
	r := toolRig(t, newFakeTools())
	r.a.Submit("go")
	c := r.p.next(t)
	c.usage(100, 10)
	c.toolStep("", toolCall(1, toolRead, ""))
	c2 := r.p.next(t)
	c2.usage(300, 20)
	c2.reply("done")
	wait(t, r.a)
	if m := r.rec.lastMeter(t); m.Used != 320 {
		t.Fatalf("meter = %+v", m)
	}
}

func TestMeter_CountsToolCallsAndMessages(t *testing.T) {
	h := []chat.Message{
		{Role: chat.RoleAssistant, ToolCalls: []chat.ToolCall{{ID: "c1", Name: "read", Args: strings.Repeat("x", 400)}}},
		{Role: chat.RoleTool, Content: strings.Repeat("y", 400)},
	}
	if n := estimateHistory("", h); n < 200 {
		t.Fatalf("estimate = %d", n)
	}
}

// ---------------------------------------------------------------------------
// Interrupt during tools and streams
// ---------------------------------------------------------------------------

func TestTools_InterruptCancelsToolsAndEndsTurn(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolRead, ""), toolCall(2, toolMut, ""))
	ft.waitStarted(t, "c1")
	r.a.Interrupt()
	wait(t, r.a)
	h := r.a.History()
	assertHistoryValid(t, h)
	if n := len(h); h[n-1].Content != "cancelled by the user" || h[n-2].Content != "cancelled by the user" {
		t.Fatalf("history = %+v", h)
	}
	noMoreStream(t, r.p)
	if e := r.rec.of(EvTurnEnded); e[0].Finish != chat.FinishInterrupted {
		t.Fatalf("TurnEnded = %+v", e[0])
	}
}

func TestTools_InterruptDuringToolsKeepsQueueForNewTurn(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolRead, ""))
	ft.waitStarted(t, "c1")
	r.a.Submit("B")
	r.a.Interrupt()
	c2 := r.p.next(t)
	m := c2.req.Messages
	if last := m[len(m)-1]; last.Role != chat.RoleUser || last.Content != "B" {
		t.Fatalf("messages = %+v", m)
	}
	c2.reply("b")
	wait(t, r.a)
	assertHistoryValid(t, r.a.History())
	if n := len(r.rec.of(EvTurnStarted)); n != 2 {
		t.Fatalf("turns = %d", n)
	}
}

func TestTools_InterruptMidStreamDropsCalls(t *testing.T) {
	r := toolRig(t, newFakeTools())
	r.a.Submit("go")
	c := r.p.next(t)
	c.say("a")
	r.rec.until(t, EvDelta, 1)
	r.a.Interrupt()
	r.rec.until(t, EvDraining, 1)
	c.toolStep("", toolCall(1, toolRead, ""))
	wait(t, r.a)
	wantHistory(t, r.a, user("go"), chat.Message{Role: chat.RoleAssistant, Content: "a", Interrupted: true})
	if len(r.rec.of(EvToolStart)) != 0 {
		t.Fatal("tool started from an interrupted stream")
	}
}

// ---------------------------------------------------------------------------
// History rules
// ---------------------------------------------------------------------------

func TestTools_ArgsNormalized(t *testing.T) {
	cases := []struct {
		name, args, stored string
		isErr              bool
	}{
		{"empty", "", "{}", false},
		{"spaces", "   ", "{}", false},
		{"null", "null", "{}", false},
		{"double encoded", `"{\"a\":1}"`, `{"a":1}`, false},
		{"not json", "not json", "{}", true},
		{"array", "[1]", "{}", true},
		{"plain string", `"hello"`, "{}", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ft := newFakeTools()
			r := toolRig(t, ft)
			req := oneStep(t, r, chat.ToolCall{ID: "c1", Name: toolRead, Args: c.args})
			if got := r.a.History()[1].ToolCalls[0].Args; got != c.stored {
				t.Fatalf("stored args = %q, want %q", got, c.stored)
			}
			res := resultsOf(req)[0]
			if c.isErr != strings.HasPrefix(res, "error: ") || (c.isErr && len(ft.events()) != 0) {
				t.Fatalf("result = %q, events = %v", res, ft.events())
			}
		})
	}
}

func TestTools_LengthWithCallsGivesErrorResults(t *testing.T) {
	ft := newFakeTools()
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).endWith(chat.StreamEvent{Kind: chat.EventDone, Finish: chat.FinishLength, ToolCalls: []chat.ToolCall{toolCall(1, toolRead, `{"a"`)}})
	c2 := r.p.next(t)
	if got := resultsOf(c2.req); len(got) != 1 || !strings.HasPrefix(got[0], "error: ") || !strings.Contains(got[0], "token limit") {
		t.Fatalf("results = %q", got)
	}
	c2.reply("done")
	wait(t, r.a)
	assertHistoryValid(t, r.a.History())
	if len(ft.events()) != 0 {
		t.Fatalf("cut call ran: %v", ft.events())
	}
}

func TestTools_ErrorStreamDiscardsCalls(t *testing.T) {
	r := toolRig(t, newFakeTools())
	r.a.Submit("go")
	r.p.next(t).endWith(chat.StreamEvent{Kind: chat.EventDone, Finish: chat.FinishError, Err: errors.New("bad"), ToolCalls: []chat.ToolCall{toolCall(1, toolRead, "")}})
	wait(t, r.a)
	wantHistory(t, r.a, user("go"))
	if e := r.rec.of(EvTurnEnded); e[0].Err == nil || len(r.rec.of(EvToolStart)) != 0 {
		t.Fatalf("TurnEnded = %+v", e[0])
	}
}

func TestTools_CallIDsAreSessionUnique(t *testing.T) {
	r := toolRig(t, newFakeTools())
	oneStep(t, r, chat.ToolCall{Name: toolRead}, chat.ToolCall{ID: "x", Name: toolRead}, chat.ToolCall{ID: "x", Name: toolRead})
	ids := func(m chat.Message) (out []string) {
		for _, c := range m.ToolCalls {
			out = append(out, c.ID)
		}
		return out
	}
	if got := strings.Join(ids(r.a.History()[1]), ","); got != "call_1,x,call_2" {
		t.Fatalf("ids = %s", got)
	}
	oneStep(t, r, chat.ToolCall{Name: toolRead})
	h := r.a.History()
	if got := ids(h[len(h)-3]); len(got) != 1 || got[0] != "call_3" {
		t.Fatalf("second turn ids = %v", got)
	}
}

func TestTools_PanicBecomesErrorResult(t *testing.T) {
	ft := newFakeTools()
	ft.panics["c1"] = true
	r := toolRig(t, ft)
	req := oneStep(t, r, toolCall(1, toolRead, ""), toolCall(2, toolMut, ""))
	if got := resultsOf(req); got[0] != "error: tool panicked: boom" || got[1] != "out:c2" {
		t.Fatalf("results = %q", got)
	}
}

// ---------------------------------------------------------------------------
// Abort
// ---------------------------------------------------------------------------

// healthProbe is a Deps.Health that records its calls.
type healthProbe struct {
	calls chan context.Context
	gate  chan struct{} // when non-nil, Health blocks until it is closed
	err   error
}

func newHealth(err error) *healthProbe {
	return &healthProbe{calls: make(chan context.Context, 8), err: err}
}

func (h *healthProbe) check(ctx context.Context) error {
	h.calls <- ctx
	if h.gate != nil {
		<-h.gate
	}
	return h.err
}

const wantBusyWarning = "model server busy or not responding; if it stays so: llm stop && llm start qwen"

func TestAbort_MidStreamCancelsRequestAndFreesWorker(t *testing.T) {
	h := newHealth(errors.New("down"))
	r := toolRig(t, newFakeTools(), func(d *Deps) { d.Health = h.check })
	r.a.Submit("A")
	c := r.p.next(t)
	c.say("ab")
	r.rec.until(t, EvDelta, 1)
	r.a.Abort()
	wait(t, r.a) // the stream never ends: no drain
	if c.ctx.Err() == nil {
		t.Fatal("request ctx not cancelled")
	}
	wantHistory(t, r.a, user("A"), chat.Message{Role: chat.RoleAssistant, Content: "ab", Interrupted: true})
	if e := r.rec.of(EvTurnEnded); e[0].Finish != chat.FinishInterrupted || e[0].Err != nil {
		t.Fatalf("TurnEnded = %+v", e[0])
	}
	if ctx := <-h.calls; ctx.Err() != nil {
		t.Fatal("Health got a dead ctx")
	}
	r.rec.until(t, EvWarning, 1)
	if w := r.rec.texts(EvWarning); w[len(w)-1] != wantBusyWarning {
		t.Fatalf("warnings = %q", w)
	}
}

func TestAbort_SecondInterruptEqualsAbort(t *testing.T) {
	r := toolRig(t, newFakeTools())
	r.a.Submit("A")
	c := r.p.next(t)
	c.say("a")
	r.rec.until(t, EvDelta, 1)
	r.a.Interrupt()
	r.rec.until(t, EvDraining, 1)
	if c.ctx.Err() != nil {
		t.Fatal("first interrupt cancelled the request")
	}
	r.a.Interrupt()
	wait(t, r.a)
	if c.ctx.Err() == nil {
		t.Fatal("second interrupt did not cancel the request")
	}
}

func TestAbort_HealthIsAsyncAndOkIsSilent(t *testing.T) {
	h := newHealth(nil)
	h.gate = make(chan struct{})
	r := toolRig(t, newFakeTools(), func(d *Deps) { d.Health = h.check })
	r.a.Submit("A")
	r.p.next(t).say("a")
	r.rec.until(t, EvDelta, 1)
	r.a.Abort()
	<-h.calls
	r.a.Submit("B") // Health is still blocked: the worker must go on
	r.p.next(t).reply("b")
	wait(t, r.a)
	close(h.gate)
	r.a.Submit("C")
	r.p.next(t).reply("c")
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 0 {
		t.Fatalf("warnings = %q", w)
	}
	if len(h.calls) != 0 {
		t.Fatal("Health called more than once")
	}
}

func TestAbort_IdleIsNoopWithoutHealth(t *testing.T) {
	h := newHealth(errors.New("down"))
	r := toolRig(t, newFakeTools(), func(d *Deps) { d.Health = h.check })
	r.a.Abort()
	r.a.Submit("A")
	r.p.next(t).reply("a")
	wait(t, r.a)
	if len(h.calls) != 0 || len(r.rec.of(EvWarning)) != 0 {
		t.Fatal("idle Abort had an effect")
	}
}

func TestAbort_DuringToolsKillsThem(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolRead, ""))
	ft.waitStarted(t, "c1")
	r.a.Abort()
	wait(t, r.a)
	h := r.a.History()
	assertHistoryValid(t, h)
	if h[len(h)-1].Content != "cancelled by the user" {
		t.Fatalf("history = %+v", h)
	}
	noMoreStream(t, r.p)
}

// ---------------------------------------------------------------------------
// Stress
// ---------------------------------------------------------------------------

func TestTools_StressParallelBatchWithInterruptRace(t *testing.T) {
	ft := newFakeTools()
	r := toolRig(t, ft)
	for i := 0; i < 50; i++ {
		base := i * 10
		ids := []string{fmt.Sprintf("c%d", base+1), fmt.Sprintf("c%d", base+2), fmt.Sprintf("c%d", base+3)}
		ft.hold(ids...)
		r.a.Submit("go")
		r.p.next(t).toolStep("", toolCall(base+1, toolRead, ""), toolCall(base+2, toolRead, ""), toolCall(base+3, toolRead, ""))
		for _, id := range ids {
			ft.waitStarted(t, id)
		}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); r.a.Interrupt() }()
		wg.Wait()
		wait(t, r.a)
		assertHistoryValid(t, r.a.History())
	}
}

// ---------------------------------------------------------------------------
// Repeated denials
// ---------------------------------------------------------------------------

// denyAll refuses every confirm-needing call.
func denyAll(d *Deps) {
	d.Approve = func(context.Context, chat.ToolCall) Approval { return Approval{} }
}

func TestTools_OneDenialLetsTheModelAnswer(t *testing.T) {
	r := toolRig(t, newFakeTools(), denyAll)
	req := oneStep(t, r, toolCall(1, toolAsk, ""))
	if got := resultsOf(req); len(got) != 1 || !strings.HasPrefix(got[0], "denied by the user. ") || !strings.Contains(got[0], "Do not retry") {
		t.Fatalf("results = %q", got)
	}
	if w := r.rec.texts(EvWarning); len(w) != 0 {
		t.Fatalf("warnings = %q", w)
	}
	if h := r.a.History(); h[len(h)-1].Content != "done" {
		t.Fatalf("history = %+v", h)
	}
}

func TestTools_TwoDenialsAcrossStepsEndTheTurn(t *testing.T) {
	r := toolRig(t, newFakeTools(), denyAll)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolAsk, ""))
	r.p.next(t).toolStep("", toolCall(2, toolAsk, ""))
	wait(t, r.a)
	noMoreStream(t, r.p)
	if w := r.rec.texts(EvWarning); len(w) == 0 || w[0] != "stopped: tool calls were denied" {
		t.Fatalf("warnings = %q", w)
	}
	if end := r.rec.of(EvTurnEnded); len(end) != 1 || end[0].Finish != chat.FinishStop {
		t.Fatalf("ended = %+v", end)
	}
	assertHistoryValid(t, r.a.History())
}

func TestTools_TwoDeniedCallsInOneStepEndTheTurn(t *testing.T) {
	r := toolRig(t, newFakeTools(), denyAll)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolAsk, ""), toolCall(2, toolAsk, ""))
	wait(t, r.a)
	noMoreStream(t, r.p)
	if w := r.rec.texts(EvWarning); len(w) == 0 || w[0] != "stopped: tool calls were denied" {
		t.Fatalf("warnings = %q", w)
	}
	assertHistoryValid(t, r.a.History())
}

func TestTools_ApprovedCallsDoNotCountAsDenials(t *testing.T) {
	r := toolRig(t, newFakeTools(), func(d *Deps) {
		d.Approve = func(_ context.Context, c chat.ToolCall) Approval { return Approval{Allow: c.ID != "c1"} }
	})
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolAsk, ""))
	r.p.next(t).toolStep("", toolCall(2, toolAsk, ""))
	r.p.next(t).toolStep("", toolCall(3, toolAsk, ""))
	r.p.next(t).reply("done")
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 0 {
		t.Fatalf("warnings = %q", w)
	}
	assertHistoryValid(t, r.a.History())
}

// ---------------------------------------------------------------------------
// Repeated failing calls (R-24)
// ---------------------------------------------------------------------------

// failingRig makes every call of the listed ids fail with err.
func failingRig(t *testing.T, ids int, err error) *rig {
	ft := newFakeTools()
	for i := 1; i <= ids; i++ {
		ft.failing[fmt.Sprintf("c%d", i)] = err
	}
	return toolRig(t, ft)
}

func TestRepeatGuard_SecondIdenticalFailureGetsNote(t *testing.T) {
	r := failingRig(t, 3, errors.New("no background jobs"))
	r.a.Submit("go")
	for i := 1; i <= 2; i++ {
		r.p.next(t).toolStep("", toolCall(i, toolRead, `{"id":"x"}`))
	}
	c3 := r.p.next(t)
	c3.reply("done")
	wait(t, r.a)
	got := resultsOf(c3.req)
	if len(got) != 2 || strings.Contains(got[0], "2 times") ||
		got[1] != "error: no background jobs"+repeatNote {
		t.Fatalf("results = %q", got)
	}
	if w := r.rec.texts(EvWarning); len(w) != 0 {
		t.Fatalf("warnings = %q", w)
	}
}

func TestRepeatGuard_ThirdIdenticalFailureEndsTheTurn(t *testing.T) {
	r := failingRig(t, 3, errors.New("no background jobs"))
	r.a.Submit("go")
	for i := 1; i <= 3; i++ {
		r.p.next(t).toolStep("", toolCall(i, toolRead, `{}`))
	}
	wait(t, r.a)
	noMoreStream(t, r.p)
	if w := r.rec.texts(EvWarning); len(w) == 0 || w[0] != "stopped: the same tool call kept failing" {
		t.Fatalf("warnings = %q", w)
	}
	evs := r.rec.of(EvTurnEnded)
	if len(evs) != 1 || evs[0].Finish != chat.FinishStop {
		t.Fatalf("ended = %+v", evs)
	}
	assertHistoryValid(t, r.a.History())
}

func TestRepeatGuard_TwoFailuresDoNotEndTheTurn(t *testing.T) {
	r := failingRig(t, 2, errors.New("boom"))
	r.a.Submit("go")
	for i := 1; i <= 2; i++ {
		r.p.next(t).toolStep("", toolCall(i, toolRead, `{}`))
	}
	r.p.next(t).reply("done")
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 0 {
		t.Fatalf("warnings = %q", w)
	}
}

// Three alternations are fewer than two periods of a cycle (R-44), so only the period-1 rule is tested.
func TestRepeatGuard_AlternatingCallsAreNotStopped(t *testing.T) {
	r := failingRig(t, 3, errors.New("boom"))
	r.a.Submit("go")
	for i := 1; i <= 3; i++ {
		r.p.next(t).toolStep("", toolCall(i, toolRead, fmt.Sprintf(`{"n":%d}`, i%2)))
	}
	c := r.p.next(t)
	c.reply("done")
	wait(t, r.a)
	for _, s := range resultsOf(c.req) {
		if strings.Contains(s, "2 times") {
			t.Fatalf("note on an alternating call: %q", s)
		}
	}
	if w := r.rec.texts(EvWarning); len(w) != 0 {
		t.Fatalf("warnings = %q", w)
	}
}

func TestRepeatGuard_SuccessResets(t *testing.T) {
	ft := newFakeTools()
	ft.failing["c1"], ft.failing["c2"], ft.failing["c4"], ft.failing["c5"] = errors.New("e"), errors.New("e"), errors.New("e"), errors.New("e")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolRead, `{"a":1,"b":2}`))
	r.p.next(t).toolStep("", toolCall(2, toolRead, `{"a":1,"b":2}`))
	r.p.next(t).toolStep("", toolCall(3, toolRead, `{"a":1,"b":2}`)) // succeeds: resets
	r.p.next(t).toolStep("", toolCall(4, toolRead, `{"a":1,"b":2}`))
	r.p.next(t).toolStep("", toolCall(5, toolRead, `{"a":1,"b":2}`))
	c := r.p.next(t)
	c.reply("done")
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 0 {
		t.Fatalf("a success did not reset the streak: %q", w)
	}
}

func TestRepeatGuard_DifferentErrorTextResets(t *testing.T) {
	ft := newFakeTools()
	ft.failing["c1"], ft.failing["c2"], ft.failing["c3"] = errors.New("a"), errors.New("b"), errors.New("a")
	r := toolRig(t, ft)
	r.a.Submit("go")
	for i := 1; i <= 3; i++ {
		r.p.next(t).toolStep("", toolCall(i, toolRead, `{}`))
	}
	c := r.p.next(t)
	c.reply("done")
	wait(t, r.a)
	for _, s := range resultsOf(c.req) {
		if strings.Contains(s, "2 times") {
			t.Fatalf("note: %q", s)
		}
	}
}

func TestRepeatGuard_CountsAcrossTurnsNever(t *testing.T) {
	r := failingRig(t, 2, errors.New("boom"))
	for turn := 0; turn < 2; turn++ {
		r.a.Submit("go")
		r.p.next(t).toolStep("", toolCall(turn+1, toolRead, `{}`))
		r.p.next(t).reply("done")
		wait(t, r.a)
	}
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(3, toolRead, `{}`))
	c := r.p.next(t)
	c.reply("done")
	wait(t, r.a)
	for _, s := range resultsOf(c.req) {
		if strings.Contains(s, "2 times") {
			t.Fatalf("a count leaked across Turns: %q", s)
		}
	}
}

func TestRepeatGuard_KeyOrderAndSpacingDoNotMatter(t *testing.T) {
	r := failingRig(t, 2, errors.New("e"))
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolRead, `{"a":1,"b":2}`))
	r.p.next(t).toolStep("", toolCall(2, toolRead, `{"b":2,"a":1}`))
	c := r.p.next(t)
	c.reply("done")
	wait(t, r.a)
	if got := resultsOf(c.req); len(got) != 2 || !strings.HasSuffix(got[1], repeatNote) {
		t.Fatalf("results = %q", got)
	}
}

// F12: identical calls of one Step are one decision; no note inside the batch.
func TestRepeatGuard_CallsInOneStepCountOnce(t *testing.T) {
	r := failingRig(t, 2, errors.New("e"))
	req := oneStep(t, r, toolCall(1, toolRead, `{}`), toolCall(2, toolRead, `{}`))
	if got := resultsOf(req); len(got) != 2 || strings.Contains(got[0], "2 times") || strings.Contains(got[1], "2 times") {
		t.Fatalf("results = %q", got)
	}
}

// F12 live case (KAT-Coder): five identical calls with the same outcome in ONE Step are one
// decision of the model; the Turn goes on to its answer.
func TestTools_SameCallsInOneBatchReachTheAnswer(t *testing.T) {
	ft := newFakeTools()
	var calls []chat.ToolCall
	for i := 1; i <= 5; i++ {
		ft.failing[fmt.Sprintf("c%d", i)] = errors.New("same")
		calls = append(calls, toolCall(i, toolRead, `{"p":1}`))
	}
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", calls...)
	r.p.next(t).reply("done")
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 0 {
		t.Fatalf("warnings = %q", w)
	}
	if end := r.rec.of(EvTurnEnded); end[0].Message.Content != "done" {
		t.Fatalf("TurnEnded = %+v", end[0])
	}
}

// R-90: an unknown tool name that shares a word with offered tools names them (the 9B called
// `save_tool` for `mcp__demo__save` in the T8 eval).
func TestSimilarTools(t *testing.T) {
	specs := []chat.ToolSpec{{Name: "read_file"}, {Name: "write_file"}, {Name: "mcp__demo__save"}, {Name: "mcp__demo__lookup"}, {Name: "shell"}}
	cases := map[string]string{
		"save_tool":   "mcp__demo__save",
		"file":        "read_file, write_file",
		"lookup":      "mcp__demo__lookup",
		"teleport":    "",
		"mcp__x__ok":  "",
		"write-files": "write_file",
	}
	for name, want := range cases {
		if got := strings.Join(similarTools(name, specs), ", "); got != want {
			t.Errorf("similarTools(%q) = %q, want %q", name, got, want)
		}
	}
}
