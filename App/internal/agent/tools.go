package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ToolRunner is the agent's view of the tool set; main adapts the tools package.
type ToolRunner interface {
	Specs() []chat.ToolSpec
	// Kind reports whether the tool exists, whether it only reads, and whether
	// a call needs the user's approval.
	Kind(name string) (known, readOnly, confirm bool)
	// Detachable reports whether a running call of the tool can move to the
	// background (hooks.Detach is then closed by Agent.Background).
	Detachable(name string) bool
	// Label is a one-line description of a call; it must tolerate bad JSON.
	Label(call chat.ToolCall) string
	// Run executes a call; err is a tool or validation error shown to the model.
	// hooks.Progress may be called from any goroutine while Run is active (not
	// after it returned); hooks.Detach is nil unless the tool is Detachable.
	Run(ctx context.Context, call chat.ToolCall, hooks ToolHooks) (output, brief string, err error)
}

// ToolHooks connect a running call to the agent.
type ToolHooks struct {
	Progress func(chunk string) // display only: becomes EvToolOutput{Call, Text}
	Detach   <-chan struct{}    // closed by Agent.Background; nil for a non-detachable tool
}

// progressSink delivers the Progress chunks of one call. Its mutex orders the
// emits of the call and makes close() a barrier: no emit starts after it.
type progressSink struct {
	mu     sync.Mutex
	closed bool
	emit   func(chunk string)
}

func (s *progressSink) send(chunk string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.emit(chunk)
	}
}

func (s *progressSink) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// Background moves the running detachable call to the background by closing its
// detach channel. It reports whether a call was detached: false when no turn
// runs, no such call runs (or it still awaits approval) or it was detached already.
func (a *Agent) Background() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running || len(a.chain) == 0 || a.inner().detach == nil {
		return false
	}
	c := a.inner()
	close(c.detach)
	c.detach = nil
	return true
}

// setDetach registers (ch) or clears (nil) the detach channel of the running call of c.
func (a *Agent) setDetach(c *conversation, ch chan struct{}) {
	a.mu.Lock()
	c.detach = ch
	a.mu.Unlock()
}

// Approval is the user's answer to a confirm-needing call.
type Approval struct {
	Allow bool
	Note  string // optional reason shown to the model on deny
}

// Results the model sees when a call did not run.
const (
	resSkipped   = "skipped: the user sent a new message"
	resCancelled = "cancelled by the user"
	resDenied    = "denied by the user"
	denyAdvice   = "Do not retry this or a similar action; ask the user how to proceed."
	taskAdvice   = "Do not retry this or a similar action; report it in your result." // inside a Task (R-9)
	errCut       = "arguments cut by the token limit"
)

// denyNote is the sentence after a denial: a Task cannot ask the user.
func (a *Agent) denyNote(c *conversation) string {
	if c.depth > 0 {
		return taskAdvice
	}
	return denyAdvice
}

// pendingCall is a model tool call ready to run: call has normalized args and a
// unique id; raw is what the model sent (display only); bad is an argument error.
// synthetic marks the `task` call of a /task Turn (made by the agent, not the model).
type pendingCall struct {
	call      chat.ToolCall
	raw       string
	bad       string
	synthetic bool
}

// result is the outcome of one call; output is what the model sees.
type result struct {
	output, brief string
	label         string // one-line description of the call, set by runBatch
	err           error
	denied        bool  // the user refused the call
	ms            int64 // run time, for the trace
}

// ---------------------------------------------------------------------------
// Normalizing the calls of a step
// ---------------------------------------------------------------------------

// prepare turns the raw calls of a completed step into pendingCalls. Arguments
// are normalized because the server parses every stored value on each request.
func (a *Agent) prepare(r *turnResult) {
	if a.deps.Tools == nil || r.draining || r.aborted || r.finish == chat.FinishError {
		return
	}
	seen := map[string]bool{}
	for _, c := range r.raw {
		p := pendingCall{raw: c.Args}
		c.Args, p.bad = normalizeArgs(c.Args)
		if r.finish == chat.FinishLength || r.looped { // the request was cut, so may be its calls
			p.bad = errCut
		}
		for c.ID == "" || seen[c.ID] {
			a.callSeq++
			c.ID = fmt.Sprintf("call_%d", a.callSeq)
		}
		seen[c.ID] = true
		p.call = c
		r.calls = append(r.calls, p)
	}
}

// normalizeArgs returns valid JSON-object arguments: blank or null become {},
// a JSON string holding the object is unwrapped once; anything else is {} plus a reason.
func normalizeArgs(raw string) (args, bad string) {
	s := strings.TrimSpace(raw)
	for pass := 0; pass < 2; pass++ {
		if s == "" || s == "null" {
			return "{}", ""
		}
		var v any
		if json.Unmarshal([]byte(s), &v) != nil {
			return "{}", "arguments are not valid JSON"
		}
		switch x := v.(type) {
		case map[string]any:
			return s, ""
		case string:
			s = strings.TrimSpace(x)
		default:
			return "{}", "arguments must be a JSON object"
		}
	}
	return "{}", "arguments must be a JSON object"
}

// storedCalls returns the calls as stored in the history.
func storedCalls(ps []pendingCall) []chat.ToolCall {
	var out []chat.ToolCall
	for _, p := range ps {
		out = append(out, p.call)
	}
	return out
}

// ---------------------------------------------------------------------------
// Running a step's calls
// ---------------------------------------------------------------------------

// runCalls runs the calls of c by schedule (consecutive read-only calls in parallel,
// every other call alone, in order) and appends one tool message per call.
// The repeat guard g may add a note to a ToolResult; stop reports that the loop must end.
func (a *Agent) runCalls(c *conversation, calls []pendingCall, g *repeatGuard) (denied int, stop bool) {
	res := make([]result, len(calls))
	for i := 0; i < len(calls); {
		j := i + 1
		if a.parallel(calls[i]) {
			for j < len(calls) && a.parallel(calls[j]) {
				j++
			}
		}
		a.runBatch(c, calls[i:j], res[i:j])
		i = j
	}
	a.mu.Lock()
	g.newStep()
	for i, p := range calls {
		if res[i].denied {
			denied++
		}
		note, st := g.observe(p, res[i])
		res[i].output += note
		stop = stop || st
		a.appendLocked(c, chat.Message{Role: chat.RoleTool, Content: res[i].output, ToolCallID: p.call.ID, Name: p.call.Name,
			Brief: res[i].brief, Label: res[i].label, Failed: res[i].err != nil})
	}
	a.mu.Unlock()
	a.sync()
	return denied, stop
}

// parallel reports whether a call may share a batch: a known read-only tool, valid args.
func (a *Agent) parallel(p pendingCall) bool {
	known, readOnly, _ := a.deps.Tools.Kind(p.call.Name)
	return p.bad == "" && known && readOnly
}

// runBatch runs the calls at once and emits ToolStart for all, then ToolEnd as
// each finishes. Start and End come from the worker one at a time; ToolOutput
// comes from the tool goroutine while the call runs (see exec).
func (a *Agent) runBatch(c *conversation, calls []pendingCall, res []result) {
	shown := make([]chat.ToolCall, len(calls))
	labels := make([]string, len(calls))
	done := make(chan int, len(calls))
	for i, p := range calls {
		shown[i], labels[i] = p.call, a.labelOf(p.call)
		shown[i].Args = p.raw
		a.emit(c.tag(Event{Kind: EvToolStart, Call: shown[i], Label: labels[i]}))
		a.traceToolStart(c, shown[i], labels[i])
	}
	for i := range calls {
		go func() {
			t0 := time.Now()
			res[i] = a.exec(c, calls[i], shown[i])
			res[i].ms = time.Since(t0).Milliseconds()
			done <- i
		}()
	}
	for range calls {
		i := <-done
		res[i].label = labels[i]
		a.emit(c.tag(Event{Kind: EvToolEnd, Call: shown[i], Label: labels[i], Brief: res[i].brief, Output: res[i].output, Err: res[i].err}))
		a.traceToolEnd(c, shown[i], res[i])
	}
}

// labelOf is the one-line description of a call: the Tools' for their tools, the goal for `task`.
func (a *Agent) labelOf(call chat.ToolCall) string {
	if call.Name == taskTool && a.tasksOn() {
		return taskLabel(call.Args)
	}
	return a.deps.Tools.Label(call)
}

// exec resolves one call of c to a result; failures become text for the model. shown is the call
// as the UI sees it (raw args), used for ToolOutput events. Tools, Approve and a Task's tools run
// on c.toolsCtx; at depth 0 a mutating call (or a Task) is skipped while user text is queued (R-10).
func (a *Agent) exec(c *conversation, p pendingCall, shown chat.ToolCall) (res result) {
	defer func() {
		if v := recover(); v != nil {
			res = failed(fmt.Errorf("tool panicked: %v", v))
		}
	}()
	ctx := c.toolsCtx
	known, readOnly, confirm := a.deps.Tools.Kind(p.call.Name)
	task := p.call.Name == taskTool && a.tasksOn()
	switch {
	case p.bad != "":
		return failed(errors.New(p.bad))
	case !known && !task:
		return failed(fmt.Errorf("unknown tool %s", p.call.Name))
	case ctx.Err() != nil:
		return note(resCancelled)
	case (!readOnly || task) && c.depth == 0 && !p.synthetic && a.queued():
		return note(resSkipped) // R-10; the user's own /task goal is never skipped
	case task:
		return a.runTask(c, p)
	}
	if confirm && a.deps.Approve != nil {
		ap := a.deps.Approve(ctx, p.call)
		a.traceC(c, "approval", map[string]any{"id": p.call.ID, "name": p.call.Name, "allow": ap.Allow, "note": ap.Note})
		switch {
		case ctx.Err() != nil:
			return note(resCancelled)
		case !ap.Allow && ap.Note != "":
			return denial(resDenied + ": " + ap.Note + ". " + a.denyNote(c))
		case !ap.Allow:
			return denial(resDenied + ". " + a.denyNote(c))
		}
	}
	sink := &progressSink{emit: func(chunk string) { a.emit(c.tag(Event{Kind: EvToolOutput, Call: shown, Text: chunk})) }}
	hooks := ToolHooks{Progress: sink.send}
	if !readOnly && a.deps.Tools.Detachable(p.call.Name) {
		ch := make(chan struct{})
		hooks.Detach = ch
		a.setDetach(c, ch)
		defer a.setDetach(c, nil)
	}
	defer sink.close()
	out, brief, err := a.deps.Tools.Run(ctx, p.call, hooks)
	switch {
	case ctx.Err() != nil && err != nil:
		return note(resCancelled)
	case err != nil:
		return failed(err)
	}
	return result{output: out, brief: brief}
}

// note is a result that is plain text (skipped, denied, cancelled).
func note(s string) result { return result{output: s, brief: s} }

// denial is a note for a call the user refused.
func denial(s string) result {
	r := note(s)
	r.denied = true
	return r
}

// failed is an error result.
func failed(err error) result {
	s := "error: " + err.Error()
	return result{output: s, brief: s, err: err}
}
