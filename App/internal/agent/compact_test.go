package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"swarg/internal/chat"
	"swarg/internal/session"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// noteText is n bytes of text that never repeats (the loop guard must not fire on it).
func noteText(seed, n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "n%dw%d ", seed, i)
	}
	return b.String()
}

// workTurn runs a Turn of `steps` Steps; each says a note and reads one file. Calls are
// numbered from first. Use it with auto-compaction off.
func workTurn(t *testing.T, r *rig, user string, first, steps, noteBytes int) {
	t.Helper()
	r.a.Submit(user)
	for i := 0; i < steps; i++ {
		r.p.next(t).toolStep(noteText(first+i, noteBytes), readCall(first+i, fmt.Sprintf("f%d.go", first+i)))
	}
	r.p.next(t).reply("answer to " + user)
	wait(t, r.a)
}

// fatTurn runs a Turn of n Steps, each an edit_file call with a 400-byte argument and no note:
// a span under 600 tokens whose generated list is much shorter.
func fatTurn(t *testing.T, r *rig, user string, first, n int) {
	t.Helper()
	r.a.Submit(user)
	for i := 0; i < n; i++ {
		id := first + i
		r.p.next(t).toolStep("", toolCall(id, "edit_file", fmt.Sprintf(`{"path":"f%d.go","old":%q,"new":"b"}`, id, big(400))))
	}
	r.p.next(t).reply("answer to " + user)
	wait(t, r.a)
}

func nextSummary(t *testing.T, r *rig) *call {
	t.Helper()
	c := r.p.next(t)
	if !isSummaryRequest(c.req) {
		t.Fatalf("expected a summary request, got %+v", c.req)
	}
	return c
}

func mustCompact(t *testing.T, a *Agent) {
	t.Helper()
	if err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func summaries(h []chat.Message) (out []chat.Message) {
	for _, m := range h {
		if m.Summary {
			out = append(out, m)
		}
	}
	return out
}

// noCall fails if a Stream call is waiting (call it after wait).
func noCall(t *testing.T, r *rig) {
	t.Helper()
	select {
	case c := <-r.p.calls:
		t.Fatalf("unexpected request %+v", c.req)
	default:
	}
}

func compactRig(t *testing.T, mod ...func(*Deps)) *rig {
	t.Helper()
	return econRig(t, newEconTools(), append([]func(*Deps){func(d *Deps) { d.Approve = nil }}, mod...)...)
}

// ---------------------------------------------------------------------------
// Span detection
// ---------------------------------------------------------------------------

func TestSpans_Detection(t *testing.T) {
	u := func(seq int, inj bool) chat.Message {
		return chat.Message{Role: chat.RoleUser, Seq: seq, TurnStart: !inj, Injected: inj}
	}
	a := func(seq int, id string) chat.Message {
		return chat.Message{Role: chat.RoleAssistant, Seq: seq, ToolCalls: []chat.ToolCall{{ID: id, Name: "x", Args: "{}"}}}
	}
	r := func(seq int, id string) chat.Message {
		return chat.Message{Role: chat.RoleTool, Seq: seq, ToolCallID: id, Name: "x"}
	}
	fin := func(seq int) chat.Message { return chat.Message{Role: chat.RoleAssistant, Seq: seq, Content: "done"} }
	sum := func(seq int) chat.Message {
		return chat.Message{Role: chat.RoleAssistant, Seq: seq, Content: summaryPrefix + "x", Summary: true}
	}
	cases := []struct {
		name string
		h    []chat.Message
		want [][2]int // fromSeq, toSeq
	}{
		{"one Turn", []chat.Message{u(1, false), a(2, "c1"), r(3, "c1"), fin(4)}, [][2]int{{2, 3}}},
		{"two Turns", []chat.Message{u(1, false), a(2, "c1"), r(3, "c1"), fin(4), u(5, false), a(6, "c2"), r(7, "c2"), fin(8)}, [][2]int{{2, 3}, {6, 7}}},
		{"no tools", []chat.Message{u(1, false), fin(2)}, nil},
		{"injected user splits", []chat.Message{u(1, false), a(2, "c1"), r(3, "c1"), u(4, true), a(5, "c2"), r(6, "c2"), fin(7)}, [][2]int{{2, 3}, {5, 6}}},
		{"summary is never part of a span", []chat.Message{u(1, false), sum(2), a(3, "c1"), r(4, "c1"), fin(5)}, [][2]int{{3, 4}}},
		{"turn ended by denials has no final answer", []chat.Message{u(1, false), a(2, "c1"), r(3, "c1"), a(4, "c2"), r(5, "c2")}, [][2]int{{2, 5}}},
		{"interrupted text is a final answer", []chat.Message{u(1, false), a(2, "c1"), r(3, "c1"), {Role: chat.RoleAssistant, Seq: 4, Content: "part", Interrupted: true}}, [][2]int{{2, 3}}},
		{"truncated answer is kept", []chat.Message{u(1, false), a(2, "c1"), r(3, "c1"), {Role: chat.RoleAssistant, Seq: 4, Content: "cut", Truncated: true}}, [][2]int{{2, 3}}},
		{"repaired dangling call is left alone", []chat.Message{u(1, false), a(2, "c1"), u(3, false), fin(4), r(5, "c1")}, nil},
		{"result out of order is left alone", []chat.Message{u(1, false), {Role: chat.RoleAssistant, Seq: 2, ToolCalls: []chat.ToolCall{{ID: "a"}, {ID: "b"}}}, r(3, "b"), r(4, "a"), fin(5)}, nil},
		{"messages without Seq are left alone", []chat.Message{u(1, false), a(0, "c1"), r(0, "c1"), fin(2)}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got [][2]int
			for _, s := range findSpans(c.h) {
				got = append(got, [2]int{s.fromSeq, s.toSeq})
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("spans %v, want %v", got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Replacing spans
// ---------------------------------------------------------------------------

func TestCompact_ReplacesSpanWithSummaryMessage(t *testing.T) {
	var lane *laneProvider
	st := &fakeStore{}
	r := compactRig(t, withLane(&lane), withStore(st, nil))
	workTurn(t, r, "first", 1, 2, 3000)
	workTurn(t, r, "second", 10, 2, 3000)
	before := r.a.History()

	mustCompact(t, r.a)
	nextSummary(t, r).reply("read f1, f2")
	nextSummary(t, r).reply("read f10, f11")
	wait(t, r.a)

	h := r.a.History()
	assertHistoryValid(t, h)
	var roles []string
	for _, m := range h {
		roles = append(roles, string(m.Role))
	}
	if got := strings.Join(roles, ","); got != "user,assistant,assistant,user,assistant,assistant" {
		t.Fatalf("roles %s", got)
	}
	if s := summaries(h); len(s) != 2 || s[0].Content != "[work summary] read f1, f2" || s[1].Content != "[work summary] read f10, f11" {
		t.Fatalf("summaries %+v", s)
	}
	for _, i := range []int{0, 3} { // user Messages
		if !reflect.DeepEqual(h[i], before[indexOfSeq(before, h[i].Seq)]) {
			t.Errorf("user Message changed: %+v", h[i])
		}
	}
	if h[2].Content != "answer to first" || h[5].Content != "answer to second" || h[2].Summary {
		t.Errorf("final answers changed: %+v %+v", h[2], h[5])
	}
	if h[1].Seq <= before[len(before)-1].Seq {
		t.Errorf("summary Seq %d is not new", h[1].Seq)
	}
	if n := st.count("compact"); n != 2 {
		t.Fatalf("Store.Compact called %d times", n)
	}
	var ops []storeOp
	for _, op := range st.all() {
		if op.kind == "compact" {
			ops = append(ops, op)
		}
	}
	if ops[0].from != before[1].Seq || ops[0].to != before[4].Seq || ops[0].msg.Seq != h[1].Seq {
		t.Errorf("first compact record %+v", ops[0])
	}
	if lane.maxInFlight() != 1 {
		t.Errorf("max requests in flight %d", lane.maxInFlight())
	}
}

func TestCompact_SummarizerRequestShape(t *testing.T) {
	et := newEconTools()
	res := strings.Repeat("h", 500) + strings.Repeat("m", 700) + strings.Repeat("z", 500)
	et.out["c1"] = res
	et.out["c2"] = "short"
	extra := map[string]any{"temperature": 0.5}
	r := econRig(t, et, func(d *Deps) { d.Approve = nil; d.Extra = extra; d.ContextLimit = 20000 })
	user := strings.Repeat("q", 1500)
	r.a.Submit(user)
	r.p.next(t).toolStep(noteText(1, 3000), readCall(1, "a.go"))
	r.p.next(t).toolStep("", readCall(2, "b.go"))
	r.p.next(t).reply("ok")
	wait(t, r.a)

	mustCompact(t, r.a)
	c := nextSummary(t, r)
	req := c.req
	if len(req.Tools) != 0 || req.MaxTokens != 300 || req.Model != "m" || !reflect.DeepEqual(req.Extra, extra) || len(req.Messages) != 2 {
		t.Fatalf("request %+v", req)
	}
	in := req.Messages[1].Content
	for _, want := range []string{"Task: " + strings.Repeat("q", 1000) + "\n", "- read_file L:read_file: brief:c1\n" + strings.Repeat("h", 500),
		strings.Repeat("z", 500), "[... 700 bytes cut ...]", "- read_file L:read_file: brief:c2\nshort", "- note: n1w0 "} {
		if !strings.Contains(in, want) {
			t.Errorf("input lacks %.60q\n%s", want, strings.ReplaceAll(in, strings.Repeat("q", 100), "Q"))
		}
	}
	if strings.Contains(in, strings.Repeat("q", 1001)) || strings.Contains(in, "m") && strings.Contains(in, "mm") {
		t.Errorf("input is not cut: user or result middle present")
	}
	c.reply("ok")
	wait(t, r.a)
}

func TestCompact_Fallbacks(t *testing.T) {
	long := strings.Repeat("y", 20000)
	cases := []struct {
		name string
		play func(c *call)
		warn bool
	}{
		{"empty summary", func(c *call) { c.reply() }, false},
		{"summary longer than the span", func(c *call) { c.reply(long) }, false},
		{"summary without gain", func(c *call) { c.reply(strings.Repeat("y", 9000)) }, false},
		{"stream error", func(c *call) { c.say("part"); c.end(chat.FinishError, errors.New("dropped")) }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := compactRig(t)
			workTurn(t, r, "go", 1, 3, 3000)
			mustCompact(t, r.a)
			tc.play(nextSummary(t, r))
			wait(t, r.a)
			h := r.a.History()
			assertHistoryValid(t, h)
			s := summaries(h)
			want := "[work summary] read_file L:read_file: brief:c1; read_file L:read_file: brief:c2; read_file L:read_file: brief:c3"
			if len(s) != 1 || s[0].Content != want {
				t.Fatalf("summaries %+v, want %q", s, want)
			}
			noCall(t, r) // no retry
			warned := false
			for _, w := range r.rec.texts(EvWarning) {
				warned = warned || strings.Contains(w, "summary request failed")
			}
			if warned != tc.warn {
				t.Errorf("warning shown: %v, want %v (%v)", warned, tc.warn, r.rec.texts(EvWarning))
			}
		})
	}
}

func TestCompact_ProviderStartError(t *testing.T) {
	r := compactRig(t)
	workTurn(t, r, "go", 1, 3, 3000)
	r.p.mu.Lock()
	r.p.failNext = []error{errors.New("HTTP 500")}
	r.p.mu.Unlock()
	mustCompact(t, r.a)
	wait(t, r.a)
	if s := summaries(r.a.History()); len(s) != 1 || !strings.HasPrefix(s[0].Content, "[work summary] read_file") {
		t.Fatalf("summaries %+v", s)
	}
	n := 0
	for _, w := range r.rec.texts(EvWarning) {
		if strings.Contains(w, "HTTP 500") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d failure warnings", n)
	}
}

func TestCompact_SmallSpanNeedsNoModelCall(t *testing.T) {
	r := compactRig(t)
	fatTurn(t, r, "go", 1, 3)
	mustCompact(t, r.a)
	wait(t, r.a)
	noCall(t, r)
	s := summaries(r.a.History())
	want := "[work summary] edit_file L:edit_file: brief:c1; edit_file L:edit_file: brief:c2; edit_file L:edit_file: brief:c3"
	if len(s) != 1 || s[0].Content != want {
		t.Fatalf("summaries %+v (warnings %v)", s, r.rec.texts(EvWarning))
	}
}

func TestCompact_NoGainLeavesTheSpan(t *testing.T) {
	r := compactRig(t)
	workTurn(t, r, "go", 1, 1, 0) // one call: the list is as long as the span
	before := r.a.History()
	st := r.rec
	mustCompact(t, r.a)
	wait(t, r.a)
	if !reflect.DeepEqual(r.a.History(), before) {
		t.Fatal("history changed without gain")
	}
	if ev := st.of(EvCompacted); len(ev) != 1 || ev[0].Spans != 0 {
		t.Errorf("Compacted events %+v", ev)
	}
}

func TestCompact_ChunksOversizedSpan(t *testing.T) {
	et := newEconTools()
	for i := 1; i <= 6; i++ {
		et.out[fmt.Sprintf("c%d", i)] = strings.Repeat(string(rune('a'+i)), 1200)
	}
	r := econRig(t, et, func(d *Deps) { d.Approve = nil; d.ContextLimit = 2000 })
	r.a.Submit("go")
	for i := 1; i <= 6; i++ {
		r.p.next(t).toolStep(noteText(i, 500), readCall(i, fmt.Sprintf("f%d.go", i)))
	}
	r.p.next(t).reply("done")
	wait(t, r.a)

	mustCompact(t, r.a)
	var inputs []string
	for n := 1; len(r.rec.of(EvCompacted)) == 0; n++ {
		c := nextSummary(t, r)
		inputs = append(inputs, c.req.Messages[1].Content)
		c.reply(fmt.Sprintf("S%d", n))
		end := time.Now().Add(deadline)
		for len(r.p.calls) == 0 && len(r.rec.of(EvCompacted)) == 0 && time.Now().Before(end) {
			runtime.Gosched()
		}
	}
	wait(t, r.a)
	if len(inputs) < 2 {
		t.Fatalf("%d summary requests, want chunks", len(inputs))
	}
	if !strings.Contains(inputs[1], "Work log so far:\nS1") {
		t.Errorf("chunk 2 input lacks the first summary: %.200s", inputs[1])
	}
	s := summaries(r.a.History())
	if len(s) != 1 || s[0].Content != fmt.Sprintf("[work summary] S%d", len(inputs)) {
		t.Errorf("summaries %+v after %d requests", s, len(inputs))
	}
}

// ---------------------------------------------------------------------------
// Control: events, nothing to do, single lane, Esc
// ---------------------------------------------------------------------------

func TestCompact_NothingToDoAndIdempotent(t *testing.T) {
	r := compactRig(t)
	nextTurn(t, r, "hi")
	mustCompact(t, r.a)
	wait(t, r.a)
	noCall(t, r)
	if w := r.rec.texts(EvWarning); len(w) != 1 || w[0] != "nothing to compact" || len(r.rec.of(EvCompactStarted)) != 0 {
		t.Fatalf("warnings %v", w)
	}
	workTurn(t, r, "go", 1, 3, 3000)
	mustCompact(t, r.a)
	nextSummary(t, r).reply("ok")
	wait(t, r.a)
	mustCompact(t, r.a)
	wait(t, r.a)
	noCall(t, r)
	if n := len(summaries(r.a.History())); n != 1 {
		t.Errorf("%d summaries", n)
	}
}

func TestCompact_EventsAndNumbers(t *testing.T) {
	r := compactRig(t)
	workTurn(t, r, "go", 1, 3, 3000)
	mustCompact(t, r.a)
	nextSummary(t, r).reply("ok")
	wait(t, r.a)
	st, done := r.rec.of(EvCompactStarted), r.rec.of(EvCompacted)
	if len(st) != 1 || len(done) != 1 || st[0].Spans != 1 || done[0].Spans != 1 || done[0].After >= done[0].Before || st[0].Before != done[0].Before {
		t.Fatalf("started %+v compacted %+v", st, done)
	}
	var kinds []EventKind
	for _, e := range r.rec.all() {
		if e.Kind == EvCompactStarted || e.Kind == EvMeter || e.Kind == EvCompacted {
			kinds = append(kinds, e.Kind)
		}
	}
	n := len(kinds)
	if kinds[n-2] != EvMeter || kinds[n-1] != EvCompacted {
		t.Errorf("event order %v", kinds)
	}
	if m := r.rec.lastMeter(t); !m.Estimated {
		t.Errorf("meter after compaction %+v is not an estimate", m)
	}
}

func TestCompact_NeverParallelToTurnRequest(t *testing.T) {
	var lane *laneProvider
	r := compactRig(t, withLane(&lane))
	workTurn(t, r, "go", 1, 3, 3000)
	mustCompact(t, r.a)
	c := nextSummary(t, r)
	r.a.Submit("next")
	if !r.a.Busy() {
		t.Error("not busy during a Compaction")
	}
	c.reply("ok")
	r.p.next(t).reply("hello")
	wait(t, r.a)
	if lane.maxInFlight() != 1 {
		t.Errorf("max in flight %d (%s)", lane.maxInFlight(), lane.order())
	}
	if got := lane.order(); !strings.Contains(got, "summary:start summary:end turn:start") {
		t.Errorf("order %s", got)
	}
}

func TestCompact_ClearRefusedWhileCompacting(t *testing.T) {
	r := compactRig(t)
	workTurn(t, r, "go", 1, 3, 3000)
	mustCompact(t, r.a)
	c := nextSummary(t, r)
	if err := r.a.Clear(); !errors.Is(err, ErrBusy) {
		t.Errorf("Clear during Compaction: %v", err)
	}
	c.reply("ok")
	wait(t, r.a)
}

func TestCompact_FirstEscDrainsThenKeepsTheHistory(t *testing.T) {
	st := &fakeStore{}
	r := compactRig(t, withStore(st, nil))
	workTurn(t, r, "go", 1, 3, 3000)
	before := r.a.History()
	mustCompact(t, r.a)
	c := nextSummary(t, r)
	r.a.Interrupt()
	select {
	case <-c.ctx.Done():
		t.Fatal("the first Esc disconnected the request")
	default:
	}
	c.reply("late")
	wait(t, r.a)
	if !reflect.DeepEqual(r.a.History(), before) || st.count("compact") != 0 {
		t.Error("history changed after Esc")
	}
	if w := r.rec.texts(EvWarning); len(w) == 0 || w[len(w)-1] != compactStopped {
		t.Errorf("warnings %v", w)
	}
	if e := r.rec.of(EvCompacted); len(e) != 1 || e[0].Spans != 0 {
		t.Errorf("Compacted %+v", e)
	}
}

func TestCompact_SecondEscAndAbortDisconnect(t *testing.T) {
	for _, hard := range []string{"double", "abort"} {
		t.Run(hard, func(t *testing.T) {
			r := compactRig(t)
			workTurn(t, r, "go", 1, 3, 3000)
			before := r.a.History()
			mustCompact(t, r.a)
			c := nextSummary(t, r)
			if hard == "double" {
				r.a.Interrupt()
				r.a.Interrupt()
			} else {
				r.a.Abort()
			}
			<-c.ctx.Done()
			wait(t, r.a)
			if !reflect.DeepEqual(r.a.History(), before) {
				t.Error("history changed")
			}
		})
	}
}

func TestCompact_FinishedSpansStayAfterEsc(t *testing.T) {
	r := compactRig(t)
	workTurn(t, r, "a", 1, 3, 3000)
	workTurn(t, r, "b", 10, 3, 3000)
	mustCompact(t, r.a)
	nextSummary(t, r).reply("one")
	c := nextSummary(t, r)
	r.a.Interrupt()
	c.reply("two")
	wait(t, r.a)
	if s := summaries(r.a.History()); len(s) != 1 || s[0].Content != "[work summary] one" {
		t.Fatalf("summaries %+v", s)
	}
	assertHistoryValid(t, r.a.History())
}

func TestCompact_AfterShutdownIsErrStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := newRig(t)
	r.a.Start(ctx)
	cancel()
	var err error
	for end := time.Now().Add(deadline); !errors.Is(err, ErrStopped) && time.Now().Before(end); {
		err = r.a.Compact(context.Background())
		runtime.Gosched()
	}
	if !errors.Is(err, ErrStopped) {
		t.Errorf("Compact after shutdown: %v", err)
	}
}

func TestCompact_TextSubmittedBeforeJoinsTheRunningTurnNotTheCompaction(t *testing.T) {
	r := compactRig(t)
	workTurn(t, r, "go", 1, 3, 3000)
	r.a.Submit("one")
	mustCompact(t, r.a)
	r.a.Submit("two")
	r.p.next(t).reply("answer one")
	nextSummary(t, r).reply("ok")
	c := r.p.next(t)
	if got := c.req.Messages[len(c.req.Messages)-1]; got.Content != "two" {
		t.Errorf("last message %+v", got)
	}
	c.reply("answer two")
	wait(t, r.a)
}

// ---------------------------------------------------------------------------
// Store and Trace
// ---------------------------------------------------------------------------

func TestCompact_TraceRecord(t *testing.T) {
	tr := &fakeTrace{}
	r := compactRig(t, withStore(nil, tr))
	workTurn(t, r, "go", 1, 3, 3000)
	mustCompact(t, r.a)
	nextSummary(t, r).reply("ok")
	wait(t, r.a)
	recs := tr.of("compact")
	if len(recs) != 1 {
		t.Fatalf("%d compact records", len(recs))
	}
	d := recs[0].D
	if d["applied"] != true || d["fallback"] != false || d["prompt"] != summaryPrompt || d["from_seq"] == nil || d["to_seq"] == nil ||
		d["gain"].(float64) < 30 || len(d["inputs"].([]any)) != 1 || d["replies"].([]any)[0] != "ok" {
		t.Errorf("record %+v", d)
	}
}

func TestStore_CompactionReplaysToTheLiveHistory(t *testing.T) {
	w, plug := newSessionStore(t)
	r := compactRig(t, plug)
	workTurn(t, r, "first", 1, 3, 3000) // model summary
	fatTurn(t, r, "second", 10, 3)      // generated list
	workTurn(t, r, "third", 20, 1, 0)   // no gain, stays
	mustCompact(t, r.a)
	nextSummary(t, r).reply("did the first")
	wait(t, r.a)
	if n := len(summaries(r.a.History())); n != 2 {
		t.Fatalf("%d summaries", n)
	}
	sameAsFile(t, w, r.a, "after the Compaction")
	nextTurn(t, r, "again")
	l := sameAsFile(t, w, r.a, "after a Turn following the Compaction")
	if len(l.Dangling) != 0 {
		t.Errorf("dangling %+v", l.Dangling)
	}
	// A resumed Session carries on above the summary's Seq.
	l2, err := session.Load(w.Path())
	if err != nil || l2.MaxSeq() < r.a.History()[len(r.a.History())-1].Seq {
		t.Errorf("MaxSeq %d, err %v", l2.MaxSeq(), err)
	}
}

// ---------------------------------------------------------------------------
// Auto-compaction
// ---------------------------------------------------------------------------

// autoTurn runs a Turn with one noted step whose final answer reports `used` tokens, and
// returns after the TurnEnded event (a Compaction may follow).
func autoTurn(t *testing.T, r *rig, user string, id, used int) {
	t.Helper()
	n := len(r.rec.of(EvTurnEnded))
	r.a.Submit(user)
	r.p.next(t).toolStep(noteText(id, 3000), readCall(id, fmt.Sprintf("f%d.go", id)))
	c := r.p.next(t)
	c.usage(used-5, 5)
	c.reply("answer to " + user)
	r.rec.until(t, EvTurnEnded, n+1)
}

func autoRig(t *testing.T, pct int, mod ...func(*Deps)) *rig {
	return compactRig(t, append([]func(*Deps){func(d *Deps) { d.AutoCompact = pct; d.ContextLimit = 10000 }}, mod...)...)
}

func TestAuto_RunsBetweenTurnsBeforeTheNextText(t *testing.T) {
	var lane *laneProvider
	r := autoRig(t, 70, withLane(&lane))
	autoTurn(t, r, "t1", 1, 7000)
	r.a.Submit("t2")
	nextSummary(t, r).reply("ok")
	r.p.next(t).reply("answer two")
	wait(t, r.a)
	if got := lane.order(); !strings.Contains(got, "turn:end summary:start summary:end turn:start") {
		t.Errorf("order %s", got)
	}
	ev := r.rec.all()
	var started, ended int = -1, -1
	for i, e := range ev {
		if e.Kind == EvTurnEnded && ended < 0 {
			ended = i
		}
		if e.Kind == EvCompactStarted && started < 0 {
			started = i
		}
	}
	if ended < 0 || started < ended {
		t.Errorf("TurnEnded at %d, CompactStarted at %d", ended, started)
	}
	if s := summaries(r.a.History()); len(s) != 1 {
		t.Errorf("summaries %+v", s)
	}
}

func TestAuto_Threshold(t *testing.T) {
	for _, tc := range []struct {
		name      string
		pct, used int
		want      bool
	}{
		{"just below", 70, 6999, false}, {"exactly", 70, 7000, true},
		{"config 50", 50, 5000, true}, {"config 50 below", 50, 4999, false}, {"off", 0, 9999, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := autoRig(t, tc.pct)
			autoTurn(t, r, "t1", 1, tc.used)
			if tc.want {
				nextSummary(t, r).reply("ok")
			}
			wait(t, r.a)
			noCall(t, r)
			if got := len(summaries(r.a.History())) == 1; got != tc.want {
				t.Errorf("compacted: %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAuto_NoSpanNoRun(t *testing.T) {
	r := autoRig(t, 70)
	r.a.Submit("hi")
	c := r.p.next(t)
	c.usage(9000, 5)
	c.reply("hello")
	wait(t, r.a)
	noCall(t, r)
	if len(r.rec.of(EvCompactStarted)) != 0 || offWarnings(r) != 0 {
		t.Errorf("events %v", r.rec.texts(EvWarning))
	}
}

// failSummary plays a summary request that fails mid-stream.
func failSummary(t *testing.T, r *rig) {
	t.Helper()
	nextSummary(t, r).end(chat.FinishError, errors.New("boom"))
}

func offWarnings(r *rig) (n int) {
	for _, w := range r.rec.texts(EvWarning) {
		if w == autoOffWarning {
			n++
		}
	}
	return n
}

func TestAuto_CircuitBreakerAndManualRearm(t *testing.T) {
	r := autoRig(t, 70)
	autoTurn(t, r, "t1", 1, 7000)
	failSummary(t, r)
	autoTurn(t, r, "t2", 2, 7000)
	failSummary(t, r)
	wait(t, r.a)
	if offWarnings(r) != 1 {
		t.Fatalf("warnings %v", r.rec.texts(EvWarning))
	}
	autoTurn(t, r, "t3", 3, 7000)
	wait(t, r.a)
	noCall(t, r) // the breaker is open: nothing is requested
	if offWarnings(r) != 1 {
		t.Errorf("warned again: %v", r.rec.texts(EvWarning))
	}
	mustCompact(t, r.a) // manual, succeeds, re-arms
	nextSummary(t, r).reply("ok")
	wait(t, r.a)
	autoTurn(t, r, "t4", 4, 7000)
	nextSummary(t, r).reply("ok")
	wait(t, r.a)
}

func TestAuto_FailureCounterResetsOnSuccess(t *testing.T) {
	r := autoRig(t, 70)
	autoTurn(t, r, "t1", 1, 7000)
	failSummary(t, r)
	autoTurn(t, r, "t2", 2, 7000)
	nextSummary(t, r).reply("ok")
	autoTurn(t, r, "t3", 3, 7000)
	failSummary(t, r)
	autoTurn(t, r, "t4", 4, 7000)
	nextSummary(t, r).reply("ok") // still armed
	wait(t, r.a)
	if offWarnings(r) != 0 {
		t.Errorf("breaker tripped: %v", r.rec.texts(EvWarning))
	}
}
