package agent

import (
	"math"
	"testing"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Generation speed (R-29)
// ---------------------------------------------------------------------------

func at(s float64) time.Time { return time.Unix(0, 0).Add(time.Duration(s * float64(time.Second))) }

func TestStepClock_Speed(t *testing.T) {
	type ev struct {
		at float64
		e  chat.StreamEvent
	}
	delta := chat.StreamEvent{Kind: chat.EventDelta, Text: "x"}
	empty := chat.StreamEvent{Kind: chat.EventDelta}
	prog := chat.StreamEvent{Kind: chat.EventProgress, Done: 1, Total: 2}
	usage := func(n int) chat.StreamEvent {
		return chat.StreamEvent{Kind: chat.EventUsage, Usage: chat.Usage{CompletionTokens: n}}
	}
	done := chat.StreamEvent{Kind: chat.EventDone, Finish: chat.FinishStop}
	for _, tc := range []struct {
		name string
		evs  []ev
		want float64 // 0 = no speed
	}{
		{"from the first text", []ev{{1, prog}, {2, delta}, {3, delta}, {4, usage(40)}, {4, done}}, 20},
		{"empty delta does not start it", []ev{{1, empty}, {2, delta}, {4, usage(40)}, {4, done}}, 20},
		{"tool calls only: from prompt reading", []ev{{1, prog}, {3, prog}, {5, usage(30)}, {5, done}}, 15},
		{"nothing streamed: from the request", []ev{{4, usage(20)}, {4, done}}, 5},
		{"no usage", []ev{{1, delta}, {3, done}}, 0},
		{"zero tokens", []ev{{1, delta}, {3, usage(0)}, {3, done}}, 0},
		{"no end", []ev{{1, delta}, {3, usage(40)}}, 0},
		{"no time passed", []ev{{2, delta}, {2, usage(40)}, {2, done}}, 0},
	} {
		c := stepClock{start: at(0)}
		for _, e := range tc.evs {
			c.note(e.e, at(e.at))
		}
		tps, _, ok := c.speed()
		if got := math.Round(tps*10) / 10; ok != (tc.want > 0) || got != tc.want {
			t.Errorf("%s: speed %v ok %v, want %v", tc.name, got, ok, tc.want)
		}
	}
}

// speedRig is a started rig whose clock moves 1 s at every call.
func speedRig(t *testing.T) *rig {
	var s float64
	return startRig(t, func(d *Deps) {
		d.Now = func() time.Time { s++; return at(s) }
	})
}

func TestSpeed_EmittedBeforeTurnEnd(t *testing.T) {
	r := speedRig(t)
	r.a.Submit("q")
	c := r.p.next(t)
	c.say("a", "b") // now: request 1, a 2, b 3
	c.usage(1, 30)  // 4
	c.end(chat.FinishStop, nil)
	wait(t, r.a)
	sp := r.rec.of(EvSpeed)
	if len(sp) != 1 || math.Round(sp[0].Speed) != 10 { // 30 tokens / (5-2) s
		t.Fatalf("speed events %+v", sp)
	}
	var kinds []EventKind
	for _, e := range r.rec.all() {
		kinds = append(kinds, e.Kind)
	}
	si, ti := -1, -1
	for i, k := range kinds {
		if k == EvSpeed {
			si = i
		}
		if k == EvTurnEnded {
			ti = i
		}
	}
	if si < 0 || si > ti {
		t.Errorf("EvSpeed at %d, TurnEnded at %d", si, ti)
	}
}

func TestSpeed_AbortedStepHasNone(t *testing.T) {
	r := speedRig(t)
	r.a.Submit("q")
	c := r.p.next(t)
	c.say("a")
	r.rec.until(t, EvDelta, 1)
	r.a.Abort()
	wait(t, r.a)
	if sp := r.rec.of(EvSpeed); len(sp) != 0 {
		t.Errorf("speed after an abort: %+v", sp)
	}
}

func TestSpeed_InterruptedStepStillMeasured(t *testing.T) {
	r := speedRig(t)
	r.a.Submit("q")
	c := r.p.next(t)
	c.say("a")
	r.rec.until(t, EvDelta, 1)
	r.a.Interrupt()
	r.rec.until(t, EvDraining, 1)
	c.say("b")
	c.usage(1, 10)
	c.end(chat.FinishStop, nil)
	wait(t, r.a)
	if sp := r.rec.of(EvSpeed); len(sp) != 1 || sp[0].Speed <= 0 {
		t.Errorf("speed events %+v, want one (the drain read the stream to its end)", sp)
	}
}

func TestSpeed_TraceRecord(t *testing.T) {
	tr := &fakeTrace{}
	var s float64
	r := startRig(t, func(d *Deps) { d.Trace = tr; d.Now = func() time.Time { s++; return at(s) } })
	r.a.Submit("q")
	c := r.p.next(t)
	c.say("a")
	c.usage(1, 8)
	c.end(chat.FinishStop, nil)
	wait(t, r.a)
	recs := tr.of("speed")
	if len(recs) != 1 || recs[0].D["tokens"] != float64(8) || recs[0].D["ms"] != float64(2000) {
		t.Errorf("trace %+v", recs)
	}
}

// Found in the T3 final regression: the panel showed "~0% · ~0/16k" for the whole first Turn,
// because the first Meter event came only at the Turn end. A Turn now starts with the estimate.
func TestTurn_MeterEstimateAtTurnStart(t *testing.T) {
	r := startRig(t)
	r.a.Submit("a question that is long enough to count")
	c := r.p.next(t)
	r.rec.until(t, EvMeter, 1)
	if m := r.rec.lastMeter(t); m.Used == 0 || !m.Estimated {
		t.Errorf("meter at the Turn start %+v, want a non-zero estimate", m)
	}
	c.reply("ok")
	wait(t, r.a)
}
