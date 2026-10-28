package agent

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// EvToolOutput (live progress) and Agent.Background
// ---------------------------------------------------------------------------

// kindsOf returns "start:id", "out:id:text", "end:id" for the tool events, in order.
func kindsOf(evs []Event) (out []string) {
	for _, ev := range evs {
		switch ev.Kind {
		case EvToolStart:
			out = append(out, "start:"+ev.Call.ID)
		case EvToolOutput:
			out = append(out, "out:"+ev.Call.ID+":"+ev.Text)
		case EvToolEnd:
			out = append(out, "end:"+ev.Call.ID)
		}
	}
	return out
}

func TestToolOutput_EventBetweenStartAndEnd(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolMut, `{"x":1}`))
	ft.waitStarted(t, "c1")
	ft.progress("c1", "a")
	ft.progress("c1", "b")
	ft.release("c1")
	r.p.next(t).reply("done")
	wait(t, r.a)
	want := []string{"start:c1", "out:c1:a", "out:c1:b", "end:c1"}
	if got := kindsOf(r.rec.all()); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if got := r.rec.of(EvToolOutput)[0].Call.Args; got != `{"x":1}` {
		t.Fatalf("Call.Args = %q, want the raw args", got)
	}
}

func TestToolOutput_ParallelCallsKeepPerCallOrder(t *testing.T) {
	const perCall = 50
	ft := newFakeTools()
	ft.hold("c1", "c2", "c3")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolRead, ""), toolCall(2, toolRead, ""), toolCall(3, toolRead, ""))
	ids := []string{"c1", "c2", "c3"}
	var wg sync.WaitGroup
	for _, id := range ids {
		ft.waitStarted(t, id)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perCall; i++ {
				ft.progress(id, fmt.Sprint(i))
			}
		}()
	}
	wg.Wait()
	for _, id := range ids {
		ft.release(id)
	}
	r.p.next(t).reply("done")
	wait(t, r.a)
	for _, id := range ids {
		var got []string
		for _, ev := range r.rec.of(EvToolOutput) {
			if ev.Call.ID == id {
				got = append(got, ev.Text)
			}
		}
		for i, g := range got {
			if g != fmt.Sprint(i) {
				t.Fatalf("%s chunk %d = %q (all %v)", id, i, g, got)
			}
		}
		if len(got) != perCall {
			t.Fatalf("%s got %d chunks, want %d", id, len(got), perCall)
		}
	}
}

func TestToolOutput_NotAfterEnd(t *testing.T) {
	ft := newFakeTools()
	r := toolRig(t, ft)
	oneStep(t, r, toolCall(1, toolMut, ""))
	before := len(r.rec.all())
	ft.progress("c1", "late") // the call is over: must be dropped
	if got := len(r.rec.all()); got != before {
		t.Fatalf("a late Progress emitted %v", kindsOf(r.rec.all()[before:]))
	}
	for _, ev := range r.rec.all() {
		if ev.Kind == EvToolOutput {
			t.Fatalf("unexpected %+v", ev)
		}
	}
}

func TestToolOutput_NotUnderLock(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	var a *Agent
	var mu sync.Mutex
	rec := newRecorder()
	r := toolRig(t, ft, func(d *Deps) {
		d.OnEvent = func(ev Event) {
			if ev.Kind == EvToolOutput {
				mu.Lock()
				ag := a
				mu.Unlock()
				ag.Busy()
				ag.History()
				ag.Background() // c1 is read-only: false, and must not deadlock
			}
			rec.add(ev)
		}
	})
	mu.Lock()
	a = r.a
	mu.Unlock()
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolRead, ""))
	ft.waitStarted(t, "c1")
	done := make(chan struct{})
	go func() { ft.progress("c1", "x"); close(done) }()
	select {
	case <-done:
	case <-time.After(deadline):
		t.Fatal("Progress deadlocked: OnEvent ran under a lock")
	}
	ft.release("c1")
	r.p.next(t).reply("done")
	wait(t, r.a)
	if n := len(rec.of(EvToolOutput)); n != 1 {
		t.Fatalf("ToolOutput events = %d", n)
	}
}

// runWithProgress plays one tool step (optionally with progress) and returns the history and both requests.
func runWithProgress(t *testing.T, progress bool) ([]chat.Message, chat.Request) {
	t.Helper()
	ft := newFakeTools()
	ft.hold("c1")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolMut, ""))
	ft.waitStarted(t, "c1")
	if progress {
		ft.progress("c1", "chunk one")
		ft.progress("c1", "chunk two")
	}
	ft.release("c1")
	c2 := r.p.next(t)
	c2.reply("done")
	wait(t, r.a)
	return r.a.History(), c2.req
}

func TestToolOutput_HistoryUnchanged(t *testing.T) {
	h1, req1 := runWithProgress(t, false)
	h2, req2 := runWithProgress(t, true)
	if !reflect.DeepEqual(h1, h2) {
		t.Fatalf("history differs:\n%+v\n%+v", h1, h2)
	}
	if !reflect.DeepEqual(req1.Messages, req2.Messages) {
		t.Fatalf("request differs:\n%+v\n%+v", req1.Messages, req2.Messages)
	}
	assertHistoryValid(t, h2)
	for _, m := range h2 {
		if strings.Contains(m.Content, "chunk") {
			t.Fatalf("progress leaked into history: %+v", m)
		}
	}
}

func TestBackground_DetachesRunningDetachableCall(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolMut, ""))
	ft.waitStarted(t, "c1")
	if !r.a.Background() {
		t.Fatal("Background() = false for a running detachable call")
	}
	c2 := r.p.next(t) // the Turn continues without release
	c2.reply("done")
	wait(t, r.a)
	if got := resultsOf(c2.req); strings.Join(got, ",") != "moved to background:c1" {
		t.Fatalf("results = %v", got)
	}
	assertHistoryValid(t, r.a.History())
}

func TestBackground_FalseWhenNothingRuns(t *testing.T) {
	ft := newFakeTools()
	r := toolRig(t, ft)
	if r.a.Background() {
		t.Fatal("true while idle")
	}
	r.a.Submit("go")
	c1 := r.p.next(t) // streaming, between tool steps
	if r.a.Background() {
		t.Fatal("true while the model streams")
	}
	c1.reply("done")
	wait(t, r.a)
	if r.a.Background() {
		t.Fatal("true after the turn")
	}
}

func TestBackground_FalseForReadOnlyOrNonDetachable(t *testing.T) {
	for _, name := range []string{toolRead, toolEdit} {
		ft := newFakeTools()
		ft.hold("c1")
		r := toolRig(t, ft)
		r.a.Submit("go")
		r.p.next(t).toolStep("", toolCall(1, name, ""))
		ft.waitStarted(t, "c1")
		if r.a.Background() {
			t.Fatalf("%s: Background() = true", name)
		}
		if ft.hooks["c1"].Detach != nil {
			t.Fatalf("%s: Detach channel given to a non-detachable tool", name)
		}
		ft.release("c1")
		r.p.next(t).reply("done")
		wait(t, r.a)
	}
}

func TestBackground_OnlyTheRunningOne(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1", "c2")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolMut, ""), toolCall(2, toolMut, ""))
	ft.waitStarted(t, "c1")
	if ft.detached("c1") {
		t.Fatal("c1 detached early")
	}
	if !r.a.Background() {
		t.Fatal("Background() = false")
	}
	ft.waitStarted(t, "c2") // c1 returned, c2 runs with its own channel
	if ft.detached("c2") {
		t.Fatal("c2 got c1's closed channel")
	}
	if !r.a.Background() {
		t.Fatal("second Background() = false for c2")
	}
	c2 := r.p.next(t)
	c2.reply("done")
	wait(t, r.a)
	if got := resultsOf(c2.req); strings.Join(got, ",") != "moved to background:c1,moved to background:c2" {
		t.Fatalf("results = %v", got)
	}
}

func TestBackground_TwiceNoPanic(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolMut, ""))
	ft.waitStarted(t, "c1")
	first := r.a.Background()
	second := r.a.Background()
	if !first || second {
		t.Fatalf("Background twice = %v, %v; want true, false", first, second)
	}
	r.p.next(t).reply("done")
	wait(t, r.a)
}

func TestBackground_FalseWhileAwaitingApproval(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	asking, answer := make(chan struct{}), make(chan struct{})
	r := toolRig(t, ft, func(d *Deps) {
		d.Approve = func(ctx context.Context, _ chat.ToolCall) Approval {
			close(asking)
			select {
			case <-answer:
			case <-ctx.Done():
			}
			return Approval{Allow: true}
		}
	})
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolAsk, ""))
	select {
	case <-asking:
	case <-time.After(deadline):
		t.Fatal("Approve not called")
	}
	if r.a.Background() {
		t.Fatal("Background() = true while the approval is pending")
	}
	close(answer)
	ft.waitStarted(t, "c1")
	if ft.detached("c1") {
		t.Fatal("the early Background() closed the channel")
	}
	if !r.a.Background() {
		t.Fatal("Background() = false after approval")
	}
	r.p.next(t).reply("done")
	wait(t, r.a)
}

func TestBackground_RaceWithCallEnd(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolMut, ""))
	ft.waitStarted(t, "c1")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				r.a.Background()
			}
		}()
	}
	ft.release("c1")
	wg.Wait()
	r.p.next(t).reply("done")
	wait(t, r.a)
	assertHistoryValid(t, r.a.History())
	if r.a.Background() {
		t.Fatal("true after the turn")
	}
}

func TestBackground_DetachedCallStillSerialized(t *testing.T) {
	ft := newFakeTools()
	ft.hold("c1", "c2")
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolMut, ""), toolCall(2, toolMut, ""))
	ft.waitStarted(t, "c1")
	r.a.Background()
	ft.waitStarted(t, "c2")
	got := ft.events()
	if i1, i2 := indexOf(got, "d:c1"), indexOf(got, "s:c2"); i1 < 0 || i2 < i1 {
		t.Fatalf("c2 started before c1 returned: %v", got)
	}
	ft.release("c2")
	r.p.next(t).reply("done")
	wait(t, r.a)
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
