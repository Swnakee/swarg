package agent

import (
	"regexp"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Trace payloads (field names are the ones trace.Timeline understands)
// ---------------------------------------------------------------------------

// traceCall is a ToolCall in a payload.
type traceCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// traceMsg is a Message as the request carried it (a Stub replaces the content).
type traceMsg struct {
	Seq        int         `json:"seq,omitempty"`
	Role       string      `json:"role"`
	Content    string      `json:"content,omitempty"`
	ToolCalls  []traceCall `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Name       string      `json:"name,omitempty"`
	Stub       bool        `json:"stub,omitempty"`
}

func traceCalls(cs []chat.ToolCall) (out []traceCall) {
	for _, c := range cs {
		out = append(out, traceCall{ID: c.ID, Name: c.Name, Args: c.Args})
	}
	return out
}

// tagged adds the Task id to a payload made inside a Task (the Session's records stay as they are).
func (c *conversation) tagged(d map[string]any) map[string]any {
	if c.depth > 0 {
		d["task"] = c.id
	}
	return d
}

// traceC records at once, tagged with the conversation; call it without mu.
func (a *Agent) traceC(c *conversation, kind string, d map[string]any) {
	a.trace(kind, c.tagged(d))
}

// traceRequestLocked queues the request record of c: the messages appended since the previous
// request in full (stubs applied, as sent), the total and the model. Caller holds mu.
func (a *Agent) traceRequestLocked(c *conversation, step int, req chat.Request) {
	if a.deps.Trace == nil {
		return
	}
	added := []traceMsg{}
	top := c.lastReqSeq
	for _, m := range req.Messages[1:] { // [0] is the system prompt
		if m.Seq <= c.lastReqSeq {
			continue
		}
		added = append(added, traceMsg{Seq: m.Seq, Role: string(m.Role), Content: m.Content, ToolCalls: traceCalls(m.ToolCalls),
			ToolCallID: m.ToolCallID, Name: m.Name, Stub: m.Role == chat.RoleTool && m.Stub != ""})
	}
	for _, m := range c.history {
		top = max(top, m.Seq)
	}
	c.lastReqSeq = top
	a.traceLocked("request", c.tagged(map[string]any{"step": step, "msgs": len(req.Messages), "added": added, "model": req.Model, "tools": len(req.Tools)}))
}

// traceReply records the finished reply of a step: full text, reasoning and calls, once per reply.
func (a *Agent) traceReply(c *conversation, r *turnResult) {
	if a.deps.Trace == nil {
		return
	}
	d := map[string]any{"text": r.text.String(), "calls": traceCalls(storedCalls(r.calls)), "finish": string(r.finish)}
	if r.reasoning.Len() > 0 {
		d["reasoning"] = r.reasoning.String()
	}
	if r.err != nil {
		d["error"] = r.err.Error()
	}
	a.traceC(c, "delta", d)
}

// cutRe matches the note tools.Cap puts into a capped output.
var cutRe = regexp.MustCompile(`\[\.\.\. \d+ bytes cut;`)

// traceToolStart records a call about to run (full raw arguments).
func (a *Agent) traceToolStart(c *conversation, call chat.ToolCall, label string) {
	a.traceC(c, "tool_start", map[string]any{"id": call.ID, "name": call.Name, "args": call.Args, "label": label})
}

// traceToolEnd records the result as the model gets it; capped says a tool cut the output.
func (a *Agent) traceToolEnd(c *conversation, call chat.ToolCall, res result) {
	if a.deps.Trace == nil {
		return
	}
	d := map[string]any{"id": call.ID, "name": call.Name, "brief": res.brief, "output": res.output, "ms": res.ms,
		"capped": cutRe.MatchString(res.output)}
	if res.err != nil {
		d["error"] = res.err.Error()
	}
	a.traceC(c, "tool_end", d)
}
