package agent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Steps for task_mode.feature, context_limit.feature and reasoning.feature. The helper is the
// model inside a Task; its replies are scripted like the Session model's, in request order.
// ---------------------------------------------------------------------------

// taskGoalArgs are the arguments of a scripted `task` call.
func taskGoalArgs(goal string) string { return jsonArgs("goal", goal) }

// startTask scripts a reply that calls `task` with the goal (the Session's or a helper's).
func (w *world) startTask(goal string) {
	w.real = true
	w.reply(w.toolReply([2]string{taskTool, taskGoalArgs(goal)}))
}

// answer scripts a plain text reply; "" is an empty answer.
func (w *world) say(text string) {
	r := llmtest.Reply{}
	if text != "" {
		r.Chunks = []string{text}
	}
	w.reply(r)
}

// taskResult returns the first TaskResult of the Session history.
func (w *world) taskResult() (chat.Message, error) {
	if err := w.settle(); err != nil {
		return chat.Message{}, err
	}
	for _, m := range w.toolMessages() {
		if m.Name == taskTool {
			return m, nil
		}
	}
	return chat.Message{}, errors.New("no TaskResult in the Session history")
}

// helperRequests counts the requests made by helpers (their first user Message is the goal frame).
func (w *world) helperRequests() int {
	n := 0
	for i := range w.srv.Requests() {
		msgs, _ := w.requestMessages(i)
		for _, m := range msgs {
			if s, _ := m["content"].(string); m["role"] == "user" && strings.HasPrefix(s, "Sub-task: ") {
				n++
				break
			}
		}
	}
	return n
}

// requestTools lists the tool names a request offered (n is 0-based).
func (w *world) requestTools(n int) ([]string, error) {
	r, err := w.request(n)
	if err != nil {
		return nil, err
	}
	var out []string
	raw, _ := r["tools"].([]any)
	for _, t := range raw {
		tm, _ := t.(map[string]any)
		fn, _ := tm["function"].(map[string]any)
		name, _ := fn["name"].(string)
		out = append(out, name)
	}
	return out, nil
}

// reasoningOf returns the reasoning_content values of a request's assistant messages.
func (w *world) reasoningOf(n int) ([]string, error) {
	msgs, err := w.requestMessages(n)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range msgs {
		if r, ok := m["reasoning_content"].(string); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func registerTaskSteps(sc *godog.ScenarioContext, w *world) {
	// Limits.
	sc.Step(`^Task mode allows depth (\d+)$`, func(d int) error { w.tasks = TaskLimits{MaxDepth: d}; return nil })
	sc.Step(`^Task mode allows depth (\d+) with (\d+) steps per Task$`, func(d, s int) error {
		w.tasks = TaskLimits{MaxDepth: d, MaxSteps: s}
		return nil
	})
	sc.Step(`^Task mode allows depth (\d+) and (\d+) Tasks per Turn$`, func(d, n int) error {
		w.tasks = TaskLimits{MaxDepth: d, MaxPerTurn: n}
		return nil
	})
	sc.Step(`^Task mode is off$`, func() error { w.tasks = TaskLimits{}; return nil })
	sc.Step(`^a context limit of (\d+) tokens for the model "([^"]*)"$`, func(n int, model string) error {
		if w.limits == nil {
			w.limits = map[string]int{}
		}
		w.limits[model] = n
		return nil
	})

	// Scripted replies, in request order.
	sc.Step(`^the (?:Provider|helper) starts a Task "([^"]*)"$`, func(goal string) error { w.startTask(goal); return nil })
	sc.Step(`^the Provider calls "([^"]*)", starts a Task "([^"]*)" and calls "([^"]*)" again$`, func(a, goal, b string) error {
		w.real = true
		w.reply(w.toolReply([2]string{a, jsonArgs("path", ".")}, [2]string{taskTool, taskGoalArgs(goal)}, [2]string{b, jsonArgs("path", ".")}))
		return nil
	})
	sc.Step(`^the helper calls "([^"]*)" for "([^"]*)" and then reports "([^"]*)"$`, func(tool, arg, report string) error {
		w.real = true
		w.reply(w.toolReply([2]string{tool, jsonArgs(argKey(tool), arg)}))
		w.say(report)
		return nil
	})
	sc.Step(`^the helper calls "([^"]*)" for "([^"]*)" (twice|three times)$`, func(tool, arg, times string) error {
		w.real = true
		for i := 0; i < map[string]int{"twice": 2, "three times": 3}[times]; i++ {
			w.reply(w.toolReply([2]string{tool, jsonArgs(argKey(tool), arg)}))
		}
		return nil
	})
	sc.Step(`^the helper edits "([^"]*)" replacing "([^"]*)" with "([^"]*)" and then reports "([^"]*)"$`, func(path, old, repl, report string) error {
		w.real = true
		w.reply(w.toolReply([2]string{"edit_file", jsonArgs("path", path, "old", old, "new", repl)}))
		w.say(report)
		return nil
	})
	sc.Step(`^the (?:helper reports|Provider then answers) "([^"]*)"$`, func(text string) error { w.say(text); return nil })
	sc.Step(`^the helper's request fails$`, func() error { w.reply(llmtest.Reply{Status: 500}); return nil })
	sc.Step(`^a Provider that thinks "([^"]*)" and then calls "([^"]*)" and then answers "([^"]*)"$`, func(think, tool, final string) error {
		w.real = true
		r := w.toolReply([2]string{tool, jsonArgs("path", ".")})
		r.Reasoning = []string{think}
		w.reply(r)
		w.say(final)
		return nil
	})
	sc.Step(`^the user sent "([^"]*)"$`, func(text string) error {
		w.act(func() error { w.submit(text); return nil })
		return nil
	})

	// Actions.
	sc.Step(`^the user sends "([^"]*)"$`, func(text string) error {
		if err := w.begin(); err != nil {
			return err
		}
		w.submit(text)
		return nil
	})
	sc.Step(`^the user runs /task "([^"]*)"$`, func(goal string) error {
		w.real = true // a Task needs Tools; the scenario's goal frame is run against the real set
		if err := w.begin(); err != nil {
			return err
		}
		w.taskErr = w.ag.SubmitTask(goal)
		if w.taskErr == nil && len(w.gates) == 0 {
			w.ag.Wait()
		}
		return nil
	})

	sc.Step(`^the user runs /task "([^"]*)" while the Turn runs$`, func(goal string) error {
		w.real = true
		if err := w.begin(); err != nil {
			return err
		}
		w.taskErr = w.ag.SubmitTask(goal)
		return w.taskErr
	})
	sc.Step(`^the text "([^"]*)" started its own Turn and nothing was injected$`, func(text string) error {
		if err := w.settle(); err != nil {
			return err
		}
		var starts []string
		for _, e := range w.snapshotEvents() {
			if e.Kind == EvTurnStarted {
				starts = append(starts, e.Text)
			}
		}
		if len(starts) != 2 || starts[1] != text {
			return fmt.Errorf("Turn starts = %q, want a second Turn %q", starts, text)
		}
		for _, m := range w.ag.History() {
			if m.Injected {
				return fmt.Errorf("a Message was injected: %+v", m)
			}
		}
		return nil
	})

	sc.Step(`^the text "([^"]*)" joined the Session after the TaskResult and never reached the helper$`, func(text string) error {
		if err := w.settle(); err != nil {
			return err
		}
		task, injected := -1, -1
		for i, m := range w.ag.History() {
			switch {
			case m.Role == chat.RoleTool && m.Name == taskTool:
				task = i
			case m.Injected && m.Content == text:
				injected = i
			}
		}
		if task < 0 || injected < task {
			return fmt.Errorf("TaskResult at %d, injected text at %d; want the text after the result", task, injected)
		}
		for i := range w.srv.Requests() {
			msgs, _ := w.requestMessages(i)
			helper := false
			for _, m := range msgs {
				s, _ := m["content"].(string)
				helper = helper || (m["role"] == "user" && strings.HasPrefix(s, "Sub-task: "))
				if helper && strings.Contains(s, text) {
					return fmt.Errorf("the helper's request %d carries %q", i+1, text)
				}
			}
		}
		return nil
	})

	// The TaskResult.
	sc.Step(`^the TaskResult starts with "([^"]*)"$`, func(prefix string) error {
		res, err := w.taskResult()
		if err != nil {
			return err
		}
		if !strings.HasPrefix(res.Content, prefix) {
			return fmt.Errorf("TaskResult = %q, want the prefix %q", res.Content, prefix)
		}
		return nil
	})
	sc.Step(`^the TaskResult starts with "([^"]*)" and lists "([^"]*)"$`, func(prefix, tool string) error {
		res, err := w.taskResult()
		if err != nil {
			return err
		}
		if !strings.HasPrefix(res.Content, prefix) || !strings.Contains(res.Content, tool) {
			return fmt.Errorf("TaskResult = %q, want the prefix %q and %q", res.Content, prefix, tool)
		}
		return nil
	})
	sc.Step(`^the TaskResult starts with "([^"]*)" and the Turn ends with the answer "([^"]*)"$`, func(prefix, answer string) error {
		res, err := w.taskResult()
		if err != nil {
			return err
		}
		last, err := w.lastAssistant()
		if err != nil {
			return err
		}
		if !strings.HasPrefix(res.Content, prefix) || last.Content != answer {
			return fmt.Errorf("TaskResult = %q, last answer = %q; want %q and %q", res.Content, last.Content, prefix, answer)
		}
		return nil
	})
	sc.Step(`^the TaskResult's first line is "([^"]*)"$`, func(line string) error {
		res, err := w.taskResult()
		if err != nil {
			return err
		}
		if first, _, _ := strings.Cut(res.Content, "\n"); first != line || res.Brief != line {
			return fmt.Errorf("TaskResult first line %q, brief %q; want %q", first, res.Brief, line)
		}
		return nil
	})
	sc.Step(`^the TaskResult is the step list under the status "([^"]*)"$`, func(status string) error {
		res, err := w.taskResult()
		if err != nil {
			return err
		}
		lines := strings.Split(res.Content, "\n")
		if len(lines) != 3 || lines[0] != status || !strings.Contains(lines[1], "read_file") || !strings.HasPrefix(lines[2], "steps: ") {
			return fmt.Errorf("TaskResult = %q, want status, the step list and the steps line", res.Content)
		}
		return nil
	})
	sc.Step(`^the Session history has exactly (\d+) TaskResults?$`, func(n int) error {
		if err := w.settle(); err != nil {
			return err
		}
		got := 0
		for _, m := range w.toolMessages() {
			if m.Name == taskTool {
				got++
			}
		}
		if got != n {
			return fmt.Errorf("%d TaskResults, want %d", got, n)
		}
		return nil
	})
	sc.Step(`^the ToolResults of the Session are ([a-z_, ]+)$`, func(list string) error {
		if err := w.settle(); err != nil {
			return err
		}
		var got []string
		for _, m := range w.toolMessages() {
			got = append(got, m.Name)
		}
		if want := strings.Join(strings.Split(list, ", "), ","); strings.Join(got, ",") != want {
			return fmt.Errorf("ToolResults = %v, want %s", got, want)
		}
		return nil
	})
	sc.Step(`^the task call "([^"]*)" is refused with "([^"]*)" and only (\d+) helper requests were made$`, func(goal, msg string, n int) error {
		if err := w.settle(); err != nil {
			return err
		}
		for _, m := range w.toolMessages() {
			if m.Name == taskTool && m.Label == goal {
				if m.Content != "error: "+msg {
					return fmt.Errorf("task %q result = %q, want %q", goal, m.Content, "error: "+msg)
				}
				if got := w.helperRequests(); got != n {
					return fmt.Errorf("%d helper requests, want %d", got, n)
				}
				return nil
			}
		}
		return fmt.Errorf("no task call %q in the history", goal)
	})

	// Requests the Provider received (1-based in the scenarios).
	sc.Step(`^the final request carries no ToolResult of "([^"]*)"$`, func(tool string) error {
		if err := w.settle(); err != nil {
			return err
		}
		msgs, err := w.requestMessages(len(w.srv.Requests()) - 1)
		if err != nil {
			return err
		}
		for _, m := range msgs {
			if m["role"] == "tool" && m["name"] == tool {
				return fmt.Errorf("the final request carries a %s result: %v", tool, m["content"])
			}
		}
		return nil
	})
	sc.Step(`^request (\d+) carries a ToolResult starting with "([^"]*)"$`, func(n int, prefix string) error {
		if err := w.settle(); err != nil {
			return err
		}
		res, err := w.toolResults(n - 1)
		if err != nil {
			return err
		}
		for _, r := range res {
			if strings.HasPrefix(r, prefix) {
				return nil
			}
		}
		return fmt.Errorf("request %d has no ToolResult starting with %q: %q", n, prefix, res)
	})
	sc.Step(`^the second request offers no Tool named "([^"]*)"$`, func(name string) error {
		if err := w.settle(); err != nil {
			return err
		}
		tools, err := w.requestTools(1)
		if err != nil {
			return err
		}
		for _, t := range tools {
			if t == name {
				return fmt.Errorf("the second request offers %q: %v", name, tools)
			}
		}
		return nil
	})
	sc.Step(`^request (\d+) carries the reasoning "([^"]*)" on its assistant message$`, func(n int, think string) error {
		if err := w.settle(); err != nil {
			return err
		}
		got, err := w.reasoningOf(n - 1)
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0] != think {
			return fmt.Errorf("request %d reasoning = %q, want [%q]", n, got, think)
		}
		return nil
	})
	sc.Step(`^request (\d+) carries no reasoning$`, func(n int) error {
		if err := w.settle(); err != nil {
			return err
		}
		got, err := w.reasoningOf(n - 1)
		if err != nil {
			return err
		}
		if len(got) != 0 {
			return fmt.Errorf("request %d carries reasoning %q", n, got)
		}
		return nil
	})
	sc.Step(`^no request carries reasoning$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		for i := range w.srv.Requests() {
			if got, _ := w.reasoningOf(i); len(got) != 0 {
				return fmt.Errorf("request %d carries reasoning %q", i+1, got)
			}
		}
		return nil
	})

	// Chain, Trace, warnings.
	sc.Step(`^the Trace has a task_end for every task_start$`, func() error { // the Session history's validity is I-3, checked by the step too
		starts, err := w.records("task_start")
		if err != nil {
			return err
		}
		ends, err := w.records("task_end")
		if err != nil {
			return err
		}
		if len(starts) == 0 || len(starts) != len(ends) {
			return fmt.Errorf("%d task_start and %d task_end records", len(starts), len(ends))
		}
		h := w.ag.History()
		answered := map[string]bool{}
		for _, m := range h {
			if m.Role == chat.RoleTool {
				answered[m.ToolCallID] = true
			}
		}
		for _, m := range h {
			for _, c := range m.ToolCalls {
				if !answered[c.ID] {
					return fmt.Errorf("ToolCall %s has no ToolResult", c.ID)
				}
			}
		}
		return nil
	})
	sc.Step(`^the Trace holds (\d+) task_end records with the status "([^"]*)"$`, func(n int, status string) error {
		ends, err := w.records("task_end")
		if err != nil {
			return err
		}
		if len(ends) != n {
			return fmt.Errorf("%d task_end records, want %d", len(ends), n)
		}
		for _, e := range ends {
			if s, _ := e.D["status"].(string); s != status {
				return fmt.Errorf("task_end status %q, want %q", s, status)
			}
		}
		return nil
	})
	sc.Step(`^the Trace records of the helper carry the Task id and the Session's do not$`, func() error {
		starts, err := w.records("tool_start")
		if err != nil {
			return err
		}
		for _, s := range starts {
			name, _ := s.D["name"].(string)
			id, _ := s.D["task"].(string)
			if name == taskTool && id != "" {
				return fmt.Errorf("the Session's task call is tagged %q", id)
			}
			if name != taskTool && id != "t1" {
				return fmt.Errorf("the helper's %s call is tagged %q, want t1", name, id)
			}
		}
		return nil
	})
	sc.Step(`^the server-busy warning is shown once$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		n := 0
		for _, t := range w.warnings() {
			if t == busyWarning {
				n++
			}
		}
		if n != 1 {
			return fmt.Errorf("the busy warning was shown %d times: %v", n, w.warnings())
		}
		return nil
	})
	sc.Step(`^the command is refused because tasks are off$`, func() error {
		if !errors.Is(w.taskErr, ErrTasksOff) {
			return fmt.Errorf("SubmitTask returned %v, want ErrTasksOff", w.taskErr)
		}
		return nil
	})
	sc.Step(`^the meter limit is (\d+)$`, func(n int) error {
		if err := w.settle(); err != nil {
			return err
		}
		evs := w.snapshotEvents()
		for i := len(evs) - 1; i >= 0; i-- {
			if evs[i].Kind == EvMeter {
				if evs[i].Limit != n {
					return fmt.Errorf("meter limit %d, want %d", evs[i].Limit, n)
				}
				return nil
			}
		}
		return errors.New("no Meter event")
	})
}
