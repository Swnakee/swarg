package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"swarg/internal/agent/guard"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Task mode: the agent loop on a goal, in its own history, returning one TaskResult
// (R-37..R-40, I-12, I-14)
// ---------------------------------------------------------------------------

// taskTool is the name of the built-in Tool that starts a Task.
const taskTool = "task"

// taskSpec is the `task` Tool as the model sees it; it is offered while depth < MaxDepth.
var taskSpec = chat.ToolSpec{
	Name:        taskTool,
	Description: `Hand one self-contained part of the work (e.g. "find where X is defined") to a helper; it returns a short report.`,
	Schema:      json.RawMessage(`{"type":"object","properties":{"goal":{"type":"string","description":"what to do"}},"required":["goal"]}`),
}

// TaskBlock is the SystemPrompt text about the `task` Tool; main adds it as a tool block when
// Tasks are on (the Tool lives in the agent, not in the tools package).
const TaskBlock = "task: hand one self-contained part of the work (many reads, a long output) to a helper that returns a short report; do the rest yourself.\n" +
	`Example: task {"goal": "find where the config is loaded"}`

// taskFrame wraps the goal as the first user Message of a Task.
const taskFrame = "Sub-task: %s\nDo only this. Do not ask the user. When done, answer with a short report: what was done, files changed (also by commands), key facts or values found, what failed."

// Refusals of a `task` call (error ToolResults without a model request, R-39).
const (
	errTaskNoGoal   = "task: goal is empty"
	errTaskDepth    = "task: nesting limit reached, do it yourself"
	errTaskCap      = "task: too many tasks in this turn, do it yourself"
	errTaskRepeated = "task: this is the task you are already doing; do it yourself"
)

// ErrTasksOff is returned by SubmitTask when Tasks are off (Deps.Tasks.MaxDepth 0).
var ErrTasksOff = errors.New("agent: tasks are off")

const (
	defaultTaskSteps   = 15  // Steps a Task may take when Deps.Tasks.MaxSteps is 0
	defaultTaskPerTurn = 8   // Tasks one Turn's Chain may start when Deps.Tasks.MaxPerTurn is 0
	taskLabelBytes     = 60  // bytes of the goal in a call's label
	taskReportBytes    = 750 // head and tail of the report kept in the TaskResult
)

// TaskLimits are the guards of Task mode. MaxDepth 0 = no Tasks (the Tool is not offered).
type TaskLimits struct {
	MaxDepth   int // Tasks may nest this deep: 1 = the Session may start Tasks, they may not
	MaxSteps   int // Steps per Task; 0 = 15
	MaxPerTurn int // Tasks per Session Turn, over the whole Chain; 0 = 8
}

// withDefaults fills the zero limits.
func (l TaskLimits) withDefaults() TaskLimits {
	if l.MaxSteps <= 0 {
		l.MaxSteps = defaultTaskSteps
	}
	if l.MaxPerTurn <= 0 {
		l.MaxPerTurn = defaultTaskPerTurn
	}
	return l
}

// tasksOn reports whether the `task` Tool exists in this Agent.
func (a *Agent) tasksOn() bool {
	return a.deps.Tasks.MaxDepth > 0 && a.deps.Tools != nil
}

// specs is the tool list of a request of c: the Tools' specs plus `task` below MaxDepth (a copy).
func (a *Agent) specs(c *conversation) []chat.ToolSpec {
	if a.deps.Tools == nil {
		return nil
	}
	specs := a.deps.Tools.Specs()
	if c.agent != nil { // an agent gets its allowlist, no `task`, no `agent` (I-41)
		out := slices.Clone(specs)
		return slices.DeleteFunc(out, func(sp chat.ToolSpec) bool {
			_, ro, _ := a.deps.Tools.Kind(sp.Name)
			return !c.agent.typ.Allows(sp.Name, ro)
		})
	}
	if a.tasksOn() && c.depth < a.deps.Tasks.MaxDepth {
		specs = slices.Concat(specs, []chat.ToolSpec{taskSpec})
	}
	if sp, ok := a.agentSpec(); ok && c.depth == 0 {
		specs = slices.Concat(specs, []chat.ToolSpec{sp})
	}
	return specs
}

// taskArgs are the arguments of a `task` call.
type taskArgs struct {
	Goal string `json:"goal"`
}

// goalOf parses the goal of a call; "" when absent or invalid.
func goalOf(args string) string {
	var ta taskArgs
	_ = json.Unmarshal([]byte(args), &ta)
	return strings.TrimSpace(ta.Goal)
}

// taskLabel is the one-line description of a `task` call: its goal, clipped.
func taskLabel(args string) string {
	return clipHead(goalOf(args), taskLabelBytes)
}

// sameGoal compares goals after trimming, case folding and collapsing blanks.
func sameGoal(x, y string) bool {
	return strings.Join(strings.Fields(strings.ToLower(x)), " ") == strings.Join(strings.Fields(strings.ToLower(y)), " ")
}

// ---------------------------------------------------------------------------
// Starting Tasks
// ---------------------------------------------------------------------------

// SubmitTask queues a /task Turn: the goal is run as a Task first, then the Session model
// answers from its result. Blank text is ignored; ErrTasksOff when Tasks are off. Like a text
// it waits for a running Turn (R-1), but it never joins one.
func (a *Agent) SubmitTask(goal string) error {
	if !a.tasksOn() {
		return ErrTasksOff
	}
	if strings.TrimSpace(goal) == "" {
		return nil
	}
	a.enqueueText(qitem{text: goal, task: true})
	return nil
}

// runGoal is the prelude of a /task Turn: a synthetic `task` call with the goal; the
// repeated-goal check does not apply to it.
func (a *Agent) runGoal(c *conversation, goal string, repeats *guard.Repeat) {
	args, _ := json.Marshal(taskArgs{Goal: goal})
	a.runPrelude(c, taskTool, string(args), repeats)
}

// runPrelude appends an assistant Message with one synthetic call (no model request) and runs
// the call like a model's, so its result lands in the history and the events flow as usual.
func (a *Agent) runPrelude(c *conversation, name, args string, repeats *guard.Repeat) {
	a.mu.Lock()
	a.callSeq++
	call := chat.ToolCall{ID: fmt.Sprintf("call_%d", a.callSeq), Name: name, Args: args}
	a.appendLocked(c, chat.Message{Role: chat.RoleAssistant, ToolCalls: []chat.ToolCall{call}})
	a.mu.Unlock()
	a.sync()
	a.runCalls(c, []pendingCall{{call: call, raw: call.Args, synthetic: true}}, repeats)
}

// runTask runs a `task` call of parent: the guards first (an error result, no model request),
// then the loop on a child conversation whose requests derive from the Turn's root ctx. The
// result is the TaskResult; it is the only thing of the Task that reaches the parent (R-37).
func (a *Agent) runTask(parent *conversation, p pendingCall) result {
	goal := goalOf(p.call.Args)
	if goal == "" {
		return failed(errors.New(errTaskNoGoal))
	}
	a.mu.Lock()
	switch {
	case a.interrupted:
		a.mu.Unlock()
		return note(resCancelled)
	case parent.depth >= a.deps.Tasks.MaxDepth:
		a.mu.Unlock()
		return failed(errors.New(errTaskDepth))
	case a.turnTasks >= a.deps.Tasks.MaxPerTurn:
		a.mu.Unlock()
		return failed(errors.New(errTaskCap))
	case !p.synthetic && a.repeatedLocked(goal):
		a.mu.Unlock()
		return failed(errors.New(errTaskRepeated))
	}
	a.taskSeq++
	a.turnTasks++
	c := &conversation{ctx: parent.ctx, system: parent.system, depth: parent.depth + 1,
		id: fmt.Sprintf("t%d", a.taskSeq), goal: goal, meter: meter{limit: parent.meter.limit}}
	c.toolsCtx, c.toolsCancel = context.WithCancel(c.ctx)
	a.push(c)
	a.appendLocked(c, chat.Message{Role: chat.RoleUser, Content: fmt.Sprintf(taskFrame, goal), TurnStart: true})
	a.mu.Unlock()
	a.sync()
	a.emit(Event{Kind: EvTaskStarted, Task: c.id, Depth: c.depth, Text: goal, Call: p.call})
	a.trace("task_start", map[string]any{"id": c.id, "parent": parent.id, "depth": c.depth, "goal": goal})

	defer func() { // also on a panic recovered by the parent's exec: the Chain must not keep a dead level
		a.mu.Lock()
		a.pop()
		c.toolsCancel()
		a.mu.Unlock()
	}()
	var repeats guard.Repeat
	r, msg := a.runLoop(c, &repeats)

	res := a.taskResult(c, r, msg)
	a.emit(Event{Kind: EvTaskEnded, Task: c.id, Brief: res.brief, Output: res.output})
	a.trace("task_end", map[string]any{"id": c.id, "status": r.status, "steps": r.steps, "result": res.output})
	return res
}

// repeatedLocked reports whether goal repeats the goal of a running ancestor or the Session
// Turn's request (the Session is chain[0]; its TurnStart is the last one). Caller holds mu.
func (a *Agent) repeatedLocked(goal string) bool {
	for _, c := range a.chain {
		if c.depth > 0 && sameGoal(c.goal, goal) {
			return true
		}
	}
	h := a.history
	if i := turnStartIndex(h); i < len(h) && h[i].TurnStart && sameGoal(h[i].Content, goal) {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// The TaskResult (R-38)
// ---------------------------------------------------------------------------

// filesRe finds the files part of a TaskResult's first line.
var filesRe = regexp.MustCompile(`^[^\n]*?; files: ([^\n]+)`)

// taskResult builds the result of a finished Task:
//
//	<status>[; files: a, b]
//	<report>
//	steps: N/M
//
// The report is the final text, else the generated step list. Line 1 doubles as the Brief, so
// a Compaction of the parent's span keeps the status and the files. A failed Task is an error
// result (R-7); a Task ended by denials counts as one denial for the parent.
func (a *Agent) taskResult(c *conversation, r *turnResult, msg chat.Message) result {
	line := r.status
	if files := a.changedFiles(c); len(files) > 0 {
		line += "; files: " + strings.Join(files, ", ")
	}
	report := headTail(strings.TrimSpace(msg.Content), taskReportBytes)
	if report == "" || r.status != statusDone {
		report = clipHead(generatedList(c.history), 2*taskReportBytes)
	}
	out := line
	if report != "" {
		out += "\n" + report
	}
	out += fmt.Sprintf("\nsteps: %d/%d", r.steps, a.stepLimit(c))
	res := result{output: out, brief: line, denied: r.status == statusDenied}
	if strings.HasPrefix(r.status, statusFailed) {
		res.err = errors.New(strings.TrimPrefix(r.status, statusFailed))
		res.output = "error: " + out
		res.brief = "error: " + line
	}
	return res
}

// changedFiles lists the paths the Task changed: the Touched path of every edit/write that ran
// and the files of every nested TaskResult, deduplicated, in order.
func (a *Agent) changedFiles(c *conversation) (out []string) {
	st := a.stubber()
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for i, m := range c.history {
		if m.Role != chat.RoleTool || !ran(m) {
			continue
		}
		call, ok := callOf(c.history, i)
		if !ok {
			continue
		}
		switch {
		case call.Name == taskTool || call.Name == agentTool:
			if sub := filesRe.FindStringSubmatch(m.Content); sub != nil {
				for _, p := range strings.Split(sub[1], ", ") {
					add(p)
				}
			}
		case st != nil:
			add(st.Touched(call))
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// /skill (R-83)
// ---------------------------------------------------------------------------

const skillTool = "skill"

// SkillOnlyText is the user text of a /skill Turn given without text (the UI shows the same).
const SkillOnlyText = "Apply this skill."

// ErrNoSkills is returned by SubmitSkill when the ToolRunner has no `skill` Tool (no Skills loaded).
var ErrNoSkills = errors.New("agent: no skills")

// SubmitSkill queues a /skill Turn: the Skill is applied by a synthetic `skill` call, then the
// model answers text (or skillOnlyText). Like a /task goal it never joins a running Turn.
func (a *Agent) SubmitSkill(name, text string) error {
	if a.deps.Tools == nil {
		return ErrNoSkills
	}
	if known, _, _ := a.deps.Tools.Kind(skillTool); !known {
		return ErrNoSkills
	}
	if strings.TrimSpace(text) == "" {
		text = SkillOnlyText
	}
	a.enqueueText(qitem{text: text, skill: strings.TrimSpace(name)})
	return nil
}

// runSkill is the prelude of a /skill Turn: a synthetic `skill` call for name.
func (a *Agent) runSkill(c *conversation, name string, repeats *guard.Repeat) {
	args, _ := json.Marshal(struct {
		Name string `json:"name"`
	}{name})
	a.runPrelude(c, skillTool, string(args), repeats)
}
