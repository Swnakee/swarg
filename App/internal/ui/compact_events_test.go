package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

func compactStarted() agent.Event { return agent.Event{Kind: agent.EvCompactStarted} }
func compacted(spans, before, after int) agent.Event {
	return agent.Event{Kind: agent.EvCompacted, Spans: spans, Before: before, After: after}
}

func TestCompaction_SpinnerStatusAndNotice(t *testing.T) {
	r := newRig(t)
	r.events(compactStarted())
	if v := r.view(); strings.Count(v, "compacting…") != 1 { // the Progress line only; the panel has no state item (R-31)
		t.Errorf("want compacting… once, on the Progress line:\n%s", v)
	}
	if !r.m.ticking {
		t.Error("spinner timer not started")
	}
	r.events(compacted(3, 9000, 4000))
	r.wantLine(result{}, "compacted 3 spans: ~9000 → ~4000 tokens")
	if strings.Contains(r.view(), "compacting…") {
		t.Error("compacting… stays after EvCompacted")
	}
}

func TestCompaction_EscInterruptsAndQuitWaits(t *testing.T) {
	r := newRig(t)
	r.events(compactStarted())
	r.send(key(tea.KeyEscape, 0))
	if r.ag.interrupts != 1 {
		t.Errorf("interrupts %d", r.ag.interrupts)
	}
	r.send(key(tea.KeyEscape, 0))
	if r.ag.aborts != 1 {
		t.Errorf("aborts %d", r.ag.aborts)
	}
	r.events(compacted(1, 10, 5))
	r.events(compactStarted())
	if res := r.send(key('c', tea.ModCtrl)); res.quit {
		t.Error("quit did not wait for the compaction")
	}
	if res := r.events(compacted(1, 10, 5)); !res.quit {
		t.Error("no quit after the compaction ended")
	}
}

func TestCompaction_TurnStartClearsFlag(t *testing.T) {
	r := newRig(t)
	r.events(compactStarted(), started("q"))
	if r.m.compacting {
		t.Error("compacting stuck after a Turn started")
	}
}

// scriptedAgent answers Submit by queueing a fixed list of events.
type scriptedAgent struct {
	*fakeAgent
	ch  chan agent.Event
	evs []agent.Event
}

func (a *scriptedAgent) Submit(string) {
	for _, e := range a.evs {
		a.ch <- e
	}
}

func TestPlain_CompactionLines(t *testing.T) {
	ch := make(chan agent.Event, 8)
	ag := &scriptedAgent{fakeAgent: &fakeAgent{}, ch: ch, evs: []agent.Event{
		compactStarted(), compacted(2, 100, 50), {Kind: agent.EvTurnEnded, Finish: chat.FinishStop},
	}}
	var out bytes.Buffer
	err := RunPlain(context.Background(), Deps{Agent: ag, Events: ch}, strings.NewReader("hi"), &out)
	if err != nil || out.String() != "[compacting…]\n[compacted 2 spans: ~100 → ~50 tokens]\n" {
		t.Errorf("err %v, out %q", err, out.String())
	}
}

func TestCompactLine_IgnoresOtherEvents(t *testing.T) {
	if _, ok := compactLine(agent.Event{Kind: agent.EvDelta}); ok {
		t.Error("line for a non-compaction event")
	}
}
