package agent

import (
	"fmt"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Store and Trace: what the agent tells the outside world about its history
// ---------------------------------------------------------------------------

// Store receives every change of the history, in history order, from the worker (or the
// caller of Clear) right after the change and never under the Agent's lock, so a Store may
// call History. `session.Store` adapts a SessionFile Writer to it. A nil Store is skipped.
// The first error is reported as ONE Warning (I-7); after it the Store is not called again
// (a file with a hole would not replay), and the Turn goes on.
type Store interface {
	Message(m chat.Message) error // an appended message (Seq set)
	Patch(ps []chat.Patch) error  // one stub batch / argument shrink
	Clear() error
	Compact(fromSeq, toSeq int, summary chat.Message) error // one WorkSpan replaced by its WorkSummary
	Repair(ms []chat.Message) error                         // synthetic results made by Load
	Model(name string) error                                // a Model choice (SetModel)
	Thinking(on bool) error                                 // a thinking mode switch (SetThinking)
	Flush() error                                           // end of every Turn
}

// Trace receives debug records; `*trace.Writer` satisfies it (and is nil-safe). A Trace that
// also has `Err() error` gets its first write error reported as one Warning. A nil Trace is
// skipped. Records are never made under the Agent's lock.
type Trace interface {
	Record(kind string, v any)
}

// outOp is one queued call to the Store (store != nil), the Trace, or a rearm: the Store is
// called again from this point on after an earlier error (a Load switched the SessionFile).
type outOp struct {
	store func(Store) error
	rearm bool
	kind  string
	v     any
}

// ---------------------------------------------------------------------------
// Queueing (under mu) and draining (outside mu)
// ---------------------------------------------------------------------------

// storeLocked queues a Store call. Caller holds mu.
func (a *Agent) storeLocked(f func(Store) error) {
	if a.deps.Store != nil {
		a.out = append(a.out, outOp{store: f})
	}
}

// traceLocked queues a Trace record. Caller holds mu.
func (a *Agent) traceLocked(kind string, v any) {
	if a.deps.Trace != nil {
		a.out = append(a.out, outOp{kind: kind, v: v})
	}
}

// trace records at once; call it without mu.
func (a *Agent) trace(kind string, v any) {
	if a.deps.Trace == nil {
		return
	}
	a.mu.Lock()
	a.traceLocked(kind, v)
	a.mu.Unlock()
	a.sync()
}

// sync drains the queue and shows the warnings it produced; call it without mu.
func (a *Agent) sync() {
	for _, w := range a.drain() {
		a.emit(Event{Kind: EvWarning, Text: w})
	}
}

// drain performs the queued Store calls and Trace records in order. Concurrent callers
// are serialized by storeMu, so the order of the queue is the order of the calls. It returns
// the warnings to show (at most one per Store and one per Trace for the Agent's life).
func (a *Agent) drain() (warns []string) {
	if a.deps.Store == nil && a.deps.Trace == nil {
		return nil
	}
	a.storeMu.Lock()
	defer a.storeMu.Unlock()
	a.mu.Lock()
	ops := a.out
	a.out = nil
	a.mu.Unlock()
	if len(ops) == 0 {
		return nil
	}
	for _, op := range ops {
		if op.rearm {
			a.storeOff = false
			continue
		}
		if op.store == nil {
			a.deps.Trace.Record(op.kind, op.v)
			continue
		}
		if a.storeOff {
			continue
		}
		if err := op.store(a.deps.Store); err != nil {
			a.storeOff = true
			warns = append(warns, fmt.Sprintf("the session is no longer saved: %v", err))
		}
	}
	if e, ok := a.deps.Trace.(interface{ Err() error }); ok && !a.traceWarned {
		if err := e.Err(); err != nil {
			a.traceWarned = true
			warns = append(warns, fmt.Sprintf("the trace is no longer written: %v", err))
		}
	}
	return warns
}

// ---------------------------------------------------------------------------
// Stub batches: patches for the Store and a record for the Trace
// ---------------------------------------------------------------------------

// flushBatchLocked turns the patches collected by apply and shrinkArgs on c into one Store
// record (the Session only: a Task's Seqs are not in the SessionFile) and one stub_batch trace
// record. Caller holds mu.
func (a *Agent) flushBatchLocked(c *conversation, why string) {
	if len(c.batch) == 0 {
		return
	}
	ps := c.batch
	c.batch = nil
	if c.depth == 0 {
		a.storeLocked(func(s Store) error { return s.Patch(ps) })
	}
	if a.deps.Trace != nil {
		var stubbed, shrunk []int
		for _, p := range ps {
			if p.Stub != "" {
				stubbed = append(stubbed, p.Seq)
			}
			if p.Args != nil {
				shrunk = append(shrunk, p.Seq)
			}
		}
		a.traceLocked("stub_batch", c.tagged(map[string]any{"why": why, "stubbed": stubbed, "shrunk": shrunk}))
	}
}
