package ui

import (
	"errors"

	"swarg/internal/agent"
)

// ---------------------------------------------------------------------------
// Task blocks: the stack of running Tasks, placing items inside them, /task
// ---------------------------------------------------------------------------

// taskStart pushes the block of the call that started a Task. The block itself was added by
// the call's EvToolStart, at the depth of the Task that made the call.
func (m *model) taskStart(ev agent.Event) {
	m.endLive(false)
	if r := m.runningByID(ev.Call.ID); r != nil {
		m.tasks = append(m.tasks, r.item)
	}
}

// taskEnd pops the innermost Task; the text it streamed last is finished first, so the
// Session's next text starts its own item.
func (m *model) taskEnd() {
	m.endLive(false)
	if n := len(m.tasks); n > 0 {
		m.tasks = m.tasks[:n-1]
	}
}

// place puts item i inside the innermost running Task (no-op for the Session) and returns i.
func (m *model) place(i int) int {
	if n := len(m.tasks); n > 0 {
		m.cv.nest(i, n, m.tasks[n-1])
	}
	return i
}

// taskGoal is the goal of the innermost running Task, "" when none runs.
func (m *model) taskGoal() string {
	if len(m.tasks) == 0 {
		return ""
	}
	top := m.tasks[len(m.tasks)-1]
	for _, n := range m.runN {
		if r := m.rec(n); r != nil && r.item == top {
			return firstLine(Clean(r.label))
		}
	}
	return ""
}

// submitTask is /task <goal>: the agent runs the goal as a Task. An agent without Tasks (or
// with them off) says so.
func (m *model) submitTask(goal string) {
	ts, ok := m.d.Agent.(taskSubmitter)
	err := agent.ErrTasksOff
	if ok {
		err = ts.SubmitTask(goal)
	}
	switch {
	case errors.Is(err, agent.ErrTasksOff):
		m.say("tasks are off (tasks.max_depth)")
	case err != nil:
		m.say("task: " + err.Error())
	default:
		m.addWaiting(goal) // shown at once, before the agent takes it (R-27)
	}
}
