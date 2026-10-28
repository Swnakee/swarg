package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Streaming into the list
// ---------------------------------------------------------------------------

func TestStream_OpenItemGrows(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), delta("First para"))
	if !strings.Contains(r.view(), "First para") {
		t.Fatalf("a partial line must be shown:\n%s", r.view())
	}
	r.events(delta("graph.\n\nsecond"))
	if v := r.view(); !strings.Contains(v, "First paragraph.") || !strings.Contains(v, "second") {
		t.Errorf("deltas append to the open item:\n%s", v)
	}
	n := r.m.cv.count()
	r.events(delta(" more"))
	if r.m.cv.count() != n {
		t.Error("a delta of the same step must not add an item")
	}
	r.events(delta("```go\nx := 1\n"))
	if v := r.view(); !strings.Contains(v, "x := 1") {
		t.Errorf("an open fence must render as a code block:\n%s", v)
	}
}

func TestStream_OpenFenceThousandRowsShowsTheTail(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), delta("```\n"))
	var evs []agent.Event
	for i := 1; i <= 1000; i++ {
		evs = append(evs, delta(fmt.Sprintf("code line %d\n", i)))
	}
	r.events(evs...)
	v := r.view()
	if !strings.Contains(v, "code line 1000") || !strings.Contains(v, "code line 990") {
		t.Errorf("the tail of an unclosed fence must be visible while streaming:\n%s", v)
	}
	wantFrame(t, r, 80, 40)
	r.events(ended(chat.FinishStop, nil))
	if !strings.Contains(r.view(), "code line 1000") {
		t.Errorf("tail lost at the end of the turn:\n%s", r.view())
	}
}

func TestStream_FinishedItemIsFinal(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), delta("answer one\n"), ended(chat.FinishStop, nil))
	items := r.m.cv.count()
	r.events(started("again"), delta("answer two\n"))
	got := r.convText()
	if strings.Count(got, "answer one") != 1 || !strings.Contains(got, "answer two") || r.m.cv.count() != items+2 {
		t.Errorf("a new Turn writes into new items:\n%s", got)
	}
}

func TestStream_ChunkSplitsInsideRuneOrEscape(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), delta("caf\xc3"), delta("\xa9 and \x1b"), delta("[31mred\x1b[0m done\n"))
	got := r.convText()
	if !strings.Contains(got, "café and") || strings.Contains(got, "�") || strings.Contains(r.m.View().Content, "\x1b[31mred") {
		t.Errorf("split chunks must be joined and cleaned: %q", got)
	}
}

func TestStream_InterruptKeepsTextAndMarksOnce(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), delta("shown before\n\n```go\nopen := 1\n"))
	r.send(key(tea.KeyEscape, 0))
	r.events(ended(chat.FinishInterrupted, nil))
	got := r.convText()
	if !strings.Contains(got, "shown before") || !strings.Contains(got, "open := 1") {
		t.Errorf("text shown before the interrupt must stay:\n%s", got)
	}
	if n := strings.Count(got, "interrupted"); n != 1 {
		t.Errorf("%d interrupted rows, want 1:\n%s", n, got)
	}
	r.events(delta("late text"))
	if strings.Contains(r.convText()[:strings.Index(r.convText(), "interrupted")], "late text") {
		t.Error("text after the interrupt appeared before the marker")
	}
	wantFrame(t, r, 80, 40)
}

func TestStream_InterruptDuringToolMarksOnce(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), delta("thinking\n"), shellStart("a", "sleep 9"))
	r.events(ended(chat.FinishInterrupted, nil))
	got := r.convText()
	if n := strings.Count(got, "interrupted"); n != 1 || !strings.Contains(got, "cancelled") {
		t.Errorf("one interrupted row and a cancelled block expected:\n%s", got)
	}
}

func TestStream_InterruptWithoutAnyTextMarksOnce(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), ended(chat.FinishInterrupted, nil))
	if n := strings.Count(r.convText(), "interrupted"); n != 1 {
		t.Errorf("%d interrupted rows:\n%s", n, r.convText())
	}
}

// ---------------------------------------------------------------------------
// The owner's "duck" session: interrupt of a long answer kept the screen intact (A2/A3/A4)
// ---------------------------------------------------------------------------

func TestEsc_OnceAndTwiceKeepBoxAndFrameIntact(t *testing.T) {
	for _, h := range []int{6, 13, 40} {
		r := newRig(t)
		r.size(80, h)
		echo := strings.Repeat("▄▄▄▄▄▄▄▄ ", 60)
		r.events(started("draw a duck"), delta("Here is the duck:\n\n```\n"))
		for i := 0; i < 64; i++ {
			r.events(delta(fmt.Sprintf("%s %d\n", echo, i)))
			if i%16 == 0 {
				wantFrame(t, r, 80, h)
			}
		}
		r.send(key(tea.KeyEscape, 0))
		wantFrame(t, r, 80, h)
		if !strings.Contains(r.view(), "Stopping…") && h > 8 {
			t.Errorf("h=%d no stopping hint:\n%s", h, r.view())
		}
		r.send(key(tea.KeyEscape, 0))
		wantFrame(t, r, 80, h)
		r.events(ended(chat.FinishInterrupted, nil))
		wantFrame(t, r, 80, h)
		if n := strings.Count(r.convText(), "interrupted"); n != 1 {
			t.Errorf("h=%d: %d interrupted rows", h, n)
		}
		if r.ag.interrupts != 1 || r.ag.aborts != 1 {
			t.Errorf("h=%d: interrupts %d aborts %d", h, r.ag.interrupts, r.ag.aborts)
		}
		r.events(started("again"), delta("fine\n"))
		wantFrame(t, r, 80, h)
	}
}

func TestEsc_HugeAnswerAtInterruptKeepsFrame(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), delta(strings.Repeat("a long line without a break ", 4000)))
	r.send(key(tea.KeyEscape, 0))
	r.events(ended(chat.FinishInterrupted, nil))
	wantFrame(t, r, 80, 40)
	if !strings.Contains(r.view(), "interrupted") {
		t.Errorf("no marker at the bottom:\n%s", r.view())
	}
}

// ---------------------------------------------------------------------------
// Status and notices
// ---------------------------------------------------------------------------

// The cache share moved from the status line to /context (R-31: the Bottom panel shows only
// model, context use, speed, auto-approve and jobs).
func TestPanel_NoCacheShare(t *testing.T) {
	r := newRig(t)
	r.events(agent.Event{Kind: agent.EvMeter, Used: 4000, Limit: 16000, CachePct: 80, HasCache: true})
	if v := r.view(); strings.Contains(v, "cache") || !strings.Contains(v, "qwen │ 25% · 4k/16k │ – tok/s │ ○ ask") {
		t.Errorf("panel:\n%s", v)
	}
}

func TestStatus_CompactingHook(t *testing.T) {
	r := newRig(t)
	r.m.compacting = true
	if !strings.Contains(r.view(), "compacting…") {
		t.Errorf("status lacks compacting…:\n%s", r.view())
	}
}

func TestModel_QueuedInputWhileBusyStillWorks(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	r.m.ta.SetValue("next thing")
	r.send(key(tea.KeyEnter, 0))
	if len(r.ag.submitted) != 1 || r.ag.submitted[0] != "next thing" {
		t.Errorf("submitted %q", r.ag.submitted)
	}
	r.events(agent.Event{Kind: agent.EvQueue, Queued: 1}, agent.Event{Kind: agent.EvUserText, Text: "next thing"})
	if v := r.view(); strings.Contains(v, "queued") || !strings.Contains(r.convText(), "▸ next thing") {
		t.Errorf("echo missing, or a queue count shown (Waiting Messages replace it):\n%s", v)
	}
}
