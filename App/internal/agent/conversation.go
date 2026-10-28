package agent

import (
	"context"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Conversation: the history the Step loop works on (the Session or a Task)
// ---------------------------------------------------------------------------

// conversation is one history the Step loop runs on: the Session (depth 0, embedded in Agent)
// or a Task (depth >= 1, made by runTask). Its fields are guarded by the Agent's mutex.
type conversation struct {
	ctx         context.Context    // the Turn's root ctx: requests derive from it, never from a toolsCtx
	toolsCtx    context.Context    // cancelled by Interrupt and Abort; tools and Approve run on it
	toolsCancel context.CancelFunc // cancels toolsCtx
	reqCancel   context.CancelFunc // cancels the model request in flight; nil between Steps
	detach      chan struct{}      // detach channel of the running detachable call; nil if none
	history     []chat.Message
	system      string // system prompt of the Turn, for the requests and the estimate
	meter       meter
	emergency   bool         // the running Turn already had its emergency stub batch
	batch       []chat.Patch // patches of the stub batch being built
	lastReqSeq  int          // highest Seq the previous request carried (trace of added messages)
	depth       int          // 0 = the Session, 1 = a Task started by it, ...
	id          string       // "" for the Session, "t1", "t2", ... for Tasks
	goal        string       // the Task's goal; "" for the Session
}

// tag marks an event with the conversation it comes from (the Session leaves Task empty).
func (c *conversation) tag(ev Event) Event {
	ev.Task = c.id
	return ev
}

// inner is the innermost running conversation: the one whose request may be in flight and
// whose tools run. Caller holds mu.
func (a *Agent) inner() *conversation {
	return a.chain[len(a.chain)-1]
}

// push adds a conversation to the running Chain; if the Turn is already interrupted its tools are
// cancelled at once (an Interrupt that came before the cancel func existed). Caller holds mu.
func (a *Agent) push(c *conversation) {
	a.chain = append(a.chain, c)
	if a.interrupted {
		c.toolsCancel()
	}
}

// pop removes the innermost conversation. Caller holds mu.
func (a *Agent) pop() {
	a.chain = a.chain[:len(a.chain)-1]
}
