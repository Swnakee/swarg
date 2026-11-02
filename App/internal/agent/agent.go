package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ErrBusy is returned by Clear while a turn is running.
var ErrBusy = errors.New("agent: a turn is running")

var errStreamClosed = errors.New("agent: stream closed without a final event")

// Deps are the injected dependencies of an Agent.
type Deps struct {
	Source       string // the name of the first Source (the config's provider name); "" is fine
	Provider     Provider
	SystemPrompt func() string // required; called at the start of every turn
	Model        string
	MaxTokens    int
	ContextLimit int
	Extra        map[string]any
	ThinkExtra   map[string]any // laid over Extra while thinking is on (R-60); nil = thinking changes nothing in the body
	ThinkTokens  int            // MaxTokens while thinking is on; 0 = MaxTokens
	OnEvent      func(Event)    // nil-safe; called from the worker, the caller, or a running tool via Progress (never after the tool returned); never under a lock

	Tools    ToolRunner                                          // nil = no tools: one step, no Tools in the request
	Approve  func(ctx context.Context, c chat.ToolCall) Approval // nil = allow; ctx is cancelled by Interrupt and Abort
	Health   func(ctx context.Context) error                     // nil = skip the server check after an Abort
	MaxSteps int                                                 // model requests per Turn; 0 = 25

	Store Store // nil = history is not saved; see Store
	Trace Trace // nil = no debug trace; see Trace

	AutoCompact int // percent of ContextLimit at which a Turn end starts a Compaction; 0 = off

	LimitFor func(model string) int // nil = ContextLimit for every model; else the ContextLimit of a model id (R-41)
	Tasks    TaskLimits             // Task mode limits; MaxDepth 0 = no Tasks (R-39)
	Agents   AgentDeps              // sub-agents (R-120..R-127); the zero value = none

	WarnUSD float64 // one warning when the Session spend first reaches this many USD (R-114); 0 = off. Never stops a Turn.

	Now func() time.Time // nil = time.Now; times the Generation speed of every Step
}

// defaultMaxSteps is the step limit when Deps.MaxSteps is 0.
const defaultMaxSteps = 25

// Agent runs queued user messages one at a time against a Provider.
//
// One worker goroutine owns the turn; mu guards queue, running, interrupted,
// aborted, stopped, the embedded conversation (the Session: history, system, meter, cancel
// funcs, detach) and the Chain. OnEvent is never called under mu. Start/End tool events come
// one at a time from the goroutine that runs the loop (the worker, or the `task` call's goroutine
// for a Task, while the worker waits); ToolOutput comes from the tool goroutine while its Run is
// active only.
type Agent struct {
	deps Deps
	wake chan struct{} // Submit -> worker (cap 1)
	intr chan struct{} // Interrupt -> worker (cap 1)

	conversation // the Session (depth 0); a.history, a.meter, ... are its fields

	mu          sync.Mutex
	idle        *sync.Cond // broadcast when a turn ends or the worker stops
	queue       []qitem
	running     bool
	interrupted bool // first Interrupt or Abort of the running turn
	aborted     bool // Abort (or a second Interrupt) of the running turn
	stopped     bool
	thinking    bool                     // the thinking mode (R-60)
	spend       spendState               // the Session's cost so far (R-111)
	src         Source                   // the model server in use and its presets (R-100)
	model       string                   // the Model choice: the model id of the next request (R-30)
	chain       []*conversation          // the running Chain: the Session, then the Tasks inside it; empty when idle
	taskSeq     int                      // Task ids handed out
	turnTasks   int                      // Tasks and agents started in the running Turn (R-39 cap)
	agents      map[string]*conversation // the running sub-agents by id; they are not on the Chain (R-122)
	agentSeq    int                      // agent ids handed out
	slots       *slots                   // agent slots, overall and per Source (R-122)
	approving   chan struct{}            // one agent approval prompt at a time (R-125)
	paths       pathLocks                // files being written right now (R-124)
	callSeq     int                      // session-wide counter of generated call ids; the Turn's goroutines take turns
	seq         int                      // last chat.Message.Seq handed out; guarded by mu, never reset

	compacting     bool               // a Compaction runs (the worker is not in a Turn); guarded by mu
	compactStop    bool               // Esc during the Compaction: no further span is replaced; guarded by mu
	compactAborted bool               // the summary request was disconnected; guarded by mu
	compactCancel  context.CancelFunc // disconnects the summary request in flight; guarded by mu
	autoFails      int                // consecutive failed or no-gain automatic Compactions; guarded by mu
	autoOff        bool               // the circuit breaker switched automatic Compaction off; guarded by mu

	out         []outOp    // Store calls and Trace records not performed yet, in order; guarded by mu
	storeMu     sync.Mutex // serializes drain, so the queue order is the call order
	storeOff    bool       // a Store call failed; guarded by storeMu
	traceWarned bool       // guarded by storeMu
}

// qitem is one queue entry: a user text (or a /task goal), or a control item (Load, Compaction)
// that runs between Turns.
type qitem struct {
	text    string
	task    bool   // the text is the goal of a /task Turn; it never joins a running Turn
	skill   string // a /skill Turn: the Skill applied before the text is answered (R-83)
	load    *loadItem
	compact *compactItem
}

// control reports whether the entry is a control item rather than a user text.
func (q qitem) control() bool { return q.load != nil || q.compact != nil }

// New creates an Agent; call Start to run it.
func New(deps Deps) *Agent {
	if deps.MaxSteps <= 0 {
		deps.MaxSteps = defaultMaxSteps
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	deps.Tasks = deps.Tasks.withDefaults()
	a := &Agent{
		deps:  deps,
		model: deps.Model,
		src: Source{Name: deps.Source, Provider: deps.Provider, Model: deps.Model, MaxTokens: deps.MaxTokens,
			ContextLimit: deps.ContextLimit, Extra: deps.Extra, ThinkExtra: deps.ThinkExtra, ThinkTokens: deps.ThinkTokens,
			LimitFor: deps.LimitFor, Health: deps.Health},
		wake: make(chan struct{}, 1),
		intr: make(chan struct{}, 1),

		slots:     newSlots(deps.Agents.Max),
		approving: make(chan struct{}, 1),
	}
	a.meter = meter{limit: a.limitFor(deps.Model)}
	a.idle = sync.NewCond(&a.mu)
	return a
}

// limitFor is the ContextLimit of a model id on the Source in use: its LimitFor, else its
// ContextLimit. Caller holds mu (or the Agent is not started yet).
func (a *Agent) limitFor(model string) int { return srcLimit(a.src, model) }

// limitOf is the ContextLimit a request of c is measured against: its Source's and model's.
// Caller holds mu.
func (a *Agent) limitOf(c *conversation) int {
	s, m := a.srcOf(c)
	return srcLimit(s, m)
}

// limit is the Session's current ContextLimit (locks).
func (a *Agent) limit() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.meter.limit
}

// setLimitLocked switches the Session meter to the limit of model when it differs: the warnings
// are re-armed and the exact value forgotten (the next usage report re-measures). It reports a
// change. Caller holds mu.
func (a *Agent) setLimitLocked(model string) bool {
	l := a.limitFor(model)
	if l == a.meter.limit {
		return false
	}
	a.meter.limit, a.meter.warned80, a.meter.warned100, a.meter.hasExact = l, false, false, false
	return true
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// Start launches the worker and returns at once. Cancelling ctx stops it.
func (a *Agent) Start(ctx context.Context) {
	go a.run(ctx)
}

// Submit queues a user message; blank text is ignored. It never interrupts.
func (a *Agent) Submit(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	a.enqueueText(qitem{text: text})
}

// enqueueText queues a user text item and announces the new queue length.
func (a *Agent) enqueueText(it qitem) {
	a.mu.Lock()
	a.queue = append(a.queue, it)
	n := a.queuedTextsLocked()
	a.mu.Unlock()
	a.emit(Event{Kind: EvQueue, Queued: n})
	signal(a.wake)
}

// Interrupt is the first Esc: it stops the running turn's output (the stream is
// drained, running tools are cancelled). While already interrupted it behaves as
// Abort. During a Compaction the first Esc stops it after the summary request in flight
// was read to its end, the second disconnects that request. A no-op when idle. The queue is kept.
func (a *Agent) Interrupt() {
	a.mu.Lock()
	if !a.running {
		cancel := a.stopCompactLocked(false)
		compacting := a.compacting
		a.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if compacting {
			a.trace("interrupt", map[string]any{})
		}
		return
	}
	if a.interrupted {
		a.mu.Unlock()
		a.Abort()
		return
	}
	a.interrupted = true
	cancels := a.toolCancelsLocked()
	signal(a.intr)
	a.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	a.trace("interrupt", map[string]any{})
}

// toolCancelsLocked lists the tool cancel funcs of the whole Chain, innermost first. Caller holds mu.
func (a *Agent) toolCancelsLocked() (out []context.CancelFunc) {
	for i := len(a.chain) - 1; i >= 0; i-- {
		if a.chain[i].toolsCancel != nil {
			out = append(out, a.chain[i].toolsCancel)
		}
	}
	for _, c := range a.agents {
		out = append(out, c.toolsCancel)
	}
	return out
}

// Abort ends the running turn at once: the request is disconnected (no drain)
// and tools are killed; the server is checked afterwards (Deps.Health). A no-op when idle.
func (a *Agent) Abort() {
	a.mu.Lock()
	if !a.running {
		cancel := a.stopCompactLocked(true)
		compacting := a.compacting
		a.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if compacting {
			a.trace("abort", map[string]any{})
		}
		return
	}
	a.interrupted, a.aborted = true, true
	cancels := a.toolCancelsLocked()
	if len(a.chain) > 0 && a.inner().reqCancel != nil { // only the innermost has a request in flight
		cancels = append(cancels, a.inner().reqCancel)
	}
	for _, c := range a.agents { // every running agent may have a request in flight
		if c.reqCancel != nil {
			cancels = append(cancels, c.reqCancel)
		}
	}
	a.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	a.trace("abort", map[string]any{})
}

// Clear empties the history and resets the meter; ErrBusy while a turn or a Compaction runs.
func (a *Agent) Clear() error {
	system := a.deps.SystemPrompt()
	a.mu.Lock()
	if a.running || a.compacting {
		a.mu.Unlock()
		return ErrBusy
	}
	a.history = nil
	a.system = system
	a.meter.reset()
	a.batch, a.lastReqSeq = nil, 0
	a.storeLocked(func(s Store) error { return s.Clear() })
	a.traceLocked("clear", map[string]any{})
	ev, _ := a.meterEvent()
	a.mu.Unlock()
	a.emit(ev)
	a.sync()
	return nil
}

// History returns a copy of the conversation.
func (a *Agent) History() []chat.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]chat.Message(nil), a.history...)
}

// Busy reports whether a turn runs or messages are queued.
func (a *Agent) Busy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running || len(a.queue) > 0
}

// Wait blocks until the queue is empty and no turn runs (or the worker stopped).
func (a *Agent) Wait() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for (a.running || len(a.queue) > 0) && !a.stopped {
		a.idle.Wait()
	}
}

// ---------------------------------------------------------------------------
// Worker
// ---------------------------------------------------------------------------

// run is the worker loop: dequeue, run the turn, mark idle.
func (a *Agent) run(ctx context.Context) {
	defer a.stop()
	for {
		it, n, ok := a.dequeue(ctx)
		if !ok {
			return
		}
		a.emit(Event{Kind: EvQueue, Queued: n})
		a.runTurn(ctx, it)
		a.mu.Lock()
		a.running = false
		if ctx.Err() == nil {
			a.enqueueAutoLocked() // before idle is announced, so Wait covers it
		}
		a.idle.Broadcast()
		a.mu.Unlock()
	}
}

// stop marks the worker gone so Wait cannot hang.
func (a *Agent) stop() {
	a.mu.Lock()
	a.stopped = true
	a.idle.Broadcast()
	a.mu.Unlock()
}

// dequeue blocks for the next message; it starts the turn in one critical section.
func (a *Agent) dequeue(ctx context.Context) (it qitem, left int, ok bool) {
	for {
		if ctx.Err() != nil { // shutting down: queued items are dropped, not run
			return qitem{}, 0, false
		}
		a.mu.Lock()
		if len(a.queue) > 0 && a.queue[0].control() {
			it := a.queue[0]
			a.mu.Unlock()
			if it.load != nil {
				a.runLoad(it.load)
			} else {
				a.runCompact(ctx, it.compact)
			}
			continue
		}
		if len(a.queue) > 0 {
			it, a.queue = a.queue[0], a.queue[1:]
			a.running, a.interrupted, a.aborted = true, false, false
			select { // drop a stale interrupt signal
			case <-a.intr:
			default:
			}
			a.emergency, a.turnTasks = false, 0
			a.appendLocked(&a.conversation, chat.Message{Role: chat.RoleUser, Content: it.text, TurnStart: true})
			left = a.queuedTextsLocked()
			a.mu.Unlock()
			a.sync()
			return it, left, true
		}
		a.mu.Unlock()
		select {
		case <-a.wake:
		case <-ctx.Done():
			return qitem{}, 0, false
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// appendLocked adds a message to the history of c and gives it the next Seq. Only the Session's
// Messages reach the Store (I-14): a Task's live in memory and in the Trace. Caller holds mu.
func (a *Agent) appendLocked(c *conversation, m chat.Message) {
	a.seq++
	m.Seq = a.seq
	c.history = append(c.history, m)
	if c.depth == 0 {
		a.storeLocked(func(s Store) error { return s.Message(m) })
	}
}

// queuedTextsLocked counts the user texts in the queue (control items do not count). Caller holds mu.
func (a *Agent) queuedTextsLocked() (n int) {
	for _, it := range a.queue {
		if !it.control() {
			n++
		}
	}
	return n
}

// leadingTextsLocked counts the texts before the first control item or /task goal: those may
// join a running Turn, the rest waits (a goal always starts its own Turn, R-40). Caller holds mu.
func (a *Agent) leadingTextsLocked() int {
	for i, it := range a.queue {
		if it.control() || it.task || it.skill != "" {
			return i
		}
	}
	return len(a.queue)
}

// meterSnapshot builds the Meter event of c without touching the warnings. Caller holds mu.
func (a *Agent) meterSnapshot(c *conversation) Event {
	bd := estimateRequest(a.request(c))
	used, est := c.meter.value(bd.Total())
	return c.tag(Event{Kind: EvMeter, Used: used, Limit: c.meter.limit, Estimated: est,
		CachePct: c.meter.cachePct, HasCache: c.meter.hasCache, Breakdown: bd})
}

// meterEvent computes the Session's Meter event and the new warnings. Caller holds mu.
func (a *Agent) meterEvent() (Event, []string) {
	ev := a.meterSnapshot(&a.conversation)
	return ev, a.meter.check(ev.Used, ev.Estimated)
}

// emit delivers an event; never call it while holding mu. The events of an agent's loop are
// reduced to progress lines first.
func (a *Agent) emit(ev Event) {
	if isAgentID(ev.Task) {
		a.agentEvent(ev)
		return
	}
	if ev.Kind == EvWarning && a.deps.Trace != nil {
		a.trace("warning", map[string]any{"text": ev.Text})
	}
	if a.deps.OnEvent != nil {
		a.deps.OnEvent(ev)
	}
}

// signal does a non-blocking send on a cap-1 channel.
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
