package agent

import (
	"context"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Compaction: finished WorkSpans become one WorkSummary each (PLAN 2.6, 5.2, 5.3; R-16, R-18)
//
// A control item of the queue: the worker runs it between Turns, so it never overlaps a model
// request of a Turn (I-1). The current Turn is never touched because none is running.
// ---------------------------------------------------------------------------

const (
	maxAutoFails     = 2 // consecutive failed or no-gain automatic Compactions that switch auto off
	nothingToCompact = "nothing to compact"
	compactStopped   = "compaction stopped"
	autoOffWarning   = "auto-compaction is off: context still full: use /clear"
)

// compactItem is a Compaction waiting in the queue; auto marks one started by the agent itself.
type compactItem struct{ auto bool }

// outcome is what one Compaction run did.
type outcome struct {
	replaced int
	failed   bool   // a summary request failed, so a generated list was used
	err      string // the first summary request error
	stopped  bool   // the user stopped it
}

// Compact queues a Compaction and returns at once: the worker runs it between Turns, in queue
// order (text submitted after it waits for it, a running Turn finishes first). It emits
// EvCompactStarted and EvCompacted, or a warning when there is nothing to compact; failures
// are warnings. ErrStopped after the worker stopped.
func (a *Agent) Compact(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return ErrStopped
	}
	a.queue = append(a.queue, qitem{compact: &compactItem{}})
	a.mu.Unlock()
	signal(a.wake)
	return nil
}

// enqueueAutoLocked puts an automatic Compaction at the head of the queue (before queued
// texts) when the meter has reached AutoCompact percent of the limit, a WorkSpan exists and
// the breaker is closed. The worker calls it at the end of every Turn. Caller holds mu.
func (a *Agent) enqueueAutoLocked() {
	pct, limit := a.deps.AutoCompact, a.meter.limit
	if pct <= 0 || limit <= 0 || a.autoOff || a.stopped {
		return
	}
	if len(a.queue) > 0 && a.queue[0].control() {
		return
	}
	if a.meterSnapshot(&a.conversation).Used*100 < pct*limit || len(findSpans(a.history)) == 0 {
		return
	}
	a.queue = append([]qitem{{compact: &compactItem{auto: true}}}, a.queue...)
}

// stopCompactLocked is Esc during a Compaction: the first press stops it after the summary
// request in flight was read to its end; a second press or hard disconnects that request.
// It returns the cancel func to call after unlocking, if any. Caller holds mu.
func (a *Agent) stopCompactLocked(hard bool) context.CancelFunc {
	if !a.compacting {
		return nil
	}
	hard = hard || a.compactStop
	a.compactStop = true
	if hard {
		a.compactAborted = true
		return a.compactCancel
	}
	return nil
}

func (a *Agent) compactStopped() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.compactStop
}

// runCompact performs the Compaction at the head of the queue; worker only, between Turns.
// The item leaves the queue last, so Busy and Wait cover the whole run.
func (a *Agent) runCompact(ctx context.Context, it *compactItem) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.mu.Lock()
	a.compacting, a.compactStop, a.compactAborted, a.compactCancel = true, false, false, cancel
	spans := findSpans(a.history)
	before := estimateRequest(a.request(&a.conversation)).Total()
	a.mu.Unlock()

	var out outcome
	if len(spans) == 0 {
		a.emit(Event{Kind: EvWarning, Text: nothingToCompact})
	} else {
		a.emit(Event{Kind: EvCompactStarted, Spans: len(spans), Before: before})
		out = a.compactSpans(sctx, spans)
	}

	a.mu.Lock()
	aborted := a.compactAborted
	a.compacting, a.compactCancel = false, nil
	after := estimateRequest(a.request(&a.conversation)).Total()
	meterEv, warns := a.meterEvent()
	if len(spans) > 0 {
		if w := a.settleAutoLocked(it, out); w != "" {
			warns = append(warns, w)
		}
	}
	a.mu.Unlock()
	a.sync()
	if len(spans) > 0 {
		if out.failed {
			a.emit(Event{Kind: EvWarning, Text: "compaction: the summary request failed (" + out.err + "); generated lists were used"})
		}
		if out.stopped {
			a.emit(Event{Kind: EvWarning, Text: compactStopped})
		}
		a.emit(meterEv)
		for _, w := range warns {
			a.emit(Event{Kind: EvWarning, Text: w})
		}
		a.emit(Event{Kind: EvCompacted, Spans: out.replaced, Before: before, After: after})
	}
	if health := a.health(); aborted && health != nil {
		go a.checkHealth(ctx, health)
	}
	a.mu.Lock()
	a.queue = a.queue[1:] // only now: Busy and Wait cover the Store and Trace calls of the run
	a.idle.Broadcast()
	a.mu.Unlock()
}

// settleAutoLocked updates the circuit breaker after a run and returns the warning to show
// when it just opened. A stopped run counts for nothing; a manual success closes the breaker.
func (a *Agent) settleAutoLocked(it *compactItem, o outcome) string {
	if o.stopped {
		return ""
	}
	if !it.auto {
		if o.replaced > 0 && !o.failed {
			a.autoFails, a.autoOff = 0, false
		}
		return ""
	}
	if !o.failed && o.replaced > 0 {
		a.autoFails = 0
		return ""
	}
	if a.autoFails++; a.autoFails >= maxAutoFails && !a.autoOff {
		a.autoOff = true
		return autoOffWarning
	}
	return ""
}

// compactSpans replaces the spans one by one, oldest first, stopping when the user did.
func (a *Agent) compactSpans(ctx context.Context, spans []span) (out outcome) {
	for _, s := range spans {
		if a.compactStopped() || ctx.Err() != nil {
			out.stopped = true
			break
		}
		applied, failed, err := a.compactOne(ctx, s)
		if ctx.Err() != nil || a.compactStopped() {
			out.stopped = true // the reply of a stopped request is thrown away
			break
		}
		if applied {
			out.replaced++
		}
		if failed && !out.failed {
			out.failed, out.err = true, err.Error()
		}
	}
	return out
}

// compactOne decides the text for one span and replaces it. failed is set (with err) when a
// summary request failed and the generated list was used instead.
func (a *Agent) compactOne(ctx context.Context, s span) (applied, failed bool, err error) {
	text, why := summaryPrefix+s.list, "small"
	var rec summaryRecord
	if s.tokens >= smallSpanTokens {
		reply, aerr := a.summarize(ctx, s, &rec)
		if ctx.Err() != nil || a.compactStopped() {
			return false, false, nil
		}
		switch cand := summaryPrefix + reply; {
		case aerr != nil:
			failed, err, why = true, aerr, "error"
		case reply == "":
			why = "empty"
		case chat.EstimateTokens(cand) >= s.tokens:
			why = "longer than the span"
		case !gainOK(cand, s.tokens):
			why = "no gain"
		default:
			text, why = cand, ""
		}
	}
	rep := map[string]any{"from_seq": s.fromSeq, "to_seq": s.toSeq, "tokens": s.tokens, "prompt": summaryPrompt,
		"inputs": rec.inputs, "replies": rec.replies, "fallback": why != "" && why != "small", "why": why, "summary": text}
	if !gainOK(text, s.tokens) {
		rep["applied"], rep["gain"] = false, gainPercent(text, s.tokens)
		a.trace("compact", rep)
		return false, failed, err
	}
	rep["applied"], rep["gain"] = true, gainPercent(text, s.tokens)
	return a.replaceSpan(s, text, rep), failed, err
}

// replaceSpan swaps the Messages Seq fromSeq..toSeq for one WorkSummary, queues the Store and
// Trace records and makes the meter fall back to the estimate. It reports whether it did.
func (a *Agent) replaceSpan(s span, text string, rep map[string]any) bool {
	a.mu.Lock()
	i, j := indexOfSeq(a.history, s.fromSeq), indexOfSeq(a.history, s.toSeq)
	if i < 0 || j < i {
		a.mu.Unlock()
		return false
	}
	a.seq++
	sum := chat.Message{Role: chat.RoleAssistant, Content: text, Summary: true, Seq: a.seq}
	h := make([]chat.Message, 0, len(a.history)-(j-i))
	h = append(append(append(h, a.history[:i]...), sum), a.history[j+1:]...)
	a.history = h
	a.meter.hasExact = false
	a.storeLocked(func(st Store) error { return st.Compact(s.fromSeq, s.toSeq, sum) })
	rep["summary_seq"] = sum.Seq
	a.traceLocked("compact", rep)
	a.mu.Unlock()
	a.sync()
	return true
}
