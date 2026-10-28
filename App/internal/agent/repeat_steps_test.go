package agent

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Steps for the repeat-guard scenarios of tool_flow.feature (R-24)
// ---------------------------------------------------------------------------

// historyToolResults returns the tool Messages of the settled Session history.
func (w *world) historyToolResults() ([]string, error) {
	if err := w.settle(); err != nil {
		return nil, err
	}
	var out []string
	for _, m := range w.ag.History() {
		if m.Role == chat.RoleTool {
			out = append(out, m.Content)
		}
	}
	return out, nil
}

// listReadPairs scripts n calls alternating list_dir and read_file of a missing file.
func (w *world) listReadPairs(missing string, n int) {
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			w.calls("", [2]string{"list_dir", jsonArgs("path", ".")})
		} else {
			w.calls("", [2]string{"read_file", jsonArgs("path", missing)})
		}
	}
}

func registerRepeatSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Provider that calls "([^"]*)" with the arguments "([^"]*)" (\d+) times in a row and then answers "([^"]*)"$`, func(tool, args string, n int, final string) error {
		w.real = true
		for i := 0; i < n; i++ {
			w.calls("", [2]string{tool, args})
		}
		w.reply(llmtest.Reply{Chunks: []string{final}})
		return nil
	})
	sc.Step(`^a Provider that calls "read_file" for the missing files "([^"]*)" and "([^"]*)" alternately (\d+) times and then answers "([^"]*)"$`, func(a, b string, n int, final string) error {
		w.real = true
		for i := 0; i < n; i++ {
			w.calls("", [2]string{"read_file", jsonArgs("path", [2]string{a, b}[i%2])})
		}
		w.reply(llmtest.Reply{Chunks: []string{final}})
		return nil
	})
	sc.Step(`^a Provider that lists the project folder (\d+) times in a row and then answers "([^"]*)"$`, func(n int, final string) error {
		w.real = true
		for i := 0; i < n; i++ {
			w.calls("", [2]string{"list_dir", jsonArgs("path", ".")})
		}
		w.reply(llmtest.Reply{Chunks: []string{final}})
		return nil
	})
	sc.Step(`^a Provider that lists the project folder and reads the missing file "([^"]*)" alternately (\d+) times and then answers "([^"]*)"$`, func(name string, n int, final string) error {
		w.real = true
		w.listReadPairs(name, n)
		w.reply(llmtest.Reply{Chunks: []string{final}})
		return nil
	})
	sc.Step(`^a Provider that lists the project folder and reads the missing file "([^"]*)" alternately (\d+) times, then reads the missing file "([^"]*)" and does the pair once more and then answers "([^"]*)"$`, func(name string, n int, other, final string) error {
		w.real = true
		w.listReadPairs(name, n)
		w.calls("", [2]string{"read_file", jsonArgs("path", other)})
		w.listReadPairs(name, 2)
		w.reply(llmtest.Reply{Chunks: []string{final}})
		return nil
	})
	sc.Step(`^a Provider that starts a background job and polls the first one alternately (\d+) times and then answers "([^"]*)"$`, func(n int, final string) error {
		w.real = true
		for i := 0; i < n; i++ {
			if i%2 == 0 {
				w.calls("", bgShell(gatedEcho("ready")))
			} else {
				w.calls("", jobCall("shell_output", "bg1"))
			}
		}
		w.reply(llmtest.Reply{Chunks: []string{final}})
		return nil
	})
	sc.Step(`^a Provider that polls a finished background job (\d+) times in a row and then answers "([^"]*)"$`, func(n int, final string) error {
		w.real, w.bgWait = true, true
		w.calls("", bgShell(gatedEcho("ready")))
		for i := 0; i < n; i++ {
			w.calls("", jobCall("shell_output", "bg1"))
		}
		w.reply(llmtest.Reply{Chunks: []string{final}})
		return nil
	})
	sc.Step(`^the last ToolResult in the Session history mentions "([^"]*)"$`, func(text string) error {
		got, err := w.historyToolResults()
		if err != nil {
			return err
		}
		if len(got) == 0 || !strings.Contains(got[len(got)-1], text) {
			return fmt.Errorf("last ToolResult = %q, want it to mention %q", got, text)
		}
		return nil
	})
	sc.Step(`^no ToolResult in the Session history mentions "([^"]*)"$`, func(text string) error {
		got, err := w.historyToolResults()
		if err != nil {
			return err
		}
		for _, r := range got {
			if strings.Contains(r, text) {
				return fmt.Errorf("ToolResult %q mentions %q", r, text)
			}
		}
		return nil
	})
}
