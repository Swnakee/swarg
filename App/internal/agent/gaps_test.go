package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Gap tests found by the T2_4 coverage audit: I-7 (Store and Trace never fail a Turn)
// during a Compaction, and a resumed history that already holds a WorkSummary.
// ---------------------------------------------------------------------------

// compactFailStore is a Store whose Compact call fails (a disk that fills up mid-compaction).
type compactFailStore struct{ *fakeStore }

func (s compactFailStore) Compact(int, int, chat.Message) error { return errors.New("disk full") }

func warningsWith(r *rig, part string) (n int) {
	for _, w := range r.rec.texts(EvWarning) {
		if strings.Contains(w, part) {
			n++
		}
	}
	return n
}

func TestStore_FailureMidCompactionIsOneWarningAndTheHistoryStaysValid(t *testing.T) {
	st := &fakeStore{}
	r := compactRig(t, func(d *Deps) { d.Store = compactFailStore{st} })
	workTurn(t, r, "first", 1, 2, 3000)
	workTurn(t, r, "second", 10, 2, 3000)

	mustCompact(t, r.a)
	nextSummary(t, r).reply("read f1, f2")
	nextSummary(t, r).reply("read f10, f11")
	wait(t, r.a)

	h := r.a.History()
	assertHistoryValid(t, h)
	if len(summaries(h)) != 2 || len(r.rec.of(EvCompacted)) != 1 {
		t.Errorf("compaction did not finish: %d summaries, %d EvCompacted", len(summaries(h)), len(r.rec.of(EvCompacted)))
	}
	if n := warningsWith(r, "disk full"); n != 1 {
		t.Errorf("warnings %q, want exactly one naming the error", r.rec.texts(EvWarning))
	}
	// the Store is off after its first error: later Turns still run and nothing more is stored
	stored := len(st.all())
	r.a.Submit("third")
	r.p.next(t).reply("answer to third")
	wait(t, r.a)
	if len(r.a.History()) != len(h)+2 || len(st.all()) != stored {
		t.Errorf("after the failure: history %d (want %d), store ops %d (want %d)", len(r.a.History()), len(h)+2, len(st.all()), stored)
	}
}

func TestTrace_FailureDuringToolsAndCompactionDoesNotFailTheTurn(t *testing.T) {
	tr := &fakeTrace{err: errors.New("no space")}
	r := compactRig(t, withStore(nil, tr))
	workTurn(t, r, "first", 1, 2, 3000)
	workTurn(t, r, "second", 10, 2, 3000)
	mustCompact(t, r.a)
	nextSummary(t, r).reply("a")
	nextSummary(t, r).reply("b")
	wait(t, r.a)

	if n := warningsWith(r, "no space"); n != 1 {
		t.Errorf("warnings %q, want exactly one", r.rec.texts(EvWarning))
	}
	for _, ev := range r.rec.of(EvTurnEnded) {
		if ev.Err != nil {
			t.Errorf("Turn failed because of the Trace: %v", ev.Err)
		}
	}
	if len(summaries(r.a.History())) != 2 {
		t.Error("compaction did not finish")
	}
}

func TestLoad_HistoryWithAWorkSummaryContinuesAndIsNeverCompactedAgain(t *testing.T) {
	r := compactRig(t)
	workTurn(t, r, "first", 1, 2, 3000)
	mustCompact(t, r.a)
	nextSummary(t, r).reply("read f1, f2")
	wait(t, r.a)
	saved := r.a.History()
	if len(summaries(saved)) != 1 {
		t.Fatalf("setup: %d summaries", len(summaries(saved)))
	}

	r2 := compactRig(t)
	if err := r2.a.Load(saved); err != nil {
		t.Fatal(err)
	}
	r2.a.Submit("next")
	c := r2.p.next(t)
	c.reply("fine")
	wait(t, r2.a)

	got := c.req.Messages
	var hasSummary bool
	for _, m := range got {
		hasSummary = hasSummary || (m.Role == chat.RoleAssistant && strings.HasPrefix(m.Content, "[work summary]"))
	}
	if !hasSummary || got[len(got)-1].Content != "next" {
		t.Errorf("request after Load lacks the summary or the new text: %+v", got)
	}
	assertHistoryValid(t, r2.a.History())
	// nothing left to compact: the summary is never a span of its own
	if err := r2.a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	wait(t, r2.a)
	noCall(t, r2)
	if len(summaries(r2.a.History())) != 1 {
		t.Error("a WorkSummary was compacted again")
	}
}

// A worker whose context is cancelled stops before it runs another queued control item
// (found as a flake of TestCompact_AfterShutdownIsErrStopped: the queue never ran dry).
func TestShutdown_QueuedCompactionIsNotRunAfterCancel(t *testing.T) {
	r := newRig(t)
	if err := r.a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.a.Start(ctx)
	wait(t, r.a)
	if ws := r.rec.texts(EvWarning); len(ws) != 0 {
		t.Errorf("the worker ran a queued Compaction after its context was cancelled: %q", ws)
	}
}

// A Turn interrupted after its tools ran: the history is valid and the next request carries
// the Stub of the finished result.
func TestEndTurn_InterruptedTurnStillStubsItsResults(t *testing.T) {
	et := newEconTools()
	et.out["c1"] = big(200)
	r := econRig(t, et)
	r.a.Submit("go")
	r.p.next(t).toolStep("", readCall(1, "a.go"))
	c := r.p.next(t)
	c.say("par")
	r.rec.until(t, EvDelta, 1)
	r.a.Interrupt()
	r.rec.until(t, EvDraining, 1)
	c.say("tial")
	c.end(chat.FinishStop, nil)
	wait(t, r.a)
	assertHistoryValid(t, r.a.History())
	if got := toolContents(nextTurn(t, r, "again")); len(got) != 1 || got[0] != "[read_file a.go: S]" {
		t.Errorf("contents = %q, want the stub", got)
	}
}

// Clear and Load reset the per-category breakdown to the new history.
func TestMeter_BreakdownAfterClearAndLoad(t *testing.T) {
	r := compactRig(t)
	workTurn(t, r, "first", 1, 2, 3000)
	saved := r.a.History()
	before := r.rec.lastMeter(t)
	if before.Breakdown.Total() != before.Used || before.Used == 0 {
		t.Fatalf("setup: %+v", before)
	}
	if err := r.a.Clear(); err != nil {
		t.Fatal(err)
	}
	if m := r.rec.lastMeter(t); m.Breakdown.User != 0 || m.Breakdown.Total() != m.Used || m.Used >= before.Used {
		t.Errorf("after Clear: %+v (before %+v)", m, before)
	}
	if err := r.a.Load(saved); err != nil {
		t.Fatal(err)
	}
	wait(t, r.a)
	if m := r.rec.lastMeter(t); m.Breakdown.User == 0 || m.Breakdown.Total() != m.Used {
		t.Errorf("after Load: %+v", m)
	}
}
