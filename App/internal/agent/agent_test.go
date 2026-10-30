package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"swarg/internal/chat"
)

const deadline = 5 * time.Second

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// call is one Stream request; the test plays the server through it.
type call struct {
	req chat.Request
	ctx context.Context
	ch  chan chat.StreamEvent
	p   *fakeProvider
}

// fakeProvider hands every Stream call to the test and counts concurrency.
type fakeProvider struct {
	calls chan *call

	mu       sync.Mutex
	failNext []error
	cur, max int
}

func newProvider() *fakeProvider { return &fakeProvider{calls: make(chan *call, 64)} }

func (p *fakeProvider) Stream(ctx context.Context, req chat.Request) (<-chan chat.StreamEvent, error) {
	p.mu.Lock()
	if len(p.failNext) > 0 {
		err := p.failNext[0]
		p.failNext = p.failNext[1:]
		p.mu.Unlock()
		return nil, err
	}
	p.cur++
	p.max = max(p.max, p.cur)
	p.mu.Unlock()
	c := &call{req: req, ctx: ctx, ch: make(chan chat.StreamEvent, 64), p: p}
	p.calls <- c
	return c.ch, nil
}

// next waits for the next Stream call.
func (p *fakeProvider) next(t *testing.T) *call {
	t.Helper()
	select {
	case c := <-p.calls:
		return c
	case <-time.After(deadline):
		t.Fatal("no Stream call")
		return nil
	}
}

func (p *fakeProvider) maxConcurrent() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.max
}

func (c *call) say(texts ...string) {
	for _, s := range texts {
		c.ch <- chat.StreamEvent{Kind: chat.EventDelta, Text: s}
	}
}

func (c *call) usage(prompt, completion int) {
	c.ch <- chat.StreamEvent{Kind: chat.EventUsage, Usage: chat.Usage{PromptTokens: prompt, CompletionTokens: completion}}
}

// end sends the final event and closes the stream.
func (c *call) end(f chat.FinishReason, err error) {
	c.ch <- chat.StreamEvent{Kind: chat.EventDone, Finish: f, Err: err}
	close(c.ch)
	c.p.mu.Lock()
	c.p.cur--
	c.p.mu.Unlock()
}

// reply says the texts and ends with stop.
func (c *call) reply(texts ...string) {
	c.say(texts...)
	c.end(chat.FinishStop, nil)
}

// recorder collects events and lets a test wait for a condition without sleeping.
type recorder struct {
	mu   sync.Mutex
	evs  []Event
	note chan struct{}
}

func newRecorder() *recorder { return &recorder{note: make(chan struct{}, 1)} }

func (r *recorder) add(ev Event) {
	r.mu.Lock()
	r.evs = append(r.evs, ev)
	r.mu.Unlock()
	signal(r.note)
}

func (r *recorder) all() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.evs...)
}

func (r *recorder) of(k EventKind) []Event {
	var out []Event
	for _, ev := range r.all() {
		if ev.Kind == k {
			out = append(out, ev)
		}
	}
	return out
}

// until waits until at least n events of kind k were recorded.
func (r *recorder) until(t *testing.T, k EventKind, n int) {
	t.Helper()
	timeout := time.After(deadline)
	for len(r.of(k)) < n {
		select {
		case <-r.note:
		case <-timeout:
			t.Fatalf("timeout waiting for %d events of kind %d", n, k)
		}
	}
}

func (r *recorder) lastMeter(t *testing.T) Event {
	t.Helper()
	m := r.of(EvMeter)
	if len(m) == 0 {
		t.Fatal("no Meter event")
	}
	return m[len(m)-1]
}

func (r *recorder) texts(k EventKind) []string {
	var out []string
	for _, ev := range r.of(k) {
		out = append(out, ev.Text)
	}
	return out
}

// rig bundles an agent with its fakes.
type rig struct {
	a   *Agent
	p   *fakeProvider
	rec *recorder
}

// newRig creates an agent (not started) with system prompt "SYS" and limit 1000.
func newRig(t *testing.T, mod ...func(*Deps)) *rig {
	t.Helper()
	p, rec := newProvider(), newRecorder()
	d := Deps{Provider: p, SystemPrompt: func() string { return "SYS" }, Model: "m", ContextLimit: 1000, OnEvent: rec.add}
	for _, f := range mod {
		f(&d)
	}
	return &rig{a: New(d), p: p, rec: rec}
}

// startRig is newRig plus Start; the worker stops when the test ends.
func startRig(t *testing.T, mod ...func(*Deps)) *rig {
	t.Helper()
	r := newRig(t, mod...)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.a.Start(ctx)
	return r
}

// wait is Agent.Wait with a deadline that only fails the test.
func wait(t *testing.T, a *Agent) {
	t.Helper()
	done := make(chan struct{})
	go func() { a.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(deadline):
		t.Fatal("Wait did not return")
	}
}

func roles(msgs []chat.Message) []chat.Role {
	var out []chat.Role
	for _, m := range msgs {
		out = append(out, m.Role)
	}
	return out
}

func wantHistory(t *testing.T, a *Agent, want ...chat.Message) {
	t.Helper()
	got := plain(a.History())
	if len(got) != len(want) {
		t.Fatalf("history = %+v, want %+v", got, want)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("history[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func user(s string) chat.Message { return chat.Message{Role: chat.RoleUser, Content: s} }
func asst(s string) chat.Message { return chat.Message{Role: chat.RoleAssistant, Content: s} }

// ---------------------------------------------------------------------------
// Turn outcomes
// ---------------------------------------------------------------------------

func TestAgent_StopStoresAssistant(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hi")
	r.p.next(t).reply("he", "llo")
	wait(t, r.a)
	wantHistory(t, r.a, user("hi"), asst("hello"))
	if end := r.rec.of(EvTurnEnded); len(end) != 1 || end[0].Finish != chat.FinishStop {
		t.Fatalf("TurnEnded = %+v", end)
	}
}

func TestAgent_SystemPromptIsMessageZero(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hi")
	c := r.p.next(t)
	c.reply("x")
	if m := c.req.Messages[0]; m.Role != chat.RoleSystem || m.Content != "SYS" {
		t.Fatalf("message 0 = %+v", m)
	}
}

func TestAgent_SystemPromptBuiltPerTurn(t *testing.T) {
	calls := 0
	r := startRig(t, func(d *Deps) { d.SystemPrompt = func() string { calls++; return "SYS" } })
	for _, s := range []string{"a", "b"} {
		r.a.Submit(s)
		r.p.next(t).reply("x")
		wait(t, r.a)
	}
	if calls != 2 {
		t.Fatalf("SystemPrompt calls = %d, want 2", calls)
	}
}

func TestAgent_LengthMarksTruncated(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hi")
	c := r.p.next(t)
	c.say("cut")
	c.end(chat.FinishLength, nil)
	wait(t, r.a)
	got := r.a.History()
	if len(got) != 2 || !got[1].Truncated || got[1].Interrupted {
		t.Fatalf("history = %+v", got)
	}
	if end := r.rec.of(EvTurnEnded); end[0].Finish != chat.FinishLength {
		t.Fatalf("TurnEnded = %+v", end[0])
	}
}

func TestAgent_EmptyAnswerNotStored(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hi")
	r.p.next(t).reply()
	wait(t, r.a)
	wantHistory(t, r.a, user("hi"))
	if w := r.rec.texts(EvWarning); len(w) != 1 || w[0] != "empty answer" {
		t.Fatalf("warnings = %q", w)
	}
}

// The server could not parse the model's tool call: finish "tool_calls" without a call. The
// Turn must not end silently: a warning, one nudge to the model, then its next answer.
func TestAgent_UnparsedToolCallAsksTheModelAgain(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hi")
	r.p.next(t).end(chat.FinishToolCalls, nil)
	c := r.p.next(t)
	if last := c.req.Messages[len(c.req.Messages)-1]; last.Role != chat.RoleUser || !strings.Contains(last.Content, unparsedNudge) {
		t.Fatalf("last request message = %+v", last)
	}
	c.reply("done")
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 1 || w[0] != unparsedWarning {
		t.Fatalf("warnings = %q", w)
	}
	if end := r.rec.of(EvTurnEnded); end[0].Finish != chat.FinishStop || end[0].Message.Content != "done" {
		t.Fatalf("TurnEnded = %+v", end[0])
	}
}

func TestAgent_UnparsedToolCallTwiceEndsTheTurn(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hi")
	r.p.next(t).end(chat.FinishToolCalls, nil)
	r.p.next(t).end(chat.FinishToolCalls, nil)
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 2 || w[0] != unparsedWarning || w[1] != unparsedStop {
		t.Fatalf("warnings = %q", w)
	}
	select {
	case c := <-r.p.calls:
		t.Fatalf("a third request: %+v", c.req)
	default:
	}
}

// The model hit max_tokens and nothing reached swarg (F11: mlx buffers a tool call that is too
// long or never ends, then cannot parse it): finish "length", no text, no call. Like F10: a
// warning, one nudge (keep the arguments short), then the model's next answer.
func TestAgent_UndeliveredOutputAsksTheModelAgain(t *testing.T) {
	r := startRig(t, func(d *Deps) { d.MaxTokens = 2048 })
	r.a.Submit("hi")
	r.p.next(t).end(chat.FinishLength, nil)
	c := r.p.next(t)
	if last := c.req.Messages[len(c.req.Messages)-1]; last.Role != chat.RoleUser || !strings.Contains(last.Content, fmt.Sprintf(undeliveredNudge, 2048)) {
		t.Fatalf("last request message = %+v", last)
	}
	c.reply("done")
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 1 || w[0] != undeliveredWarning {
		t.Fatalf("warnings = %q", w)
	}
	if end := r.rec.of(EvTurnEnded); end[0].Finish != chat.FinishStop || end[0].Message.Content != "done" {
		t.Fatalf("TurnEnded = %+v", end[0])
	}
}

func TestAgent_UndeliveredOutputTwiceEndsTheTurn(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hi")
	r.p.next(t).end(chat.FinishLength, nil)
	r.p.next(t).end(chat.FinishLength, nil)
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 2 || w[0] != undeliveredWarning || w[1] != undeliveredStop {
		t.Fatalf("warnings = %q", w)
	}
	select {
	case c := <-r.p.calls:
		t.Fatalf("a third request: %+v", c.req)
	default:
	}
}

// One nudge per loop in all: an unparsed tool call after an undelivered output ends the Turn.
func TestAgent_OneNudgePerLoop(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hi")
	r.p.next(t).end(chat.FinishLength, nil)
	r.p.next(t).end(chat.FinishToolCalls, nil)
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 2 || w[0] != undeliveredWarning || w[1] != unparsedStop {
		t.Fatalf("warnings = %q", w)
	}
}

// A thinking model that used the whole budget on reasoning is a plain cut, not a lost call.
func TestAgent_ReasoningOnlyCutIsNoNudge(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hi")
	c := r.p.next(t)
	c.ch <- chat.StreamEvent{Kind: chat.EventReasoning, Text: "hmm"}
	c.end(chat.FinishLength, nil)
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 0 {
		t.Fatalf("warnings = %q", w)
	}
}

func TestAgent_ErrorKeepsUserDropsAssistant(t *testing.T) {
	r := startRig(t)
	r.a.Submit("a")
	c := r.p.next(t)
	c.say("partial")
	c.end(chat.FinishError, errors.New("boom"))
	wait(t, r.a)
	wantHistory(t, r.a, user("a"))
	if end := r.rec.of(EvTurnEnded); end[0].Finish != chat.FinishError || end[0].Err == nil {
		t.Fatalf("TurnEnded = %+v", end[0])
	}
	r.a.Submit("b")
	r.p.next(t).reply("ok")
	wait(t, r.a)
	if got := r.a.History(); len(got) != 3 || got[2].Content != "ok" {
		t.Fatalf("history = %+v", got)
	}
}

func TestAgent_ErrorFromStreamCall(t *testing.T) {
	r := startRig(t)
	boom := errors.New("cannot reach")
	r.p.failNext = []error{boom}
	r.a.Submit("a")
	r.a.Submit("b")
	r.p.next(t).reply("ok")
	wait(t, r.a)
	end := r.rec.of(EvTurnEnded)
	if len(end) != 2 || end[0].Finish != chat.FinishError || !errors.Is(end[0].Err, boom) {
		t.Fatalf("TurnEnded = %+v", end)
	}
	wantHistory(t, r.a, user("a"), user("b"), asst("ok"))
}

func TestAgent_ConsecutiveUserMessagesMerged(t *testing.T) {
	r := startRig(t)
	r.a.Submit("a")
	r.p.next(t).end(chat.FinishError, errors.New("boom"))
	wait(t, r.a)
	r.a.Submit("b")
	c := r.p.next(t)
	c.reply("ok")
	wait(t, r.a)
	want := []chat.Message{{Role: chat.RoleSystem, Content: "SYS"}, user("a\n\nb")}
	if len(c.req.Messages) != 2 || !reflect.DeepEqual(plain(c.req.Messages)[1], want[1]) {
		t.Fatalf("request = %+v", c.req.Messages)
	}
	if got := plain(r.a.History()); len(got) != 3 || !reflect.DeepEqual(got[0], user("a")) || !reflect.DeepEqual(got[1], user("b")) {
		t.Fatalf("history = %+v", got)
	}
}

func TestAgent_ExtraAndMaxTokensPassed(t *testing.T) {
	extra := map[string]any{"k": "v"}
	r := startRig(t, func(d *Deps) { d.Extra, d.MaxTokens = extra, 77 })
	r.a.Submit("hi")
	c := r.p.next(t)
	c.reply("x")
	if c.req.MaxTokens != 77 || c.req.Extra["k"] != "v" || c.req.Model != "m" {
		t.Fatalf("request = %+v", c.req)
	}
}

// ---------------------------------------------------------------------------
// Queue
// ---------------------------------------------------------------------------

func TestAgent_SubmitWhileBusyQueuesInOrder(t *testing.T) {
	r := startRig(t)
	r.a.Submit("A")
	c1 := r.p.next(t)
	r.a.Submit("B")
	r.a.Submit("C")
	r.rec.until(t, EvQueue, 4)
	var counts []int
	for _, ev := range r.rec.of(EvQueue) {
		counts = append(counts, ev.Queued)
	}
	// The Submit and worker events of A can arrive in either order (see Gotchas in agent.md).
	slices.Sort(counts)
	if !slices.Equal(counts, []int{0, 1, 1, 2}) {
		t.Fatalf("queue counts = %v", counts)
	}
	c1.reply("a1")
	c2 := r.p.next(t)
	c2.reply("b1")
	c3 := r.p.next(t)
	c3.reply("c1")
	wait(t, r.a)
	if got := c2.req.Messages; len(got) != 4 || got[2].Content != "a1" || got[3].Content != "B" {
		t.Fatalf("request 2 = %+v", got)
	}
	if got := c3.req.Messages; got[len(got)-1].Content != "C" {
		t.Fatalf("request 3 = %+v", got)
	}
	if m := r.p.maxConcurrent(); m != 1 {
		t.Fatalf("concurrent Stream calls = %d", m)
	}
}

func TestAgent_SubmitDoesNotInterrupt(t *testing.T) {
	r := startRig(t)
	r.a.Submit("A")
	c := r.p.next(t)
	c.say("x")
	r.rec.until(t, EvDelta, 1)
	r.a.Submit("B")
	c.reply("y")
	r.p.next(t).reply("b")
	wait(t, r.a)
	if got := r.a.History(); !reflect.DeepEqual(plain(got)[1], asst("xy")) {
		t.Fatalf("history = %+v", got)
	}
	if len(r.rec.of(EvDraining)) != 0 {
		t.Fatal("unexpected Draining")
	}
}

func TestAgent_EmptyAndWhitespaceIgnored(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t "} {
		r := startRig(t)
		r.a.Submit(in)
		if r.a.Busy() {
			t.Fatalf("Busy after %q", in)
		}
		wait(t, r.a)
		if len(r.rec.all()) != 0 || len(r.p.calls) != 0 {
			t.Fatalf("%q started a turn", in)
		}
	}
}

// ---------------------------------------------------------------------------
// Interrupt
// ---------------------------------------------------------------------------

// interruptedAfter starts "A", queues more messages, streams the texts,
// interrupts and waits for Draining.
func interruptedAfter(t *testing.T, r *rig, queued []string, texts ...string) *call {
	t.Helper()
	r.a.Submit("A")
	c := r.p.next(t)
	for _, q := range queued {
		r.a.Submit(q)
	}
	c.say(texts...)
	r.rec.until(t, EvDelta, len(texts))
	r.a.Interrupt()
	r.rec.until(t, EvDraining, 1)
	return c
}

func TestAgent_InterruptStopsDeltasKeepsPartial(t *testing.T) {
	r := startRig(t)
	c := interruptedAfter(t, r, nil, "ab")
	c.say("cd")
	c.end(chat.FinishStop, nil)
	wait(t, r.a)
	if d := r.rec.texts(EvDelta); len(d) != 1 || d[0] != "ab" {
		t.Fatalf("deltas = %q", d)
	}
	wantHistory(t, r.a, user("A"), chat.Message{Role: chat.RoleAssistant, Content: "ab", Interrupted: true})
	if n := len(r.rec.of(EvDraining)); n != 1 {
		t.Fatalf("Draining events = %d", n)
	}
	if end := r.rec.of(EvTurnEnded); end[0].Finish != chat.FinishInterrupted {
		t.Fatalf("TurnEnded = %+v", end[0])
	}
}

func TestAgent_InterruptDrainsBeforeNextTurn(t *testing.T) {
	r := startRig(t)
	c := interruptedAfter(t, r, []string{"B"}, "a")
	c.say("more")
	c.end(chat.FinishStop, nil)
	r.p.next(t).reply("b")
	wait(t, r.a)
	if m := r.p.maxConcurrent(); m != 1 {
		t.Fatalf("request B started before A was drained (concurrency %d)", m)
	}
}

func TestAgent_FirstInterruptNeverCancelsRequest(t *testing.T) {
	r := startRig(t)
	c := interruptedAfter(t, r, nil, "a")
	if err := c.ctx.Err(); err != nil {
		t.Fatalf("ctx cancelled: %v", err)
	}
	c.end(chat.FinishStop, nil)
	wait(t, r.a)
}

func TestAgent_InterruptKeepsQueue(t *testing.T) {
	r := startRig(t)
	c := interruptedAfter(t, r, []string{"B"})
	c.end(chat.FinishStop, nil)
	r.p.next(t).reply("b1")
	wait(t, r.a)
	wantHistory(t, r.a, user("A"), user("B"), asst("b1"))
}

func TestAgent_UsageDuringDrainCounted(t *testing.T) {
	r := startRig(t)
	c := interruptedAfter(t, r, nil, "a")
	c.usage(500, 100)
	c.end(chat.FinishStop, nil)
	wait(t, r.a)
	if m := r.rec.lastMeter(t); m.Used != 600 || m.Estimated {
		t.Fatalf("meter = %+v", m)
	}
}

func TestAgent_InterruptEmptyPartialStoresNothing(t *testing.T) {
	r := startRig(t)
	c := interruptedAfter(t, r, nil)
	c.say("late")
	c.end(chat.FinishStop, nil)
	wait(t, r.a)
	wantHistory(t, r.a, user("A"))
	if end := r.rec.of(EvTurnEnded); !reflect.DeepEqual(end[0].Message, (chat.Message{})) {
		t.Fatalf("TurnEnded message = %+v", end[0].Message)
	}
}

func TestAgent_InterruptWhenIdleNoop(t *testing.T) {
	r := startRig(t)
	r.a.Interrupt()
	r.a.Submit("A")
	r.p.next(t).reply("ok")
	wait(t, r.a)
	wantHistory(t, r.a, user("A"), asst("ok"))
	if len(r.rec.of(EvDraining)) != 0 {
		t.Fatal("stale interrupt leaked into the next turn")
	}
}

func TestAgent_InterruptAfterDoneRace(t *testing.T) {
	r := startRig(t)
	for i := 0; i < 20; i++ {
		r.a.Submit("q")
		c := r.p.next(t)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); r.a.Interrupt() }()
		c.reply("a")
		wg.Wait()
		wait(t, r.a)
		h := r.a.History()
		for j, m := range h {
			if m.Role == chat.RoleAssistant && (j == 0 || h[j-1].Role != chat.RoleUser) {
				t.Fatalf("inconsistent history %+v", h)
			}
		}
		r.a.Interrupt() // idle: must not poison the next turn
	}
}

func TestAgent_OnEventMayCallAgent(t *testing.T) {
	var r *rig
	var inClear atomic.Bool // an idle Clear emits Meter, which would call Clear again without end
	r = startRig(t, func(d *Deps) {
		d.OnEvent = func(ev Event) {
			r.a.Busy()
			r.a.History()
			r.a.Interrupt()
			if inClear.CompareAndSwap(false, true) {
				_ = r.a.Clear()
				inClear.Store(false)
			}
		}
	})
	r.a.Submit("A")
	r.p.next(t).reply("ok")
	wait(t, r.a)
}

// ---------------------------------------------------------------------------
// Clear, Wait, shutdown
// ---------------------------------------------------------------------------

func TestAgent_ClearEmptiesHistoryResetsMeter(t *testing.T) {
	r := startRig(t)
	r.a.Submit("A")
	c := r.p.next(t)
	c.usage(500, 100)
	c.reply("ok")
	wait(t, r.a)
	if err := r.a.Clear(); err != nil {
		t.Fatal(err)
	}
	if len(r.a.History()) != 0 {
		t.Fatal("history not empty")
	}
	if m := r.rec.lastMeter(t); !m.Estimated || m.Used != chat.EstimateTokens("SYS") {
		t.Fatalf("meter = %+v", m)
	}
}

func TestAgent_ClearBusyReturnsErrBusy(t *testing.T) {
	r := startRig(t)
	r.a.Submit("A")
	c := r.p.next(t)
	if err := r.a.Clear(); !errors.Is(err, ErrBusy) {
		t.Fatalf("Clear = %v", err)
	}
	c.reply("ok")
	wait(t, r.a)
	wantHistory(t, r.a, user("A"), asst("ok"))
}

func TestAgent_ClearKeepsQueue(t *testing.T) {
	r := newRig(t) // not started: nothing runs, the queue just fills
	r.a.Submit("A")
	if err := r.a.Clear(); err != nil {
		t.Fatal(err)
	}
	if !r.a.Busy() {
		t.Fatal("queue lost by Clear")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.a.Start(ctx)
	c := r.p.next(t)
	c.reply("ok")
	wait(t, r.a)
	if got := c.req.Messages; got[len(got)-1].Content != "A" {
		t.Fatalf("request = %+v", got)
	}
}

func TestAgent_ClearRearmsWarnings(t *testing.T) {
	r := startRig(t)
	for i := 0; i < 2; i++ {
		r.a.Submit("A")
		c := r.p.next(t)
		c.usage(850, 0)
		c.reply("ok")
		wait(t, r.a)
		if err := r.a.Clear(); err != nil {
			t.Fatal(err)
		}
	}
	if w := r.rec.of(EvWarning); len(w) != 2 {
		t.Fatalf("warnings = %+v", w)
	}
}

func TestAgent_WaitReturnsWhenQueueEmptyAndIdle(t *testing.T) {
	r := startRig(t)
	wait(t, r.a) // idle: returns at once
	r.a.Submit("A")
	c := r.p.next(t)
	done := make(chan struct{})
	go func() { r.a.Wait(); close(done) }()
	select {
	case <-done:
		t.Fatal("Wait returned during a turn")
	default:
	}
	c.reply("ok")
	select {
	case <-done:
	case <-time.After(deadline):
		t.Fatal("Wait did not return")
	}
	if r.a.Busy() {
		t.Fatal("still busy")
	}
}

func TestAgent_ShutdownViaContext(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.a.Start(ctx)
	r.a.Submit("A")
	r.p.next(t)
	cancel()
	wait(t, r.a)
	r.a.Submit("B") // queued after shutdown: Wait must still not hang
	wait(t, r.a)
}

func TestAgent_HistoryReturnsCopy(t *testing.T) {
	r := startRig(t)
	r.a.Submit("A")
	r.p.next(t).reply("ok")
	wait(t, r.a)
	h := r.a.History()
	h[0].Content = "changed"
	wantHistory(t, r.a, user("A"), asst("ok"))
}

func TestAgent_OnEventNil(t *testing.T) {
	r := startRig(t, func(d *Deps) { d.OnEvent = nil })
	r.a.Submit("A")
	r.p.next(t).reply("ok")
	wait(t, r.a)
	if err := r.a.Clear(); err != nil {
		t.Fatal(err)
	}
}

// An answer that cost tokens but arrived empty (mlx_lm.server drops a tool call the model did not
// close with </tool_call>, T5 M1) asks the model again, once.
func TestAgent_EmptyAnswerWithTokensAsksTheModelAgain(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hi")
	c := r.p.next(t)
	c.usage(100, 25)
	c.end(chat.FinishStop, nil)
	c = r.p.next(t)
	if last := c.req.Messages[len(c.req.Messages)-1]; last.Role != chat.RoleUser || !strings.Contains(last.Content, droppedNudge) {
		t.Fatalf("last request message = %+v", last)
	}
	c.reply("done")
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 1 || w[0] != droppedWarning {
		t.Fatalf("warnings = %q", w)
	}
	if end := r.rec.of(EvTurnEnded); end[0].Message.Content != "done" {
		t.Fatalf("TurnEnded = %+v", end[0])
	}
}

// A truly empty answer (only the end token) ends the Turn as before.
func TestAgent_EmptyAnswerOfOneTokenIsNoNudge(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hi")
	c := r.p.next(t)
	c.usage(100, 1)
	c.end(chat.FinishStop, nil)
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 1 || w[0] != "empty answer" {
		t.Fatalf("warnings = %q", w)
	}
}

// A second dropped answer ends the Turn with the stop note only (no extra "empty answer").
func TestAgent_EmptyAnswerWithTokensTwiceEndsTheTurn(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hi")
	for i := 0; i < 2; i++ {
		c := r.p.next(t)
		c.usage(100, 25)
		c.end(chat.FinishStop, nil)
	}
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 2 || w[0] != droppedWarning || w[1] != droppedStop {
		t.Fatalf("warnings = %q", w)
	}
}
