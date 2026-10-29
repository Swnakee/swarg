package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// taskCall builds the `task` call "c<i>" with a goal.
func taskCall(i int, goal string) chat.ToolCall {
	return toolCall(i, taskTool, fmt.Sprintf(`{"goal":%q}`, goal))
}

// tasks sets the Task limits of a rig.
func tasks(l TaskLimits) func(*Deps) { return func(d *Deps) { d.Tasks = l } }

// toolNames lists the tool names of a request.
func toolNames(req chat.Request) (out []string) {
	for _, t := range req.Tools {
		out = append(out, t.Name)
	}
	return out
}

// taskResultOf returns the ToolResult of the first `task` call in h.
func taskResultOf(t *testing.T, h []chat.Message) chat.Message {
	t.Helper()
	for _, m := range h {
		if m.Role == chat.RoleTool && m.Name == taskTool {
			return m
		}
	}
	t.Fatalf("no task result in %+v", roles(h))
	return chat.Message{}
}

// oneTask plays the simplest Chain: the parent starts a Task, the child reads once and reports,
// the parent answers. It returns the child's two requests and the parent's final one.
func oneTask(t *testing.T, r *rig, report string) (child1, child2, parent2 *call) {
	t.Helper()
	r.a.Submit("find the port")
	r.p.next(t).toolStep("", taskCall(1, "collect facts"))
	child1 = r.p.next(t)
	child1.toolStep("", toolCall(2, toolRead, ""))
	child2 = r.p.next(t)
	child2.reply(report)
	parent2 = r.p.next(t)
	parent2.reply("the answer")
	wait(t, r.a)
	return child1, child2, parent2
}

// ---------------------------------------------------------------------------
// Clean return (R-37), the result (R-38)
// ---------------------------------------------------------------------------

func TestTask_CleanReturn(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1}))
	child1, _, parent2 := oneTask(t, r, "port 8081")

	if got := roles(child1.req.Messages); len(got) != 2 || got[1] != chat.RoleUser {
		t.Fatalf("child request = %v, want system + goal", got)
	}
	if !strings.HasPrefix(child1.req.Messages[1].Content, "Sub-task: collect facts\n") {
		t.Fatalf("goal frame = %q", child1.req.Messages[1].Content)
	}
	for _, m := range parent2.req.Messages {
		if strings.Contains(m.Content, "out:c2") {
			t.Fatalf("the child's read reached the parent: %+v", m)
		}
	}
	h := r.a.History()
	assertHistoryValid(t, h)
	if got := roles(h); len(got) != 4 {
		t.Fatalf("history = %v, want user, assistant, tool, assistant", got)
	}
	res := taskResultOf(t, h)
	if want := "done\nport 8081\nsteps: 2/15"; res.Content != want {
		t.Fatalf("result = %q, want %q", res.Content, want)
	}
	if res.Brief != "done" || res.Label != "collect facts" {
		t.Fatalf("brief %q label %q", res.Brief, res.Label)
	}
}

func TestTask_EmptyReportGetsStepList(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1}))
	oneTask(t, r, "")
	res := taskResultOf(t, r.a.History())
	if want := "done\nread L:read{}: brief:c2\nsteps: 2/15"; res.Content != want {
		t.Fatalf("result = %q, want %q", res.Content, want)
	}
}

func TestTask_SpecOfferedBelowMaxDepthOnly(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1}))
	r.a.Submit("go")
	parent := r.p.next(t)
	if got := toolNames(parent.req); !strings.Contains(fmt.Sprint(got), taskTool) {
		t.Fatalf("parent tools = %v, want task", got)
	}
	parent.toolStep("", taskCall(1, "x"))
	child := r.p.next(t)
	if got := toolNames(child.req); strings.Contains(fmt.Sprint(got), taskTool) {
		t.Fatalf("child tools = %v, want no task at MaxDepth", got)
	}
	child.reply("r")
	r.p.next(t).reply("a")
	wait(t, r.a)
}

func TestTask_StepLimitStatus(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1, MaxSteps: 1}))
	r.a.Submit("go")
	r.p.next(t).toolStep("", taskCall(1, "x"))
	r.p.next(t).toolStep("", toolCall(2, toolRead, "")) // step 1 = the limit: no second child request
	r.p.next(t).reply("a")
	wait(t, r.a)
	res := taskResultOf(t, r.a.History())
	if !strings.HasPrefix(res.Content, statusStepLimit+"\nread L:read{}") || !strings.HasSuffix(res.Content, "steps: 1/1") {
		t.Fatalf("result = %q", res.Content)
	}
}

func TestTask_ProviderErrorIsFailedResult(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1}))
	r.a.Submit("go")
	r.p.next(t).toolStep("", taskCall(1, "x"))
	r.p.mu.Lock()
	r.p.failNext = []error{errors.New("boom")}
	r.p.mu.Unlock()
	r.p.next(t).reply("a") // the parent continues (R-7)
	wait(t, r.a)
	h := r.a.History()
	res := taskResultOf(t, h)
	if !res.Failed || !strings.HasPrefix(res.Content, "error: failed: boom") {
		t.Fatalf("result = %+v", res)
	}
	if h[len(h)-1].Content != "a" {
		t.Fatalf("the Turn did not go on: %v", roles(h))
	}
}

func TestTask_DeniedCountsOnce(t *testing.T) {
	deny := func(context.Context, chat.ToolCall) Approval { return Approval{} }
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1}), func(d *Deps) { d.Approve = deny })
	r.a.Submit("go")
	r.p.next(t).toolStep("", taskCall(1, "x"))
	r.p.next(t).toolStep("", toolCall(2, toolAsk, ""))
	r.p.next(t).toolStep("", toolCall(3, toolAsk, "")) // second denial ends the Task
	r.p.next(t).reply("a")                             // one denial for the parent: it goes on
	wait(t, r.a)
	h := r.a.History()
	if res := taskResultOf(t, h); !strings.HasPrefix(res.Content, statusDenied) {
		t.Fatalf("result = %q", res.Content)
	}
	if h[len(h)-1].Content != "a" {
		t.Fatalf("the Turn did not go on: %v", roles(h))
	}
}

func TestTask_ChildMessagesNotStored(t *testing.T) {
	st := &fakeStore{}
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1}), func(d *Deps) { d.Store = st })
	oneTask(t, r, "r")
	for _, op := range st.all() {
		if op.kind == "message" && (op.msg.Name == toolRead || strings.HasPrefix(op.msg.Content, "Sub-task")) {
			t.Fatalf("a Task Message reached the Store: %+v", op.msg)
		}
	}
	if len(st.all()) == 0 {
		t.Fatal("the Session's Messages were not stored")
	}
}

func TestTask_EventsCarryId(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1}))
	oneTask(t, r, "r")
	started := r.rec.of(EvTaskStarted)
	if len(started) != 1 || started[0].Task != "t1" || started[0].Depth != 1 || started[0].Text != "collect facts" {
		t.Fatalf("TaskStarted = %+v", started)
	}
	var childStarts, sessionStarts int
	for _, ev := range r.rec.of(EvToolStart) {
		if ev.Task == "t1" {
			childStarts++
		} else {
			sessionStarts++
		}
	}
	if childStarts != 1 || sessionStarts != 1 {
		t.Fatalf("tool starts: child %d session %d", childStarts, sessionStarts)
	}
	if ended := r.rec.of(EvTaskEnded); len(ended) != 1 || ended[0].Brief != "done" {
		t.Fatalf("TaskEnded = %+v", ended)
	}
}

// ---------------------------------------------------------------------------
// Guards (R-39)
// ---------------------------------------------------------------------------

func TestTask_GuardsNoRequest(t *testing.T) {
	cases := []struct {
		name   string
		limits TaskLimits
		child  chat.ToolCall // what the child calls
		want   string
	}{
		{"depth", TaskLimits{MaxDepth: 1}, taskCall(2, "deeper"), errTaskDepth},
		{"repeated ancestor", TaskLimits{MaxDepth: 2}, taskCall(2, " Collect  FACTS "), errTaskRepeated},
		{"empty goal", TaskLimits{MaxDepth: 2}, taskCall(2, ""), errTaskNoGoal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := toolRig(t, newFakeTools(), tasks(tc.limits))
			r.a.Submit("go")
			r.p.next(t).toolStep("", taskCall(1, "collect facts"))
			r.p.next(t).toolStep("", tc.child)
			r.p.next(t).reply("") // the child's next request at once: the refusal made none of its own
			r.p.next(t).reply("a")
			wait(t, r.a)
			res := taskResultOf(t, r.a.History()) // an empty report: the step list shows the refusal
			if !strings.Contains(res.Content, "error: "+tc.want) {
				t.Fatalf("result = %q, want the refusal %q", res.Content, tc.want)
			}
		})
	}
}

func TestTask_RepeatsTurnTextAndCap(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1, MaxPerTurn: 1}))
	r.a.Submit("collect facts")
	r.p.next(t).toolStep("", taskCall(1, "collect FACTS")) // the Turn's own text: refused
	r.p.next(t).toolStep("", taskCall(2, "other"))         // the first real Task
	r.p.next(t).reply("r")
	r.p.next(t).toolStep("", taskCall(3, "another")) // the cap: refused
	r.p.next(t).reply("a")
	wait(t, r.a)
	h := r.a.History()
	var got []string
	for _, m := range h {
		if m.Role == chat.RoleTool {
			got = append(got, m.Brief)
		}
	}
	want := []string{"error: " + errTaskRepeated, "done", "error: " + errTaskCap}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("briefs = %q, want %q", got, want)
	}
}

func TestTask_TurnTasksResetPerTurn(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1, MaxPerTurn: 1}))
	for i := 0; i < 2; i++ {
		r.a.Submit(fmt.Sprintf("go %d", i))
		r.p.next(t).toolStep("", taskCall(1, fmt.Sprintf("x %d", i)))
		r.p.next(t).reply("r")
		r.p.next(t).reply("a")
		wait(t, r.a)
	}
	for _, m := range r.a.History() {
		if m.Role == chat.RoleTool && m.Failed {
			t.Fatalf("a Task of the second Turn was refused: %q", m.Content)
		}
	}
}

func TestTask_PromptCost(t *testing.T) {
	cost := chat.EstimateTokens(TaskBlock) + chat.EstimateBytes(len(taskSpec.Name)+len(taskSpec.Description)+len(taskSpec.Schema))
	if cost > 110 {
		t.Fatalf("task spec + block = %d estimated tokens, ceiling 110", cost)
	}
}

// ---------------------------------------------------------------------------
// Esc (R-40)
// ---------------------------------------------------------------------------

func TestTask_ChildRequestUsesRootCtx(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1}))
	r.a.Submit("go")
	r.p.next(t).toolStep("", taskCall(1, "x"))
	child := r.p.next(t)
	child.say("partial")
	r.rec.until(t, EvDelta, 1)
	r.a.Interrupt()
	r.rec.until(t, EvDraining, 1)
	if child.ctx.Err() != nil {
		t.Fatal("the first Esc disconnected the child's request (must drain, R-2)")
	}
	child.reply(" more") // drained, not shown
	wait(t, r.a)
	h := r.a.History()
	assertHistoryValid(t, h)
	res := taskResultOf(t, h)
	if !strings.HasPrefix(res.Content, statusInterrupted) {
		t.Fatalf("result = %q", res.Content)
	}
	if ev := r.rec.of(EvTurnEnded); len(ev) != 1 || ev[0].Finish != chat.FinishInterrupted {
		t.Fatalf("TurnEnded = %+v", ev)
	}
}

func TestTask_InterruptCancelsChildTool(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c2")
	r := toolRig(t, ft, tasks(TaskLimits{MaxDepth: 1}))
	r.a.Submit("go")
	r.p.next(t).toolStep("", taskCall(1, "x"))
	r.p.next(t).toolStep("", toolCall(2, toolMut, ""))
	ft.waitStarted(t, "c2")
	r.a.Interrupt()
	wait(t, r.a) // the held tool saw its ctx cancelled (x:c2) and the Chain ended
	if ev := ft.events(); !strings.Contains(fmt.Sprint(ev), "x:c2") {
		t.Fatalf("tool events = %v, want the child's tool cancelled", ev)
	}
	assertHistoryValid(t, r.a.History())
}

func TestTask_AbortDisconnectsChildRequest(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1}))
	r.a.Submit("go")
	r.p.next(t).toolStep("", taskCall(1, "x"))
	child := r.p.next(t)
	child.say("partial")
	r.rec.until(t, EvDelta, 1)
	r.a.Abort()
	select {
	case <-child.ctx.Done():
	case <-time.After(deadline):
		t.Fatal("Abort did not disconnect the child's request")
	}
	wait(t, r.a)
	if res := taskResultOf(t, r.a.History()); !strings.HasPrefix(res.Content, statusAborted) {
		t.Fatalf("result = %q", res.Content)
	}
}

// ---------------------------------------------------------------------------
// /task (SubmitTask)
// ---------------------------------------------------------------------------

func TestSubmitTask_RunsGoalThenAnswers(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1}))
	if err := r.a.SubmitTask("collect facts"); err != nil {
		t.Fatal(err)
	}
	r.p.next(t).reply("r") // the child: no parent request before it
	r.p.next(t).reply("a")
	wait(t, r.a)
	h := r.a.History()
	assertHistoryValid(t, h)
	if got := roles(h); fmt.Sprint(got) != "[user assistant tool assistant]" || h[0].Content != "collect facts" {
		t.Fatalf("history = %v / %q", got, h[0].Content)
	}
	if res := taskResultOf(t, h); res.Failed {
		t.Fatalf("the synthetic call was refused: %q", res.Content)
	}
}

func TestSubmitTask_NotInjected(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	r := toolRig(t, ft, tasks(TaskLimits{MaxDepth: 1}))
	r.a.Submit("first")
	r.p.next(t).toolStep("", toolCall(1, toolMut, ""))
	ft.waitStarted(t, "c1")
	if err := r.a.SubmitTask("goal"); err != nil {
		t.Fatal(err)
	}
	ft.release("c1")
	r.p.next(t).reply("done first") // the goal was not injected into this Turn
	r.p.next(t).reply("r")          // the goal's Task
	r.p.next(t).reply("a")
	wait(t, r.a)
	for _, m := range r.a.History() {
		if m.Injected {
			t.Fatalf("the goal was injected: %+v", m)
		}
	}
	if starts := r.rec.texts(EvTurnStarted); fmt.Sprint(starts) != "[first goal]" {
		t.Fatalf("turns = %v", starts)
	}
}

func TestSubmitTask_TasksOff(t *testing.T) {
	r := toolRig(t, newFakeTools())
	if err := r.a.SubmitTask("x"); !errors.Is(err, ErrTasksOff) {
		t.Fatalf("err = %v, want ErrTasksOff", err)
	}
}

// ---------------------------------------------------------------------------
// Per-model limit (R-41, I-13)
// ---------------------------------------------------------------------------

func limits(m map[string]int) func(*Deps) {
	return func(d *Deps) {
		d.LimitFor = func(model string) int {
			if l, ok := m[model]; ok {
				return l
			}
			return d.ContextLimit
		}
	}
}

func TestLimit_FollowsModel(t *testing.T) {
	r := startRig(t, limits(map[string]int{"big": 4000}))
	if err := r.a.SetModel("big"); err != nil {
		t.Fatal(err)
	}
	if ev := r.rec.lastMeter(t); ev.Limit != 4000 {
		t.Fatalf("limit = %d, want 4000", ev.Limit)
	}
	if err := r.a.SetModel("other"); err != nil { // no entry: the provider's
		t.Fatal(err)
	}
	if ev := r.rec.lastMeter(t); ev.Limit != 1000 {
		t.Fatalf("limit = %d, want 1000", ev.Limit)
	}
}

func TestLimit_RearmsWarningsOnlyWhenLimitChanges(t *testing.T) {
	r := startRig(t, limits(map[string]int{"same": 1000, "big": 4000}))
	r.a.Submit("go")
	c := r.p.next(t)
	c.usage(900, 10) // 91%: warned
	c.reply("a")
	wait(t, r.a)
	if n := len(r.rec.of(EvWarning)); n != 1 {
		t.Fatalf("warnings = %v", r.rec.texts(EvWarning))
	}
	_ = r.a.SetModel("same") // the same limit: nothing re-armed
	r.a.Submit("go")
	c = r.p.next(t)
	c.usage(900, 10)
	c.reply("a")
	wait(t, r.a)
	if n := len(r.rec.of(EvWarning)); n != 1 {
		t.Fatalf("a model with the same limit re-fired the warning: %v", r.rec.texts(EvWarning))
	}
	_ = r.a.SetModel("big")
	r.a.Submit("go")
	c = r.p.next(t)
	c.usage(3900, 10) // 97% of 4000: a new upward crossing
	c.reply("a")
	wait(t, r.a)
	if n := len(r.rec.of(EvWarning)); n != 2 {
		t.Fatalf("warnings = %v", r.rec.texts(EvWarning))
	}
}

func TestLimit_RequestUsesCurrentModelLimit(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1}), limits(map[string]int{"big": 4000}))
	r.a.Submit("go")
	r.p.next(t).toolStep("", taskCall(1, "x"))
	child := r.p.next(t)
	_ = r.a.SetModel("big") // mid-Task
	child.toolStep("", toolCall(2, toolRead, ""))
	child2 := r.p.next(t) // built after the switch: the child's meter names the new limit (I-13)
	r.a.mu.Lock()
	limit := r.a.chain[1].meter.limit
	r.a.mu.Unlock()
	if limit != 4000 || child2.req.Model != "big" {
		t.Fatalf("child limit %d model %q, want 4000 / big", limit, child2.req.Model)
	}
	child2.reply("r")
	r.p.next(t).reply("a")
	wait(t, r.a)
}

// ---------------------------------------------------------------------------
// Reasoning (R-42, R-43)
// ---------------------------------------------------------------------------

func TestAgent_ReasoningStoredAndSentBackThisTurnOnly(t *testing.T) {
	r := toolRig(t, newFakeTools())
	r.a.Submit("go")
	c := r.p.next(t)
	c.ch <- chat.StreamEvent{Kind: chat.EventReasoning, Text: "think"}
	c.toolStep("", toolCall(1, toolRead, ""))
	c2 := r.p.next(t)
	if m := c2.req.Messages[2]; m.Role != chat.RoleAssistant || m.Reasoning != "think" {
		t.Fatalf("the reasoning was not sent back: %+v", m)
	}
	c2.reply("a")
	wait(t, r.a)
	r.a.Submit("next")
	c3 := r.p.next(t)
	for _, m := range c3.req.Messages {
		if m.Reasoning != "" {
			t.Fatalf("an earlier Turn's reasoning was sent: %+v", m)
		}
	}
	c3.reply("b")
	wait(t, r.a)
	if h := r.a.History(); h[1].Reasoning != "think" {
		t.Fatalf("history[1] = %+v, want the reasoning kept", h[1])
	}
}

func TestSpeed_FirstDeltaOfAnyKind(t *testing.T) {
	t0 := time.Unix(0, 0)
	var c stepClock
	c.start = t0
	c.note(chat.StreamEvent{Kind: chat.EventReasoning, Text: "r"}, t0.Add(1*time.Second))
	c.note(chat.StreamEvent{Kind: chat.EventDelta, Text: "t"}, t0.Add(3*time.Second))
	c.note(chat.StreamEvent{Kind: chat.EventUsage, Usage: chat.Usage{CompletionTokens: 40}}, t0.Add(5*time.Second))
	c.note(chat.StreamEvent{Kind: chat.EventDone}, t0.Add(5*time.Second))
	tps, _, ok := c.speed()
	if !ok || tps != 10 { // 40 tokens over 4 s from the reasoning, not 20 over 2 s from the text
		t.Fatalf("speed = %v, %v; want 10 tok/s from the first reasoning delta", tps, ok)
	}
}

func TestEstimate_CountsReasoning(t *testing.T) {
	msgs := []chat.Message{{Role: chat.RoleSystem}, {Role: chat.RoleAssistant, Content: "abcd", Reasoning: "efgh"}}
	if got := estimateRequest(chat.Request{Messages: msgs}).Answers; got != 2 {
		t.Fatalf("Answers = %d, want 2 (text + reasoning)", got)
	}
}

// ---------------------------------------------------------------------------
// Code review fixes (test-first)
// ---------------------------------------------------------------------------

func TestSubmitTask_RunsEvenWhenTextQueued(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	r := toolRig(t, ft, tasks(TaskLimits{MaxDepth: 1}))
	r.a.Submit("first")
	r.p.next(t).toolStep("", toolCall(1, toolMut, ""))
	ft.waitStarted(t, "c1")
	if err := r.a.SubmitTask("goal"); err != nil {
		t.Fatal(err)
	}
	r.a.Submit("also") // queued behind the goal: R-10 must not skip the goal's own call
	ft.release("c1")
	r.p.next(t).reply("x")
	r.p.next(t).reply("r") // the goal's helper
	r.p.next(t).reply("a")
	r.p.next(t).reply("b") // "also"
	wait(t, r.a)
	for _, m := range r.a.History() {
		if m.Role == chat.RoleTool && m.Name == taskTool && !strings.HasPrefix(m.Content, statusDone) {
			t.Fatalf("the /task goal was not run: %q", m.Content)
		}
	}
}

func TestSubmitTask_InterruptDuringGoalMakesNoSessionRequest(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: 1}))
	if err := r.a.SubmitTask("goal"); err != nil {
		t.Fatal(err)
	}
	child := r.p.next(t)
	child.say("partial")
	r.rec.until(t, EvDelta, 1)
	r.a.Interrupt()
	r.rec.until(t, EvDraining, 1)
	child.reply(" more")
	wait(t, r.a) // hangs if the Session made a request nobody answers
	if ev := r.rec.of(EvTurnEnded); len(ev) != 1 || ev[0].Finish != chat.FinishInterrupted {
		t.Fatalf("TurnEnded = %+v", ev)
	}
	assertHistoryValid(t, r.a.History())
}

func TestTask_SkippedWhenTextQueuedAtDepth0Only(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	r := toolRig(t, ft, tasks(TaskLimits{MaxDepth: 1}))
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolMut, ""), taskCall(2, "x")) // c1 runs alone first, then the task call
	ft.waitStarted(t, "c1")
	r.a.Submit("also") // arrives while c1 runs: the Session's task call is skipped (R-10), "also" injected
	ft.release("c1")
	r.p.next(t).reply("a")
	wait(t, r.a)
	r.a.Submit("next")
	r.p.next(t).toolStep("", taskCall(3, "y")) // the Task runs...
	ft.hold("c4")
	r.p.next(t).toolStep("", toolCall(4, toolMut, ""))
	ft.waitStarted(t, "c4")
	r.a.Submit("more") // ...and text queued now does not skip the helper's next mutating call
	ft.release("c4")
	r.p.next(t).toolStep("", toolCall(5, toolMut, ""))
	r.p.next(t).reply("r")
	r.p.next(t).reply("b") // the Session, with "more" injected
	wait(t, r.a)
	var briefs []string
	for _, m := range r.a.History() {
		if m.Role == chat.RoleTool {
			briefs = append(briefs, m.Brief)
		}
	}
	want := "[brief:c1 skipped: the user sent a new message done]"
	if fmt.Sprint(briefs) != want {
		t.Fatalf("briefs = %q, want %s", briefs, want)
	}
}

func TestTask_ChainCancelsEveryLevel(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c3")
	r := toolRig(t, ft, tasks(TaskLimits{MaxDepth: 2}))
	r.a.Submit("go")
	r.p.next(t).toolStep("", taskCall(1, "outer"))
	r.p.next(t).toolStep("", taskCall(2, "inner"))
	r.p.next(t).toolStep("", toolCall(3, toolMut, ""))
	ft.waitStarted(t, "c3")
	r.a.Interrupt()
	wait(t, r.a)
	if ev := ft.events(); !strings.Contains(fmt.Sprint(ev), "x:c3") {
		t.Fatalf("tool events = %v, want the grandchild's tool cancelled", ev)
	}
	ended := r.rec.of(EvTaskEnded)
	if len(ended) != 2 || ended[0].Task != "t2" || ended[1].Task != "t1" {
		t.Fatalf("TaskEnded order = %+v, want innermost first", ended)
	}
	assertHistoryValid(t, r.a.History())
}

func TestLimit_DefaultWhenNoFunc(t *testing.T) {
	r := startRig(t)
	_ = r.a.SetModel("other")
	if got := r.a.limit(); got != 1000 {
		t.Fatalf("limit = %d, want the provider's 1000", got)
	}
}

// ---------------------------------------------------------------------------
// Depth 8 (T5 N2)
// ---------------------------------------------------------------------------

// maxDepthLimits is the deepest Chain the config allows.
const maxDepthLimits = 8

// startChain plays n nested `task` calls: the Session starts level 1, each level starts the next.
func startChain(t *testing.T, r *rig, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		r.p.next(t).toolStep("", taskCall(i, fmt.Sprintf("level %d", i)))
	}
}

func TestTask_Depth8ChainReturnsResultsUp(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: maxDepthLimits}))
	r.a.Submit("go")
	startChain(t, r, maxDepthLimits)
	for i := maxDepthLimits; i >= 1; i-- {
		r.p.next(t).reply(fmt.Sprintf("report %d", i)) // the innermost reports first
	}
	r.p.next(t).reply("final")
	wait(t, r.a)
	started := r.rec.of(EvTaskStarted)
	if len(started) != maxDepthLimits || started[7].Depth != maxDepthLimits {
		t.Fatalf("TaskStarted = %d events, last depth %d, want 8 and 8", len(started), started[len(started)-1].Depth)
	}
	ended := r.rec.of(EvTaskEnded)
	for i, e := range ended {
		if want := fmt.Sprintf("t%d", maxDepthLimits-i); e.Task != want || !strings.Contains(e.Output, fmt.Sprintf("report %d", maxDepthLimits-i)) {
			t.Fatalf("TaskEnded[%d] = %q %q, want %s with its own report", i, e.Task, e.Output, want)
		}
	}
	if res := taskResultOf(t, r.a.History()); !strings.Contains(res.Content, "report 1") || strings.Contains(res.Content, "report 2") {
		t.Fatalf("Session result = %q, want only level 1's report", res.Content)
	}
	assertHistoryValid(t, r.a.History())
}

func TestTask_Depth9RefusedAndToolNotOffered(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: maxDepthLimits}))
	r.a.Submit("go")
	for i := 1; i <= maxDepthLimits; i++ { // the Session and levels 1..7 start levels 1..8
		c := r.p.next(t)
		if !strings.Contains(fmt.Sprint(toolNames(c.req)), taskTool) {
			t.Fatalf("depth %d is not offered the task Tool", i-1)
		}
		c.toolStep("", taskCall(i, fmt.Sprintf("level %d", i)))
	}
	deepest := r.p.next(t) // level 8's first request
	if strings.Contains(fmt.Sprint(toolNames(deepest.req)), taskTool) {
		t.Fatalf("level 8 tools = %v, want no task", toolNames(deepest.req))
	}
	deepest.toolStep("", taskCall(8, "level 9")) // a call anyway
	for i := 0; i < maxDepthLimits; i++ {
		r.p.next(t).reply("r") // level 8 gets the refusal as a ToolResult, then everyone reports
	}
	r.p.next(t).reply("final")
	wait(t, r.a)
	if n := len(r.rec.of(EvTaskStarted)); n != maxDepthLimits {
		t.Fatalf("%d Tasks started, want 8", n)
	}
}

func TestTask_Depth8EscStopsInnermostFirst(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c9")
	r := toolRig(t, ft, tasks(TaskLimits{MaxDepth: maxDepthLimits}))
	r.a.Submit("go")
	startChain(t, r, maxDepthLimits)
	r.p.next(t).toolStep("", toolCall(9, toolMut, "")) // level 8 runs a tool
	ft.waitStarted(t, "c9")
	r.a.Interrupt()
	wait(t, r.a)
	ended := r.rec.of(EvTaskEnded)
	if len(ended) != maxDepthLimits {
		t.Fatalf("%d TaskEnded, want 8", len(ended))
	}
	for i, e := range ended {
		if want := fmt.Sprintf("t%d", maxDepthLimits-i); e.Task != want || !strings.HasPrefix(e.Output, statusInterrupted) {
			t.Fatalf("TaskEnded[%d] = %q %q, want %s interrupted", i, e.Task, e.Output, want)
		}
	}
	assertHistoryValid(t, r.a.History())
}

func TestTask_Depth8StepBudgetPerLevel(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: maxDepthLimits, MaxSteps: 1}))
	r.a.Submit("go")
	startChain(t, r, maxDepthLimits) // every level spends its one step on the next `task` call
	r.p.next(t).toolStep("", toolCall(9, toolRead, ""))
	r.p.next(t).reply("final") // the levels end by the limit without a request of their own
	wait(t, r.a)
	ended := r.rec.of(EvTaskEnded)
	if len(ended) != maxDepthLimits {
		t.Fatalf("%d TaskEnded, want 8", len(ended))
	}
	for _, e := range ended {
		if !strings.HasPrefix(e.Output, statusStepLimit) || !strings.HasSuffix(e.Output, "steps: 1/1") {
			t.Fatalf("%s result = %q, want the step limit", e.Task, e.Output)
		}
	}
}

func TestTask_Depth8CycleGuardRefusesAncestorGoal(t *testing.T) {
	r := toolRig(t, newFakeTools(), tasks(TaskLimits{MaxDepth: maxDepthLimits}))
	r.a.Submit("go")
	startChain(t, r, maxDepthLimits-1)                  // levels 1..7
	r.p.next(t).toolStep("", taskCall(8, " LEVEL  2 ")) // level 7 repeats level 2's goal
	for i := 0; i < maxDepthLimits-1; i++ {
		r.p.next(t).reply("r") // level 7 after the refusal, then levels 6..1
	}
	r.p.next(t).reply("final")
	wait(t, r.a)
	if n := len(r.rec.of(EvTaskStarted)); n != maxDepthLimits-1 {
		t.Fatalf("%d Tasks started, want 7 (the repeat refused)", n)
	}
}
