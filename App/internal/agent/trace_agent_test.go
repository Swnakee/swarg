package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// What the agent records in the Trace
// ---------------------------------------------------------------------------

// addedOf returns the roles+ids of the messages a request record carries.
func addedOf(rec traceRec) (out []string) {
	l, _ := rec.D["added"].([]any)
	for _, x := range l {
		m := x.(map[string]any)
		s, _ := m["role"].(string)
		if id, ok := m["tool_call_id"].(string); ok {
			s += ":" + id
		}
		out = append(out, s)
	}
	return out
}

func TestTrace_RequestHoldsOnlyTheMessagesAppendedSinceThePreviousRequest(t *testing.T) {
	et := newEconTools()
	tr := &fakeTrace{}
	r := econRig(t, et, withStore(nil, tr), func(d *Deps) { d.Approve = nil })
	script(t, r, "go", [][]chat.ToolCall{{readCall(1, "a.go"), readCall(2, "b.go")}}, "done")
	nextTurn(t, r, "again")

	reqs := tr.of("request")
	if len(reqs) != 3 {
		t.Fatalf("%d request records", len(reqs))
	}
	want := [][]string{{"user"}, {"assistant", "tool:c1", "tool:c2"}, {"assistant", "user"}}
	for i, w := range want {
		if got := addedOf(reqs[i]); !reflect.DeepEqual(got, w) {
			t.Errorf("request %d added %v, want %v", i+1, got, w)
		}
	}
	if reqs[1].D["step"] != float64(2) || reqs[2].D["step"] != float64(1) {
		t.Errorf("steps %v %v", reqs[1].D["step"], reqs[2].D["step"])
	}
	if reqs[1].D["msgs"] != float64(5) || reqs[1].D["model"] != "m" {
		t.Errorf("msgs/model %v", reqs[1].D)
	}
}

func TestTrace_RequestCarriesTheStubsAsSent(t *testing.T) {
	et := newEconTools()
	et.out["c1"] = big(300)
	tr := &fakeTrace{}
	r := econRig(t, et, withStore(nil, tr), func(d *Deps) { d.Approve = nil })
	script(t, r, "go", [][]chat.ToolCall{{shellCall(1)}}, "done")
	// A stubbed result is never "added" again; only the next user message is.
	nextTurn(t, r, "again")
	reqs := tr.of("request")
	if got := addedOf(reqs[2]); !reflect.DeepEqual(got, []string{"assistant", "user"}) {
		t.Errorf("added %v", got)
	}
	b := tr.of("stub_batch")
	if len(b) != 1 || b[0].D["why"] != "turn_end" || !reflect.DeepEqual(b[0].D["stubbed"], []any{float64(3)}) {
		t.Errorf("stub_batch %+v", b)
	}
	// The record sits before the next request, after the last reply of the Turn.
	k := tr.kinds()
	if idx(k, "stub_batch") < idx(k, "delta") || idx(k, "stub_batch") > lastIdx(k, "request") {
		t.Errorf("order %v", k)
	}
}

func idx(k []string, s string) int {
	for i, x := range k {
		if x == s {
			return i
		}
	}
	return -1
}

func lastIdx(k []string, s string) int {
	for i := len(k) - 1; i >= 0; i-- {
		if k[i] == s {
			return i
		}
	}
	return -1
}

func TestTrace_ReplyHasFullTextAndCalls(t *testing.T) {
	et := newEconTools()
	tr := &fakeTrace{}
	r := econRig(t, et, withStore(nil, tr), func(d *Deps) { d.Approve = nil })
	r.a.Submit("go")
	r.p.next(t).toolStep("thinking ", readCall(1, "a.go"))
	r.p.next(t).reply("one ", "two")
	wait(t, r.a)
	ds := tr.of("delta")
	if len(ds) != 2 { // per reply, not per token
		t.Fatalf("%d delta records", len(ds))
	}
	calls, _ := ds[0].D["calls"].([]any)
	if ds[0].D["text"] != "thinking " || len(calls) != 1 || calls[0].(map[string]any)["args"] != `{"path":"a.go"}` {
		t.Errorf("first reply %+v", ds[0].D)
	}
	if ds[1].D["text"] != "one two" || ds[1].D["finish"] != "stop" {
		t.Errorf("second reply %+v", ds[1].D)
	}
}

func TestTrace_UsageRecord(t *testing.T) {
	tr := &fakeTrace{}
	r := startRig(t, withStore(nil, tr))
	r.a.Submit("q")
	c := r.p.next(t)
	c.ch <- chat.StreamEvent{Kind: chat.EventUsage, Usage: chat.Usage{PromptTokens: 1000, CompletionTokens: 7, CachedTokens: 800}}
	c.reply("a")
	wait(t, r.a)
	u := tr.of("usage")
	if len(u) != 1 || u[0].D["prompt"] != float64(1000) || u[0].D["cached"] != float64(800) || u[0].D["completion"] != float64(7) {
		t.Errorf("usage %+v", u)
	}
}

func TestTrace_ToolStartAndEnd(t *testing.T) {
	et := newEconTools()
	capped := strings.Repeat("l\n", 50) + "[... 1234 bytes cut; use read_file offset/limit or grep for the rest]\n" + strings.Repeat("t\n", 20)
	et.out["c1"] = capped
	et.fail["c2"] = true
	tr := &fakeTrace{}
	r := econRig(t, et, withStore(nil, tr), func(d *Deps) { d.Approve = nil })
	script(t, r, "go", [][]chat.ToolCall{{readCall(1, "a.go")}, {readCall(2, "b.go")}}, "done")

	st := tr.of("tool_start")
	en := tr.of("tool_end")
	if len(st) != 2 || len(en) != 2 {
		t.Fatalf("%d starts, %d ends", len(st), len(en))
	}
	if st[0].D["name"] != "read_file" || st[0].D["args"] != `{"path":"a.go"}` || st[0].D["id"] != "c1" {
		t.Errorf("tool_start %+v", st[0].D)
	}
	if en[0].D["output"] != capped || en[0].D["capped"] != true || en[0].D["brief"] != "brief:c1" {
		t.Errorf("tool_end %+v", en[0].D)
	}
	if _, has := en[0].D["error"]; has {
		t.Errorf("a successful call has an error field: %+v", en[0].D)
	}
	if en[1].D["error"] != "boom c2" || en[1].D["capped"] != false || en[1].D["output"] != "error: boom c2" {
		t.Errorf("failed tool_end %+v", en[1].D)
	}
	if _, ok := en[0].D["ms"].(float64); !ok {
		t.Errorf("no ms in %+v", en[0].D)
	}
	// Start comes before its End, which comes before the next request.
	k := tr.kinds()
	if !(idx(k, "tool_start") < idx(k, "tool_end") && idx(k, "tool_end") < lastIdx(k, "request")) {
		t.Errorf("order %v", k)
	}
}

func TestTrace_ApprovalRecorded(t *testing.T) {
	et := newEconTools()
	tr := &fakeTrace{}
	r := econRig(t, et, withStore(nil, tr), func(d *Deps) {
		d.Approve = func(_ context.Context, c chat.ToolCall) Approval { return Approval{Allow: false, Note: "no way"} }
	})
	r.a.Submit("go")
	r.p.next(t).toolStep("", shellCall(1))
	r.p.next(t).reply("ok")
	wait(t, r.a)
	ap := tr.of("approval")
	if len(ap) != 1 || ap[0].D["allow"] != false || ap[0].D["note"] != "no way" || ap[0].D["name"] != "shell" {
		t.Errorf("approval %+v", ap)
	}
}

func TestTrace_InterruptAndAbort(t *testing.T) {
	tr := &fakeTrace{}
	r := startRig(t, withStore(nil, tr))
	r.a.Interrupt() // idle: nothing happened, nothing recorded
	r.a.Submit("q")
	c := r.p.next(t)
	c.say("x")
	r.rec.until(t, EvDelta, 1)
	r.a.Interrupt()
	r.a.Interrupt() // the second press is an Abort
	wait(t, r.a)
	_ = c
	if len(tr.of("interrupt")) != 1 || len(tr.of("abort")) != 1 {
		t.Errorf("trace %v", tr.kinds())
	}
}

func TestTrace_LoopGuardWarningAndError(t *testing.T) {
	tr := &fakeTrace{}
	r := startRig(t, withStore(nil, tr))
	r.a.Submit("loop")
	c := r.p.next(t)
	sayLoop(c, 1)
	wait(t, r.a)
	lg := tr.of("loop_guard")
	if len(lg) != 1 {
		t.Fatalf("trace %v", tr.kinds())
	}
	if w := tr.of("warning"); len(w) != 1 || w[0].D["text"] != loopWarning {
		t.Errorf("warning %+v", w)
	}

	r.p.failNext = []error{errors.New("server on fire")}
	r.a.Submit("again")
	wait(t, r.a)
	e := tr.of("error")
	if len(e) != 1 || !strings.Contains(e[0].D["text"].(string), "server on fire") {
		t.Errorf("error %+v", e)
	}
}

func TestTrace_LoadAndClearRecorded(t *testing.T) {
	tr := &fakeTrace{}
	r := startRig(t, withStore(nil, tr))
	if err := r.a.Load(savedHistory()); err != nil {
		t.Fatal(err)
	}
	wait(t, r.a)
	if err := r.a.Clear(); err != nil {
		t.Fatal(err)
	}
	l := tr.of("load")
	if len(l) != 1 || l[0].D["messages"] != float64(4) || l[0].D["repaired"] != float64(0) || len(tr.of("clear")) != 1 {
		t.Errorf("trace %v %+v", tr.kinds(), l)
	}
	// The first request after a Load carries the whole loaded history as added.
	r.a.Submit("next")
	r.p.next(t).reply("ok")
	wait(t, r.a)
	if got := addedOf(tr.of("request")[0]); len(got) != 1 {
		t.Errorf("after Clear the request added %v", got)
	}
}

func TestTrace_ErrorOfTheWriterIsOneWarning(t *testing.T) {
	tr := &fakeTrace{err: errors.New("no space")}
	r := startRig(t, withStore(nil, tr))
	for i := 0; i < 2; i++ {
		r.a.Submit("q")
		r.p.next(t).reply("a")
		wait(t, r.a)
	}
	ws := r.rec.texts(EvWarning)
	n := 0
	for _, w := range ws {
		if strings.Contains(w, "no space") {
			n++
		}
	}
	if n != 1 || len(r.rec.of(EvTurnEnded)) != 2 || len(r.a.History()) != 4 {
		t.Errorf("warnings %q, history %d", ws, len(r.a.History()))
	}
}

func TestTrace_DoesNotChangeTheRequests(t *testing.T) {
	run := func(withTrace bool) (reqs []chat.Request) {
		et := newEconTools()
		et.out["c1"] = big(300)
		var mod []func(*Deps)
		mod = append(mod, func(d *Deps) { d.Approve = nil })
		if withTrace {
			mod = append(mod, withStore(&fakeStore{}, &fakeTrace{}))
		}
		r := econRig(t, et, mod...)
		reqs = script(t, r, "go", [][]chat.ToolCall{{shellCall(1)}, {readCall(2, "a.go")}}, "done")
		return append(reqs, nextTurn(t, r, "more"))
	}
	if a, b := run(false), run(true); !reflect.DeepEqual(a, b) {
		t.Errorf("requests differ:\n%+v\n%+v", a, b)
	}
}

func TestTrace_PayloadsAreJSON(t *testing.T) {
	// fakeTrace.Record panics on a payload that cannot be marshalled; run every kind once.
	et := newEconTools()
	et.out["c1"] = big(400)
	tr := &fakeTrace{}
	r := econRig(t, et, withStore(&fakeStore{}, tr))
	script(t, r, "go", [][]chat.ToolCall{{shellCall(1)}}, fmt.Sprint("done"))
	if len(tr.kinds()) < 7 {
		t.Errorf("trace %v", tr.kinds())
	}
}
