package agent

import (
	"errors"
	"regexp"
	"slices"
	"strconv"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Load: replace the history by a saved one (worker-serialized, PLAN 5.2)
// ---------------------------------------------------------------------------

// ErrStopped is returned by Load when the worker has stopped.
var ErrStopped = errors.New("agent: stopped")

// loadItem is a Load waiting in the queue.
type loadItem struct {
	history []chat.Message
	maxSeq  int
	model   string // WithModel; "" = keep the model
	think   *bool  // WithThinking; nil = keep the thinking mode
}

// LoadOption tunes Load.
type LoadOption func(*loadItem)

// WithMaxSeq tells Load the highest Seq the SessionFile ever used (`session.Loaded.MaxSeq`), so
// new messages continue above it even when the file's tail was cleared or compacted.
func WithMaxSeq(n int) LoadOption { return func(l *loadItem) { l.maxSeq = n } }

// WithThinking makes Load also set the thinking mode (a resumed Session continues with its last
// choice; a file with no choice is off). The SessionFile already holds it, so nothing is recorded.
func WithThinking(on bool) LoadOption { return func(l *loadItem) { l.think = &on } }

// callIDRe matches the ids the agent generates.
var callIDRe = regexp.MustCompile(`^call_(\d+)$`)

// Load queues the replacement of the history by history (a resumed Session); it returns at
// once. The worker runs it between Turns, in queue order: while a Turn runs, the Load waits
// for it (it is never refused), and a Submit after Load runs after it. Wait returns once it
// ran; an EvMeter event follows it. The worker restores Seq and the call id counter from the
// history, appends a `cancelled by the user` result for every ToolCall without a result (R-21)
// and records exactly those through Store.Repair, so the file and the live history match;
// the loaded messages themselves are not stored again. Messages without a Seq are numbered
// above the highest one. The input slice is copied.
func (a *Agent) Load(history []chat.Message, opts ...LoadOption) error {
	ld := &loadItem{history: slices.Clone(history)}
	for _, o := range opts {
		o(ld)
	}
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return ErrStopped
	}
	a.queue = append(a.queue, qitem{load: ld})
	a.mu.Unlock()
	signal(a.wake)
	return nil
}

// runLoad performs the Load at the head of the queue; worker only, between Turns. The item
// leaves the queue last, so Busy and Wait cover the whole Load.
func (a *Agent) runLoad(ld *loadItem) {
	system := a.deps.SystemPrompt()
	a.mu.Lock()
	a.system = system
	repaired := a.applyLoadLocked(ld)
	changed := ld.model != "" && ld.model != a.model
	if changed {
		a.model = ld.model
		a.setLimitLocked(ld.model) // after applyLoadLocked's meter.reset: the limit of the loaded model
	}
	thinkChanged := ld.think != nil && *ld.think != a.thinking
	if thinkChanged {
		a.thinking = *ld.think
	}
	ev, _ := a.meterEvent()
	a.traceLocked("load", map[string]any{"messages": len(a.history), "repaired": repaired, "seq": a.seq, "model": a.model})
	a.mu.Unlock()
	a.emit(ev)
	if changed {
		a.emit(Event{Kind: EvModel, Text: ld.model})
	}
	if thinkChanged {
		a.emit(thinkEvent(*ld.think))
	}
	a.sync()
	a.mu.Lock()
	a.queue = a.queue[1:] // only now: Busy and Wait cover the Store and Trace calls of the Load
	a.idle.Broadcast()
	a.mu.Unlock()
}

// applyLoadLocked installs the history and returns the number of repaired calls. Caller holds mu.
func (a *Agent) applyLoadLocked(ld *loadItem) int {
	h := ld.history
	seq := ld.maxSeq
	for _, m := range h {
		seq = max(seq, m.Seq)
	}
	for i := range h {
		if h[i].Seq == 0 {
			seq++
			h[i].Seq = seq
		}
	}
	a.callSeq = 0
	answered := map[string]bool{}
	for _, m := range h {
		if m.Role == chat.RoleTool {
			answered[m.ToolCallID] = true
		}
		for _, id := range append([]string{m.ToolCallID}, callIDs(m.ToolCalls)...) {
			if sub := callIDRe.FindStringSubmatch(id); sub != nil {
				n, _ := strconv.Atoi(sub[1])
				a.callSeq = max(a.callSeq, n)
			}
		}
	}
	var rep []chat.Message
	for _, m := range h {
		for _, c := range m.ToolCalls {
			if answered[c.ID] {
				continue
			}
			answered[c.ID] = true
			seq++
			rep = append(rep, chat.Message{Role: chat.RoleTool, Content: resCancelled, Brief: resCancelled,
				ToolCallID: c.ID, Name: c.Name, Seq: seq})
		}
	}
	if a.deps.Store != nil { // a Load may come with another SessionFile: try the Store again
		a.out = append(a.out, outOp{rearm: true})
	}
	a.history = append(h, rep...)
	a.seq = seq
	a.meter.reset()
	a.batch, a.lastReqSeq, a.emergency = nil, 0, false
	if len(rep) > 0 {
		a.storeLocked(func(s Store) error { return s.Repair(rep) })
	}
	return len(rep)
}

func callIDs(cs []chat.ToolCall) (out []string) {
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}
