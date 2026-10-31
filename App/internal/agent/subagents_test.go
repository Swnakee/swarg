package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Helpers: a rig whose agent types open Sources with their own fake Providers
// ---------------------------------------------------------------------------

// stubTools is fakeTools that also reports the path an edit changes (ToolStubber).
type stubTools struct{ *fakeTools }

func (stubTools) Stub(chat.ToolCall, string, string) string { return "" }
func (stubTools) Touched(c chat.ToolCall) string {
	if c.Name != toolEdit {
		return ""
	}
	var a struct{ Path string }
	_ = json.Unmarshal([]byte(c.Args), &a)
	return a.Path
}

// agentWorld holds the fake servers of the agent Sources.
type agentWorld struct {
	mu    sync.Mutex
	provs map[string]*fakeProvider
	slots map[string]int
}

func (w *agentWorld) prov(name string) *fakeProvider {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.provs[name] == nil {
		w.provs[name] = newProvider()
	}
	return w.provs[name]
}

// agentRig starts a rig whose `agent` Tool offers types; a type's Server names its fake server
// ("" = "srv"), its Model defaults to "am"; w.slots gives the slots per Source (default 4).
func agentRig(t *testing.T, tr ToolRunner, types []AgentType, mod ...func(*Deps)) (*rig, *agentWorld) {
	t.Helper()
	w := &agentWorld{provs: map[string]*fakeProvider{}, slots: map[string]int{}}
	open := func(ty AgentType) (AgentRun, error) {
		srv, model := ty.Server, ty.Model
		if srv == "" {
			srv = "srv"
		}
		if model == "" {
			model = "am"
		}
		return AgentRun{Source: Source{Name: srv, Provider: w.prov(srv), Model: model, ContextLimit: 1000}, System: "AGENT:" + ty.Name}, nil
	}
	slotsFn := func(src string) int {
		w.mu.Lock()
		defer w.mu.Unlock()
		if n, ok := w.slots[src]; ok {
			return n
		}
		return 4
	}
	mods := append([]func(*Deps){func(d *Deps) {
		d.Tools = tr
		d.Agents = AgentDeps{Types: func() []AgentType { return types }, Open: open, Slots: slotsFn}
	}}, mod...)
	return startRig(t, mods...), w
}

var generalType = AgentType{Name: "general", Description: "does jobs", Tools: []string{"*"}}

func agentCall(i int, typ, goal string) chat.ToolCall {
	return toolCall(i, agentTool, fmt.Sprintf(`{"type":%q,"goal":%q}`, typ, goal))
}

// goalOf reads the job out of an agent's first user message.
func jobOf(req chat.Request) string {
	return strings.TrimPrefix(strings.SplitN(req.Messages[1].Content, "\n", 2)[0], "Job from the main agent: ")
}

// agentResult returns the ToolResult of the agent call with id.
func agentResult(t *testing.T, h []chat.Message, id string) chat.Message {
	t.Helper()
	for _, m := range h {
		if m.Role == chat.RoleTool && m.ToolCallID == id {
			return m
		}
	}
	t.Fatalf("no result for %s in %v", id, roles(h))
	return chat.Message{}
}

// ---------------------------------------------------------------------------
// One agent: result, own Source, allowlist, no nesting
// ---------------------------------------------------------------------------

func TestAgent_OneRunReportsLikeATask(t *testing.T) {
	ft := newFakeTools()
	r, w := agentRig(t, ft, []AgentType{generalType})
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "general", "find the port"))
	ac := w.prov("srv").next(t)
	if ac.req.Messages[0].Content != "AGENT:general" || ac.req.Model != "am" {
		t.Fatalf("agent request: system %q model %q", ac.req.Messages[0].Content, ac.req.Model)
	}
	if got := jobOf(ac.req); got != "find the port" {
		t.Fatalf("job = %q", got)
	}
	ac.toolStep("", toolCall(2, toolRead, ""))
	w.prov("srv").next(t).reply("port 8081")
	last := r.p.next(t)
	last.reply("the answer")
	wait(t, r.a)

	res := agentResult(t, r.a.History(), "c1")
	if want := "done\nport 8081\nsteps: 2/15"; res.Content != want {
		t.Fatalf("result = %q, want %q", res.Content, want)
	}
	if res.Label != "general: find the port" {
		t.Errorf("label = %q", res.Label)
	}
	for _, m := range last.req.Messages {
		if strings.Contains(m.Content, "out:c2") {
			t.Fatal("the agent's read reached the Session")
		}
	}
	assertHistoryValid(t, r.a.History())
}

func TestAgent_OwnSourceAndModel(t *testing.T) {
	far := AgentType{Name: "far", Description: "d", Server: "other", Model: "big", Tools: []string{"*"}}
	r, w := agentRig(t, newFakeTools(), []AgentType{far})
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "far", "x"))
	ac := w.prov("other").next(t)
	if ac.req.Model != "big" {
		t.Fatalf("agent model = %q", ac.req.Model)
	}
	ac.reply("r")
	r.p.next(t).reply("a")
	wait(t, r.a)
	if len(w.prov("srv").calls) != 0 {
		t.Fatal("a request went to the wrong server")
	}
}

func TestAgent_ToolAllowlist(t *testing.T) {
	ro := AgentType{Name: "ro", Description: "d", Tools: []string{"readonly"}}
	r, w := agentRig(t, newFakeTools(), []AgentType{ro})
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "ro", "x"))
	ac := w.prov("srv").next(t)
	if got := toolNames(ac.req); !slices.Equal(got, []string{toolRead}) {
		t.Fatalf("agent tools = %v", got)
	}
	ac.toolStep("", toolCall(2, toolMut, ""))
	second := w.prov("srv").next(t)
	last := second.req.Messages[len(second.req.Messages)-1]
	if !strings.Contains(last.Content, "not available to this agent") {
		t.Fatalf("mutating call result = %q", last.Content)
	}
	second.reply("r")
	r.p.next(t).reply("a")
	wait(t, r.a)
	for _, e := range r.tools(t).events() {
		if e == "s:c2" {
			t.Fatal("a tool outside the allowlist ran")
		}
	}
}

func (r *rig) tools(t *testing.T) *fakeTools {
	t.Helper()
	switch v := r.a.deps.Tools.(type) {
	case *fakeTools:
		return v
	case stubTools:
		return v.fakeTools
	}
	t.Fatal("no fakeTools")
	return nil
}

func TestAgent_NoNesting(t *testing.T) {
	r, w := agentRig(t, newFakeTools(), []AgentType{generalType}, tasks(TaskLimits{MaxDepth: 2}))
	r.a.Submit("go")
	parent := r.p.next(t)
	if got := toolNames(parent.req); !slices.Contains(got, agentTool) || !slices.Contains(got, taskTool) {
		t.Fatalf("session tools = %v", got)
	}
	parent.toolStep("", agentCall(1, "general", "x"))
	ac := w.prov("srv").next(t)
	if got := toolNames(ac.req); slices.Contains(got, agentTool) || slices.Contains(got, taskTool) {
		t.Fatalf("agent tools = %v, want neither agent nor task", got)
	}
	ac.toolStep("", agentCall(2, "general", "y"), taskCall(3, "z"))
	second := w.prov("srv").next(t)
	msgs := second.req.Messages
	for _, m := range msgs[len(msgs)-2:] {
		if !strings.Contains(m.Content, "unknown tool") {
			t.Fatalf("nested call result = %q", m.Content)
		}
	}
	second.reply("r")
	r.p.next(t).reply("a")
	wait(t, r.a)
}

func TestAgent_NotOfferedWithoutTypes(t *testing.T) {
	r, _ := agentRig(t, newFakeTools(), nil)
	r.a.Submit("go")
	parent := r.p.next(t)
	if got := toolNames(parent.req); slices.Contains(got, agentTool) {
		t.Fatalf("tools = %v, want no agent", got)
	}
	parent.toolStep("", agentCall(1, "general", "x"))
	next := r.p.next(t)
	if !strings.Contains(next.req.Messages[len(next.req.Messages)-1].Content, "unknown tool") {
		t.Fatal("an agent call without types must be an unknown tool")
	}
	next.reply("a")
	wait(t, r.a)
}

func TestAgent_RefusesBadCalls(t *testing.T) {
	r, _ := agentRig(t, newFakeTools(), []AgentType{generalType})
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "general", " "), agentCall(2, "nope", "x"))
	next := r.p.next(t)
	h := next.req.Messages
	if !strings.Contains(h[len(h)-2].Content, errAgentNoGoal) || !strings.Contains(h[len(h)-1].Content, `unknown type "nope"`) {
		t.Fatalf("results = %q / %q", h[len(h)-2].Content, h[len(h)-1].Content)
	}
	next.reply("a")
	wait(t, r.a)
}

// ---------------------------------------------------------------------------
// Parallel agents and the slots
// ---------------------------------------------------------------------------

// runAgents sends n agent calls in one step, answers each agent with its job, and returns the
// highest number of requests that were in flight on the agent server at once.
func runAgents(t *testing.T, n int, w *agentWorld, r *rig, wantAtOnce int) int {
	t.Helper()
	var calls []chat.ToolCall
	for i := 1; i <= n; i++ {
		calls = append(calls, agentCall(i, "general", fmt.Sprintf("job%d", i)))
	}
	r.a.Submit("go")
	r.p.next(t).toolStep("", calls...)
	srv := w.prov("srv")
	var held []*call
	for i := 0; i < wantAtOnce; i++ {
		held = append(held, srv.next(t))
	}
	if len(srv.calls) != 0 {
		t.Fatalf("more than %d agent requests at once", wantAtOnce)
	}
	started := wantAtOnce
	for answered := 0; answered < n; answered++ {
		c := held[0]
		held = held[1:]
		c.reply("did " + jobOf(c.req))
		if started < n { // the answer freed a slot: a queued agent starts
			held = append(held, srv.next(t))
			started++
		}
	}
	r.p.next(t).reply("all done")
	wait(t, r.a)
	for i := 1; i <= n; i++ {
		res := agentResult(t, r.a.History(), fmt.Sprintf("c%d", i))
		if want := fmt.Sprintf("done\ndid job%d\nsteps: 1/15", i); res.Content != want {
			t.Fatalf("c%d = %q, want %q", i, res.Content, want)
		}
	}
	return srv.maxConcurrent()
}

func TestAgent_FourRunAtOnceAndTheFifthQueues(t *testing.T) {
	r, w := agentRig(t, newFakeTools(), []AgentType{generalType})
	if got := runAgents(t, 5, w, r, 4); got != 4 {
		t.Fatalf("max concurrent agent requests = %d, want 4", got)
	}
}

func TestAgent_SourceLimitSerializes(t *testing.T) {
	r, w := agentRig(t, newFakeTools(), []AgentType{generalType})
	w.slots["srv"] = 1
	if got := runAgents(t, 3, w, r, 1); got != 1 {
		t.Fatalf("max concurrent agent requests = %d, want 1", got)
	}
}

func TestAgent_GlobalCapBeatsSourceSlots(t *testing.T) {
	r, w := agentRig(t, newFakeTools(), []AgentType{generalType}, func(d *Deps) { d.Agents.Max = 2 })
	w.slots["srv"] = 4
	if got := runAgents(t, 4, w, r, 2); got != 2 {
		t.Fatalf("max concurrent = %d, want 2", got)
	}
}

func TestAgent_SameJobTwiceIsRefused(t *testing.T) {
	r, w := agentRig(t, newFakeTools(), []AgentType{generalType})
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "general", "Same job"), agentCall(2, "general", "same  job"))
	ac := w.prov("srv").next(t)
	ac.reply("r")
	next := r.p.next(t)
	var refused int
	for _, m := range next.req.Messages {
		if strings.Contains(m.Content, errAgentRepeated) {
			refused++
		}
	}
	if refused != 1 {
		t.Fatalf("refused %d, want 1", refused)
	}
	next.reply("a")
	wait(t, r.a)
}

// ---------------------------------------------------------------------------
// File lock, approvals
// ---------------------------------------------------------------------------

func TestPathLocks_SecondHolderRefusedThenFree(t *testing.T) {
	var l pathLocks
	if _, ok := l.lock("a/b.go", "agent a1"); !ok {
		t.Fatal("first lock refused")
	}
	if who, ok := l.lock("a/b.go", "agent a2"); ok || who != "agent a1" {
		t.Fatalf("second lock = %q, %v", who, ok)
	}
	l.unlock("a/b.go")
	if _, ok := l.lock("a/b.go", "agent a2"); !ok {
		t.Fatal("lock not freed")
	}
}

func TestAgent_TwoEditsOfOneFileAtOnce(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c11")
	r, w := agentRig(t, stubTools{ft}, []AgentType{generalType})
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "general", "job1"), agentCall(2, "general", "job2"))
	srv := w.prov("srv")
	byJob := map[string]*call{}
	for i := 0; i < 2; i++ {
		c := srv.next(t)
		byJob[jobOf(c.req)] = c
	}
	byJob["job1"].toolStep("", toolCall(11, toolEdit, `{"path":"f.txt"}`))
	ft.waitStarted(t, "c11")
	byJob["job2"].toolStep("", toolCall(21, toolEdit, `{"path":"f.txt"}`))
	blocked := srv.next(t) // job2's next request: its edit was refused
	last := blocked.req.Messages[len(blocked.req.Messages)-1]
	if !strings.Contains(last.Content, "f.txt is being written by agent") {
		t.Fatalf("second edit result = %q", last.Content)
	}
	ft.release("c11")
	blocked.reply("could not edit")
	srv.next(t).reply("edited") // job1 after its edit
	r.p.next(t).reply("a")
	wait(t, r.a)
	res := agentResult(t, r.a.History(), "c1")
	if !strings.HasPrefix(res.Content, "done; files: f.txt") {
		t.Fatalf("job1 result = %q, want the file listed", res.Content)
	}
	if strings.Contains(agentResult(t, r.a.History(), "c2").Content, "files:") {
		t.Error("the refused agent must not list the file")
	}
	for _, e := range ft.events() {
		if e == "s:c21" {
			t.Fatal("the refused edit ran")
		}
	}
}

func TestAgent_ApprovalsOneAtATime(t *testing.T) {
	ft := newFakeTools()
	var mu sync.Mutex
	cur, peak, asked := 0, 0, 0
	release := make(chan struct{})
	approve := func(ctx context.Context, c chat.ToolCall) Approval {
		mu.Lock()
		cur++
		asked++
		peak = max(peak, cur)
		mu.Unlock()
		<-release
		mu.Lock()
		cur--
		mu.Unlock()
		return Approval{Allow: true}
	}
	r, w := agentRig(t, ft, []AgentType{generalType}, func(d *Deps) { d.Approve = approve })
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "general", "job1"), agentCall(2, "general", "job2"))
	srv := w.prov("srv")
	a, b := srv.next(t), srv.next(t)
	a.toolStep("", toolCall(11, toolAsk, ""))
	b.toolStep("", toolCall(21, toolAsk, ""))
	release <- struct{}{} // the first prompt is answered; only then the second is shown
	release <- struct{}{}
	srv.next(t).reply("r1")
	srv.next(t).reply("r2")
	r.p.next(t).reply("a")
	wait(t, r.a)
	mu.Lock()
	defer mu.Unlock()
	if peak != 1 || asked != 2 {
		t.Fatalf("peak prompts %d, asked %d; want 1 and 2", peak, asked)
	}
}

// ---------------------------------------------------------------------------
// Esc, spend, Store
// ---------------------------------------------------------------------------

func TestAgent_InterruptStopsAllAgents(t *testing.T) {
	r, w := agentRig(t, newFakeTools(), []AgentType{generalType})
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "general", "job1"), agentCall(2, "general", "job2"))
	srv := w.prov("srv")
	a, b := srv.next(t), srv.next(t)
	r.a.Interrupt()
	a.say("partial")
	b.say("partial")
	a.end(chat.FinishStop, nil)
	b.end(chat.FinishStop, nil)
	wait(t, r.a)
	h := r.a.History()
	for _, id := range []string{"c1", "c2"} {
		if res := agentResult(t, h, id); !strings.HasPrefix(res.Content, statusInterrupted) {
			t.Errorf("%s = %q, want interrupted", id, res.Content)
		}
	}
	if len(r.p.calls) != 0 {
		t.Error("the Session asked the model again after Esc")
	}
}

func TestAgent_AbortCancelsRequestsOfAllAgents(t *testing.T) {
	r, w := agentRig(t, newFakeTools(), []AgentType{generalType})
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "general", "job1"), agentCall(2, "general", "job2"))
	srv := w.prov("srv")
	a, b := srv.next(t), srv.next(t)
	r.a.Abort()
	for _, c := range []*call{a, b} {
		select {
		case <-c.ctx.Done():
		case <-time.After(deadline):
			t.Fatal("an agent request was not disconnected")
		}
	}
	wait(t, r.a)
	for _, id := range []string{"c1", "c2"} {
		if res := agentResult(t, r.a.History(), id); !strings.HasPrefix(res.Content, statusAborted) {
			t.Errorf("%s = %q, want aborted", id, res.Content)
		}
	}
}

func TestAgent_InterruptWhileQueued(t *testing.T) {
	r, w := agentRig(t, newFakeTools(), []AgentType{generalType})
	w.slots["srv"] = 1
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "general", "job1"), agentCall(2, "general", "job2"))
	a := w.prov("srv").next(t) // the other one waits for the slot
	r.a.Interrupt()
	a.say("x")
	a.end(chat.FinishStop, nil)
	wait(t, r.a)
	for _, id := range []string{"c1", "c2"} {
		// Esc may come before the second agent is registered: then the call is cancelled unstarted
		if res := agentResult(t, r.a.History(), id); !strings.HasPrefix(res.Content, statusInterrupted) && res.Content != resCancelled {
			t.Errorf("%s = %q, want interrupted or cancelled", id, res.Content)
		}
	}
}

func TestAgent_SpendOfAgentsAddsToTheSession(t *testing.T) {
	r, w := agentRig(t, newFakeTools(), []AgentType{generalType})
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "general", "job1"), agentCall(2, "general", "job2"))
	srv := w.prov("srv")
	for i := 0; i < 2; i++ {
		c := srv.next(t)
		c.ch <- chat.StreamEvent{Kind: chat.EventUsage, Usage: chat.Usage{PromptTokens: 5, CompletionTokens: 1, Cost: 0.25}}
		c.reply("r")
	}
	r.p.next(t).reply("a")
	wait(t, r.a)
	if got := r.a.Spend(); got.Total != 0.5 || got.ByModel["am"] != 0.5 {
		t.Fatalf("spend = %+v, want 0.5 on am", got)
	}
}

func TestAgent_MessagesNeverReachTheStoreOrTheHistory(t *testing.T) {
	st := &fakeStore{}
	r, w := agentRig(t, newFakeTools(), []AgentType{generalType}, func(d *Deps) { d.Store = st })
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "general", "job1"))
	ac := w.prov("srv").next(t)
	ac.toolStep("", toolCall(2, toolRead, ""))
	w.prov("srv").next(t).reply("r")
	r.p.next(t).reply("a")
	wait(t, r.a)
	for _, op := range st.all() {
		if op.kind == "message" && (op.msg.Name == toolRead || strings.HasPrefix(op.msg.Content, "Job from")) {
			t.Fatalf("an agent Message reached the Store: %+v", op.msg)
		}
	}
	if got := roles(r.a.History()); len(got) != 4 {
		t.Fatalf("history = %v, want user, assistant, tool, assistant", got)
	}
}

func TestAgent_EventsAreProgressLinesOnTheCallBlock(t *testing.T) {
	r, w := agentRig(t, newFakeTools(), []AgentType{generalType})
	r.a.Submit("go")
	r.p.next(t).toolStep("", agentCall(1, "general", "job1"))
	ac := w.prov("srv").next(t)
	ac.say("thinking aloud")
	ac.toolStep("", toolCall(2, toolRead, ""))
	w.prov("srv").next(t).reply("r")
	r.p.next(t).reply("a")
	wait(t, r.a)
	for _, ev := range r.rec.all() {
		if isAgentID(ev.Task) {
			t.Fatalf("an agent event reached the UI: %+v", ev)
		}
		if ev.Kind == EvDelta && ev.Text == "thinking aloud" {
			t.Fatal("the agent's text reached the UI")
		}
	}
	var lines []string
	for _, ev := range r.rec.of(EvToolOutput) {
		if ev.Call.ID == "c1" {
			lines = append(lines, ev.Text)
		}
	}
	want := []string{"model am\n", "step 1/15 L:read{}\n"}
	if !slices.Equal(lines, want) {
		t.Fatalf("progress lines = %q, want %q", lines, want)
	}
}

func TestAgentType_Allows(t *testing.T) {
	for _, tc := range []struct {
		tools    []string
		name     string
		readOnly bool
		want     bool
	}{
		{nil, "x", false, true},
		{[]string{"*"}, "x", false, true},
		{[]string{"readonly"}, "grep", true, true},
		{[]string{"readonly"}, "edit", false, false},
		{[]string{"readonly", "shell"}, "shell", false, true},
		{[]string{"read_file"}, "grep", true, false},
	} {
		if got := (AgentType{Tools: tc.tools}).Allows(tc.name, tc.readOnly); got != tc.want {
			t.Errorf("%v %s ro=%v: %v, want %v", tc.tools, tc.name, tc.readOnly, got, tc.want)
		}
	}
}
