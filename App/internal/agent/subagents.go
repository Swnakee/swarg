package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"swarg/internal/agent/guard"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Sub-agents: child conversations with their own Source, prompt and tools, several at once
// (R-120..R-127, I-41, I-42)
// ---------------------------------------------------------------------------

// agentTool is the name of the built-in Tool that starts an agent.
const agentTool = "agent"

// agentFrame wraps the goal as the first user Message of an agent.
const agentFrame = "Job from the main agent: %s\nDo exactly this, nothing more. Do not ask the user; if something is unclear, make the most reasonable choice and say so. When done, answer with a short report: what was done, files changed (also by commands), key facts or values found, what failed."

// Refusals of an `agent` call (error ToolResults without a model request).
const (
	errAgentNoGoal   = "agent: goal is empty"
	errAgentCap      = "agent: too many agents and tasks in this turn, do it yourself"
	errAgentRepeated = "agent: another agent is already doing this job"
)

// AgentType is one kind of agent as the runtime needs it; main builds it from the agents package.
// Server, Model, Profile and Body are only passed on to Deps.Agents.Open.
type AgentType struct {
	Name, Description string
	Tools             []string // allowlist: tool names, "*" = all, "readonly" = every read-only tool; empty = all
	MaxSteps          int      // 0 = tasks.max_steps
	Server, Model     string
	Profile, Body     string
}

// Allows reports whether the type may use the tool (readOnly is its Kind flag).
func (t AgentType) Allows(name string, readOnly bool) bool {
	if len(t.Tools) == 0 {
		return true
	}
	for _, e := range t.Tools {
		if e == "*" || e == name || (e == "readonly" && readOnly) {
			return true
		}
	}
	return false
}

// AgentRun is what Open gives for one run: the Source (client, model, limits) and the system prompt.
type AgentRun struct {
	Source Source
	System string
}

// AgentDeps connect the agent runtime to the config and the prompt. A nil Types or Open means no
// agents at all.
type AgentDeps struct {
	Types func() []AgentType                  // the types offered now; none = the `agent` Tool is not offered (low profile, all off)
	Open  func(t AgentType) (AgentRun, error) // resolves the Source and the system prompt of a run
	Slots func(source string) int             // requests at once on a Source (max_parallel); nil or < 1 = 1
	Max   int                                 // agents at once over all Sources; 0 or more than 4 = 4
}

// agentRun is the state of one running agent, held by its conversation.
type agentRun struct {
	typ  AgentType
	src  Source
	call chat.ToolCall // the parent's `agent` call as shown: progress lines go to its block
}

// agentArgs are the arguments of an `agent` call.
type agentArgs struct {
	Type string `json:"type"`
	Goal string `json:"goal"`
}

func parseAgentArgs(args string) (a agentArgs) {
	_ = json.Unmarshal([]byte(args), &a)
	a.Type, a.Goal = strings.TrimSpace(a.Type), strings.TrimSpace(a.Goal)
	return a
}

// agentLabel is the one-line description of an `agent` call: type and goal, clipped.
func agentLabel(args string) string {
	a := parseAgentArgs(args)
	return clipHead(a.Type+": "+a.Goal, taskLabelBytes)
}

// agentTypes lists the types on offer now.
func (a *Agent) agentTypes() []AgentType {
	if a.deps.Tools == nil || a.deps.Agents.Types == nil || a.deps.Agents.Open == nil {
		return nil
	}
	return a.deps.Agents.Types()
}

// agentsOn reports whether the `agent` Tool is offered now.
func (a *Agent) agentsOn() bool { return len(a.agentTypes()) > 0 }

// agentSpec is the `agent` Tool as the model sees it, with the type names as an enum.
func (a *Agent) agentSpec() (chat.ToolSpec, bool) {
	types := a.agentTypes()
	if len(types) == 0 {
		return chat.ToolSpec{}, false
	}
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t.Name
	}
	enum, _ := json.Marshal(names)
	return chat.ToolSpec{
		Name:        agentTool,
		Description: "Hand a self-contained job to a sub-agent (" + strings.Join(names, ", ") + "); it works alone and returns a short report. Several agent calls in one step run in parallel.",
		Schema: json.RawMessage(`{"type":"object","properties":{"type":{"type":"string","enum":` + string(enum) +
			`},"goal":{"type":"string","description":"everything the agent needs: it sees nothing else"}},"required":["type","goal"]}`),
	}, true
}

// stepLimit is the Step limit of a conversation: its own, else the Task's, else the Turn's.
func (a *Agent) stepLimit(c *conversation) int {
	switch {
	case c.maxSteps > 0:
		return c.maxSteps
	case c.depth > 0:
		return a.deps.Tasks.MaxSteps
	}
	return a.deps.MaxSteps
}

// srcOf is the Source and the model a request of c uses: an agent's own, else the Session's.
// Caller holds mu.
func (a *Agent) srcOf(c *conversation) (Source, string) {
	if c.agent != nil {
		return c.agent.src, c.agent.src.Model
	}
	return a.src, a.model
}

// srcLimit is the ContextLimit of model on Source s.
func srcLimit(s Source, model string) int {
	if s.LimitFor != nil {
		return s.LimitFor(model)
	}
	return s.ContextLimit
}

// runAgent runs an `agent` call of the Session: the checks first (error results, no model
// request), then the Step loop on a child conversation of its own Source. The result is the
// TaskResult, the only thing of the agent that reaches the Session (R-127). Many runAgent calls
// may be active at once; they wait for a free slot (R-122).
func (a *Agent) runAgent(parent *conversation, p pendingCall, shown chat.ToolCall) result {
	args := parseAgentArgs(p.call.Args)
	if args.Goal == "" {
		return failed(errors.New(errAgentNoGoal))
	}
	typ, ok := a.agentType(args.Type)
	if !ok {
		return failed(fmt.Errorf("agent: unknown type %q; types: %s", args.Type, strings.Join(a.agentNames(), ", ")))
	}
	run, err := a.deps.Agents.Open(typ)
	if err != nil {
		return failed(fmt.Errorf("agent %s: %w", typ.Name, err))
	}
	limit := srcLimit(run.Source, run.Source.Model)

	a.mu.Lock()
	switch {
	case a.interrupted:
		a.mu.Unlock()
		return note(resCancelled)
	case a.turnTasks >= a.deps.Tasks.MaxPerTurn:
		a.mu.Unlock()
		return failed(errors.New(errAgentCap))
	case a.agentRunningLocked(typ.Name, args.Goal):
		a.mu.Unlock()
		return failed(errors.New(errAgentRepeated))
	}
	a.agentSeq++
	a.turnTasks++
	c := &conversation{ctx: parent.ctx, system: run.System, depth: parent.depth + 1,
		id: fmt.Sprintf("a%d", a.agentSeq), goal: args.Goal, meter: meter{limit: limit}, maxSteps: typ.MaxSteps,
		agent: &agentRun{typ: typ, src: run.Source, call: shown}}
	if c.maxSteps <= 0 {
		c.maxSteps = a.deps.Tasks.MaxSteps
	}
	c.toolsCtx, c.toolsCancel = context.WithCancel(c.ctx)
	if a.agents == nil {
		a.agents = map[string]*conversation{}
	}
	a.agents[c.id] = c
	if a.interrupted {
		c.toolsCancel()
	}
	a.appendLocked(c, chat.Message{Role: chat.RoleUser, Content: fmt.Sprintf(agentFrame, args.Goal), TurnStart: true})
	a.mu.Unlock()
	a.trace("agent_start", map[string]any{"id": c.id, "type": typ.Name, "source": run.Source.Name, "model": run.Source.Model, "goal": args.Goal})
	defer func() {
		a.mu.Lock()
		delete(a.agents, c.id)
		c.toolsCancel()
		a.mu.Unlock()
	}()

	n := 1
	if a.deps.Agents.Slots != nil {
		n = a.deps.Agents.Slots(run.Source.Name)
	}
	release, got := a.slots.take(c.toolsCtx, run.Source.Name, n, func() {
		a.agentProgress(c, "waiting for a free slot on "+run.Source.Name+"\n")
	})
	var r *turnResult
	var msg chat.Message
	if got {
		defer release()
		a.agentProgress(c, "model "+run.Source.Model+"\n")
		var repeats guard.Repeat
		r, msg = a.runLoop(c, &repeats)
	} else {
		r = &turnResult{finish: chat.FinishInterrupted, status: a.interruptedStatus()}
	}
	res := a.taskResult(c, r, msg)
	a.trace("agent_end", map[string]any{"id": c.id, "status": r.status, "steps": r.steps, "result": res.output})
	return res
}

// agentType finds an offered type by name.
func (a *Agent) agentType(name string) (AgentType, bool) {
	i := slices.IndexFunc(a.agentTypes(), func(t AgentType) bool { return t.Name == name })
	if i < 0 {
		return AgentType{}, false
	}
	return a.agentTypes()[i], true
}

func (a *Agent) agentNames() (out []string) {
	for _, t := range a.agentTypes() {
		out = append(out, t.Name)
	}
	return out
}

// agentRunningLocked reports whether a running agent of the type has the same goal. Caller holds mu.
func (a *Agent) agentRunningLocked(typ, goal string) bool {
	for _, c := range a.agents {
		if c.agent.typ.Name == typ && sameGoal(c.goal, goal) {
			return true
		}
	}
	return false
}

// agentProgress adds a line to the block of the agent's call in the UI (live output).
func (a *Agent) agentProgress(c *conversation, line string) {
	a.emit(Event{Kind: EvToolOutput, Call: c.agent.call, Text: line})
}

// agentEvent handles an event of an agent's loop. Nothing of an agent reaches the Session's list
// but a progress line per tool call in the block of its `agent` call (R-126).
func (a *Agent) agentEvent(ev Event) {
	if ev.Kind != EvToolStart {
		return
	}
	a.mu.Lock()
	c := a.agents[ev.Task]
	var line string
	if c != nil {
		line = fmt.Sprintf("step %d/%d %s\n", c.step, a.stepLimit(c), ev.Label)
	}
	a.mu.Unlock()
	if c != nil {
		a.agentProgress(c, line)
	}
}

// isAgentID reports whether a conversation id names an agent.
func isAgentID(id string) bool { return strings.HasPrefix(id, "a") }

// agentApproval runs the user's approval of a call of an agent one at a time (R-125); the
// prompts of parallel agents queue. It reports false when ctx ended while waiting.
func (a *Agent) agentApproval(ctx context.Context, call chat.ToolCall) (Approval, bool) {
	select {
	case a.approving <- struct{}{}:
	case <-ctx.Done():
		return Approval{}, false
	}
	defer func() { <-a.approving }()
	// Esc wins over a freed slot: select picks at random when both are ready, and Interrupt
	// cancels the agents' contexts one by one after setting interrupted.
	if ctx.Err() != nil || a.isInterrupted() {
		return Approval{}, false
	}
	return a.deps.Approve(ctx, call), true
}
