package agent

import (
	"context"
	"fmt"
	"strings"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Turn: a loop of model steps and tool batches
// ---------------------------------------------------------------------------

// busyWarning is shown when the server check fails after an Abort.
const busyWarning = "model server busy or not responding; if it stays so: llm stop && llm start qwen"

// loopWarning is shown when the loop guard cut an answer.
const loopWarning = "stopped: the answer started repeating itself"

// A Step that ended with finish "tool_calls" but no call: the server could not parse the
// model's tool call. The model gets one nudge per loop; a second such Step ends the loop.
const (
	unparsedWarning = "the model's tool call could not be parsed; asking it to call again"
	unparsedStop    = "stopped: the model's tool call could not be parsed"
	unparsedNudge   = "Your last tool call could not be parsed. Call the tool again with valid JSON arguments."

	undeliveredWarning = "the model's output was not delivered (probably a tool call that was too long or unparsable); asking it again"
	undeliveredStop    = "stopped: the model's output was not delivered"
	undeliveredNudge   = "Your last answer hit the output limit and nothing arrived (probably a tool call that was too long). Try again and keep tool arguments short: write big files in several smaller steps."
)

// maxDenials is the number of denied calls in one Turn that ends the Turn.
const maxDenials = 2

// Status words of a finished loop (the first line of a TaskResult, R-38).
const (
	statusDone        = "done"
	statusStepLimit   = "stopped: step limit"
	statusDenied      = "stopped: tool calls were denied"
	statusInterrupted = "interrupted"
	statusAborted     = "aborted"
	statusFailed      = "failed: "
	statusCut         = "stopped: answer cut off" // the token limit cut the report (FinishLength)
)

// turnResult is what the worker learned while reading one model step.
type turnResult struct {
	text      strings.Builder // delivered before any interrupt
	reasoning strings.Builder // the model's reasoning (sent back as reasoning_content, R-42)
	finish    chat.FinishReason
	err       error
	draining  bool
	aborted   bool
	raw       []chat.ToolCall // calls of EventDone, before normalization
	calls     []pendingCall   // valid only for a step that completed normally
	note      string          // extra warning emitted after TurnEnded
	guard     loopGuard       // watches the streamed text for repetition
	looped    bool            // the guard cancelled the request
	clock     stepClock       // times of the stream, for the Generation speed
	status    string          // how the loop ended (statusDone, ...); set by runLoop
	steps     int             // Steps the loop took
}

// runTurn runs one Turn of the Session: steps until the model answers without tool calls, the
// user interrupts, an error occurs or MaxSteps is reached. Queued user texts join the history
// only between a tool batch and the next request. A /task item first runs the goal as a Task.
func (a *Agent) runTurn(ctx context.Context, it qitem) {
	a.emit(Event{Kind: EvTurnStarted, Text: it.text})
	system := a.deps.SystemPrompt()
	c := &a.conversation
	toolsCtx, toolsCancel := context.WithCancel(ctx)
	defer toolsCancel()
	a.mu.Lock()
	c.ctx, c.toolsCtx, c.toolsCancel, c.system = ctx, toolsCtx, toolsCancel, system
	a.chain = append(a.chain[:0], c)
	if a.interrupted { // an Interrupt that came before the cancel func existed
		toolsCancel()
	}
	meter := a.meterSnapshot(c) // the panel shows the context use from the start, not only at the end
	a.mu.Unlock()
	a.emit(meter)

	var repeats repeatGuard
	var r *turnResult
	var msg chat.Message
	if it.task {
		a.runGoal(c, it.text, &repeats)
	}
	if it.task && a.isInterrupted() { // the Esc ended the goal's Task: no Session request (R-11)
		r = &turnResult{finish: chat.FinishInterrupted, status: statusInterrupted}
		if a.wasAborted() {
			r.status = statusAborted
		}
	} else {
		r, msg = a.runLoop(c, &repeats)
	}
	a.mu.Lock()
	a.chain = a.chain[:0]
	a.mu.Unlock()
	a.endTurn(r, msg)
	if (a.wasAborted() || r.looped) && a.deps.Health != nil {
		go a.checkHealth(ctx)
	}
}

// runLoop is the Step loop on one conversation: the Session's or a Task's. The repeat guard and
// the denial count are per loop; the Step limit is the Turn's for the Session and the Task's
// otherwise. Queued user texts join the Session only (A3): a Task's context stays clean.
func (a *Agent) runLoop(c *conversation, repeats *repeatGuard) (*turnResult, chat.Message) {
	var r *turnResult
	var msg chat.Message
	denials, nudged := 0, false
	maxSteps := a.deps.MaxSteps
	if c.depth > 0 {
		maxSteps = a.deps.Tasks.MaxSteps
	}
	for step := 1; ; step++ {
		a.emergencyBatch(c)
		r = a.stream(c, step)
		r.steps = step
		if msg = a.commit(c, r); len(r.calls) == 0 {
			if w, nudge, stop := lostCall(r); w != "" { // F10, F11: nothing usable arrived
				if !nudged && step < maxSteps {
					nudged = true
					a.nudge(c, w, nudge)
					continue
				}
				r.note = stop
			}
			r.status = statusDone
			switch {
			case r.finish == chat.FinishError:
				r.status = statusFailed + errText(r.err)
			case r.aborted:
				r.status = statusAborted
			case r.finish == chat.FinishInterrupted:
				r.status = statusInterrupted
			case r.finish == chat.FinishLength:
				r.status = statusCut
			case r.note != "":
				r.status = r.note
			}
			break
		}
		d, stuck := a.runCalls(c, r.calls, repeats)
		if a.isInterrupted() || c.ctx.Err() != nil {
			r.finish, r.status = chat.FinishInterrupted, statusInterrupted
			if a.wasAborted() {
				r.status = statusAborted
			}
			break
		}
		if denials += d; d > 0 && denials >= maxDenials {
			r.finish, r.note, r.status = chat.FinishStop, statusDenied, statusDenied
			break
		}
		if stuck {
			r.finish, r.note = chat.FinishStop, repeats.warning()
			r.status = r.note
			break
		}
		if step >= maxSteps {
			r.finish, r.note, r.status = chat.FinishStop, fmt.Sprintf("step limit reached (%d)", step), statusStepLimit
			break
		}
		if c.depth == 0 {
			a.inject()
		}
	}
	return r, msg
}

// lostCall reports a Step whose output never became usable: finish "tool_calls" without a call
// (the server could not parse it, F10), or finish "length" with no text, no reasoning and no
// call (the server held a too long or endless tool call back until max_tokens, F11). It returns
// the warning, the nudge for the model and the stop note; all "" for a normal Step.
func lostCall(r *turnResult) (warning, nudge, stop string) {
	switch {
	case r.finish == chat.FinishToolCalls:
		return unparsedWarning, unparsedNudge, unparsedStop
	case r.finish == chat.FinishLength && !r.looped && r.text.Len() == 0 && r.reasoning.Len() == 0:
		return undeliveredWarning, undeliveredNudge, undeliveredStop
	}
	return "", "", ""
}

// nudge shows warning and asks the model, in the conversation, to try again.
func (a *Agent) nudge(c *conversation, warning, text string) {
	a.emit(Event{Kind: EvWarning, Text: warning})
	a.mu.Lock()
	a.appendLocked(c, chat.Message{Role: chat.RoleUser, Content: text})
	a.mu.Unlock()
	a.sync()
}

// errText is the error's text, "" for nil.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// stream performs one model request of c and reads it to its end (or to an Abort). The request
// derives from the Turn's root ctx (R-2, R-11: a soft interrupt drains, never disconnects) and
// names the limit of the model in use (I-13).
func (a *Agent) stream(c *conversation, step int) *turnResult {
	reqCtx, reqCancel := context.WithCancel(c.ctx)
	defer reqCancel()
	a.mu.Lock()
	c.meter.limit = a.limitFor(a.model)
	req := a.request(c)
	a.traceRequestLocked(c, step, req)
	c.reqCancel = reqCancel
	if a.aborted {
		reqCancel()
	}
	a.mu.Unlock()
	a.sync()

	r := &turnResult{finish: chat.FinishError, err: errStreamClosed}
	r.clock.start = a.deps.Now()
	if ch, err := a.deps.Provider.Stream(reqCtx, req); err != nil {
		r.err = err
	} else {
		a.consume(c, reqCtx, ch, r)
	}
	a.mu.Lock()
	c.reqCancel, r.aborted = nil, a.aborted
	a.mu.Unlock()
	a.reportSpeed(c, r)
	a.prepare(r)
	return r
}

// request builds the Request of c; consecutive user messages are merged. Reasoning goes out for
// the assistant Messages of the running Turn only (R-42): earlier Turns are re-rendered by the
// chat template at a new user Message anyway, so sending theirs would only cost context. The
// `task` Tool is offered below MaxDepth. Caller holds mu.
func (a *Agent) request(c *conversation) chat.Request {
	msgs := []chat.Message{{Role: chat.RoleSystem, Content: c.system}}
	turn := turnStartIndex(c.history)
	for i, m := range c.history {
		if last := len(msgs) - 1; m.Role == chat.RoleUser && msgs[last].Role == chat.RoleUser {
			msgs[last].Content += "\n\n" + m.Content
			continue
		}
		if m.Role == chat.RoleTool && m.Stub != "" {
			m.Content = m.Stub // the copy goes out; Stub stays set so the estimate counts it as a stub
		}
		if i < turn {
			m.Reasoning = ""
		}
		msgs = append(msgs, m)
	}
	return chat.Request{Model: a.model, Messages: msgs, MaxTokens: a.deps.MaxTokens, Extra: a.deps.Extra, Tools: a.specs(c)}
}

// consume reads the stream to its end, even after an interrupt (drain); a
// cancelled request context (Abort, shutdown) ends it at once.
func (a *Agent) consume(c *conversation, ctx context.Context, ch <-chan chat.StreamEvent, r *turnResult) {
	for {
		select {
		case <-ctx.Done():
			r.err = ctx.Err()
			return
		case <-a.intr:
			a.startDrain(c, r)
		case ev, ok := <-ch:
			if !ok || a.handle(c, r, ev) {
				return
			}
		}
	}
}

// handle processes one stream event; it reports whether the stream is done.
func (a *Agent) handle(c *conversation, r *turnResult, ev chat.StreamEvent) bool {
	r.clock.note(ev, a.deps.Now())
	switch ev.Kind {
	case chat.EventReasoning:
		if !r.draining && !a.isInterrupted() {
			r.reasoning.WriteString(ev.Text)
		}
	case chat.EventDelta:
		if r.draining || a.isInterrupted() {
			a.startDrain(c, r)
			return false
		}
		r.text.WriteString(ev.Text)
		if cut, ok := r.guard.feed(ev.Text); ok {
			// Same path as Abort for the request: returning ends consume and stream's deferred
			// reqCancel disconnects. The answer is stored cut before the repetition.
			r.text.Reset()
			r.text.WriteString(cut)
			// Stop, not Length: the guard said why; "cut off by a limit" would be wrong. The
			// stored Message is still marked truncated (commitLocked).
			r.looped, r.finish, r.err, r.note = true, chat.FinishStop, nil, loopWarning
			a.traceC(c, "loop_guard", map[string]any{"kept_bytes": len(cut)})
			return true
		}
		a.emit(c.tag(Event{Kind: EvDelta, Text: ev.Text}))
	case chat.EventProgress:
		if !r.draining {
			a.emit(c.tag(Event{Kind: EvProgress, Done: ev.Done, Total: ev.Total}))
		}
	case chat.EventUsage:
		a.mu.Lock()
		c.meter.setUsage(ev.Usage)
		a.mu.Unlock()
		a.traceC(c, "usage", map[string]any{"prompt": ev.Usage.PromptTokens, "completion": ev.Usage.CompletionTokens, "cached": ev.Usage.CachedTokens})
	case chat.EventDone:
		r.finish, r.err, r.raw = ev.Finish, ev.Err, ev.ToolCalls
		return true
	}
	return false
}

// startDrain announces the drain once; later deltas are discarded.
func (a *Agent) startDrain(c *conversation, r *turnResult) {
	if !r.draining {
		r.draining = true
		a.emit(c.tag(Event{Kind: EvDraining}))
	}
}

func (a *Agent) isInterrupted() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.interrupted
}

func (a *Agent) wasAborted() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.aborted
}

// ---------------------------------------------------------------------------
// Step boundaries
// ---------------------------------------------------------------------------

// commit stores the assistant message of a step and returns it (zero if none).
// An interrupted or failed step never keeps tool calls.
func (a *Agent) commit(c *conversation, r *turnResult) chat.Message {
	if r.draining || r.aborted {
		r.finish, r.err = chat.FinishInterrupted, nil
	}
	a.traceReply(c, r)
	a.mu.Lock()
	msg := a.commitLocked(c, r)
	a.mu.Unlock()
	a.sync()
	return msg
}

// commitLocked appends the assistant message of the step, if there is one. Caller holds mu.
func (a *Agent) commitLocked(c *conversation, r *turnResult) chat.Message {
	text := r.text.String()
	if r.finish == chat.FinishError || (text == "" && len(r.calls) == 0) {
		return chat.Message{}
	}
	msg := chat.Message{
		Role: chat.RoleAssistant, Content: text, Reasoning: r.reasoning.String(),
		Truncated:   r.finish == chat.FinishLength || r.looped,
		Interrupted: r.finish == chat.FinishInterrupted,
		ToolCalls:   storedCalls(r.calls),
	}
	a.appendLocked(c, msg)
	return msg
}

// inject moves the queued user texts into the Session's history in one critical section
// and reports them; the queue is empty afterwards.
func (a *Agent) inject() {
	var meter Event
	var changed bool
	a.mu.Lock()
	n := a.leadingTextsLocked()
	var texts []string
	for _, it := range a.queue[:n] {
		texts = append(texts, it.text)
	}
	a.queue = a.queue[n:]
	for _, t := range texts {
		a.appendLocked(&a.conversation, chat.Message{Role: chat.RoleUser, Content: t, Injected: true})
	}
	if len(texts) > 0 && a.stubInjectLocked() {
		meter = a.meterSnapshot(&a.conversation)
		changed = true
	}
	a.mu.Unlock()
	a.sync()
	if changed {
		a.emit(meter)
	}
	for _, t := range texts {
		a.emit(Event{Kind: EvUserText, Text: t})
	}
	if len(texts) > 0 {
		a.emit(Event{Kind: EvQueue, Queued: 0})
	}
}

// emergencyBatch runs the once-per-Turn emergency stub batch of c when the next request is too big.
func (a *Agent) emergencyBatch(c *conversation) {
	a.mu.Lock()
	c.meter.limit = a.limitFor(a.model) // a /model switch mid-Task reaches the Task's next Step (I-13)
	changed := a.stubEmergencyLocked(c)
	var ev Event
	if changed {
		ev = a.meterSnapshot(c)
	}
	a.mu.Unlock()
	a.sync()
	if changed {
		a.emit(ev)
	}
}

// queued reports whether user text waits to join the running Turn (texts behind a Load do not).
func (a *Agent) queued() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.leadingTextsLocked() > 0
}

// checkHealth probes the server after an Abort; it runs in its own goroutine.
func (a *Agent) checkHealth(ctx context.Context) {
	if a.deps.Health(ctx) != nil {
		a.emit(Event{Kind: EvWarning, Text: busyWarning})
	}
}

// endTurn emits warnings, the meter and, LAST, TurnEnded: consumers that stop
// reading at TurnEnded (plain mode) still see everything of the turn.
func (a *Agent) endTurn(r *turnResult, msg chat.Message) {
	var evs []Event
	if r.text.Len() == 0 && r.finish == chat.FinishStop && len(r.calls) == 0 {
		evs = append(evs, Event{Kind: EvWarning, Text: "empty answer"})
	}
	if r.note != "" {
		evs = append(evs, Event{Kind: EvWarning, Text: r.note})
	}
	if r.err != nil {
		a.trace("error", map[string]any{"text": r.err.Error(), "finish": string(r.finish)})
	}
	a.mu.Lock()
	a.finishTurnLocked()
	a.storeLocked(func(s Store) error { return s.Flush() })
	meterEv, warns := a.meterEvent()
	a.mu.Unlock()

	for _, w := range a.drain() { // before TurnEnded, which stays last
		evs = append(evs, Event{Kind: EvWarning, Text: w})
	}
	evs = append(evs, meterEv)
	for _, w := range warns {
		evs = append(evs, Event{Kind: EvWarning, Text: w})
	}
	evs = append(evs, Event{Kind: EvTurnEnded, Message: msg, Finish: r.finish, Err: r.err})
	for _, ev := range evs {
		a.emit(ev)
	}
}
