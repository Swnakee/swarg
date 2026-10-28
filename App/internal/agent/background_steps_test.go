package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Steps for background_jobs.feature. Real shell commands run as BackgroundJobs
// of the world's tools.Jobs (killed by the After hook); the user's move of a
// running Tool to the background goes through the probe runner.
// ---------------------------------------------------------------------------

// longCommand runs far longer than any scenario.
func longCommand() string {
	if runtime.GOOS == "windows" {
		return "Start-Sleep 30"
	}
	return "sleep 30"
}

// bgShell is the raw arguments of a background shell call.
func bgShell(command string) [2]string {
	b, _ := json.Marshal(map[string]any{"command": command, "background": true})
	return [2]string{"shell", string(b)}
}

// jobCall is a shell_output or shell_kill call for a job id.
func jobCall(tool, id string) [2]string { return [2]string{tool, jsonArgs("id", id)} }

// toolMessagesOf returns the contents of the tool Messages of one Tool, in order.
func (w *world) toolMessagesOf(name string) (out []string) {
	for _, m := range w.toolMessages() {
		if m.Name == name {
			out = append(out, m.Content)
		}
	}
	return out
}

// wantJobRunning settles the Session and checks whether the job runs.
func (w *world) wantJobRunning(id string, want bool) error {
	if err := w.settle(); err != nil {
		return err
	}
	for _, j := range w.jobs.List() {
		if j.ID == id {
			if j.Running != want {
				return fmt.Errorf("BackgroundJob %s running = %v, want %v", id, j.Running, want)
			}
			return nil
		}
	}
	return fmt.Errorf("no BackgroundJob %q (%v)", id, w.jobs.List())
}

// wantRead settles the Session and checks the n-th (0-based) shell_output result.
func (w *world) wantRead(n int, ok func(string) bool, what string) error {
	if err := w.settle(); err != nil {
		return err
	}
	reads := w.toolMessagesOf("shell_output")
	if n >= len(reads) {
		return fmt.Errorf("only %d shell_output results, want index %d", len(reads), n)
	}
	if !ok(reads[n]) {
		return fmt.Errorf("shell_output result %d = %q, want %s", n, reads[n], what)
	}
	return nil
}

func registerBackgroundSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Provider that starts a long-running command as a BackgroundJob and then answers "([^"]*)"$`, func(final string) error {
		w.real = true
		w.calls(final, bgShell(longCommand()))
		return nil
	})
	sc.Step(`^a Provider that starts a long-running command as a BackgroundJob, kills "([^"]*)" and then answers "([^"]*)"$`, func(id, final string) error {
		w.real = true
		w.calls("", bgShell(longCommand()))
		w.calls(final, jobCall("shell_kill", id))
		return nil
	})
	sc.Step(`^a Provider that starts a command printing "([^"]*)" as a BackgroundJob, reads "([^"]*)"( twice)? and then answers "([^"]*)"$`, func(text, id, twice, final string) error {
		w.real = true
		w.calls("", bgShell(gatedEcho(text)))
		w.calls("", jobCall("shell_output", id))
		if twice != "" {
			w.calls("", jobCall("shell_output", id))
		}
		w.reply(llmtest.Reply{Chunks: []string{final}})
		return nil
	})
	sc.Step(`^a Provider that reads the BackgroundJob "([^"]*)" and then answers "([^"]*)"$`, func(id, final string) error {
		w.real = true
		w.calls(final, jobCall("shell_output", id))
		return nil
	})
	sc.Step(`^the BackgroundJob is given time to finish before the Tool reads it$`, func() error {
		w.bgWait = true
		return nil
	})
	sc.Step(`^the user approves every mutating ToolCall$`, func() error {
		w.answer(Approval{Allow: true})
		return nil
	})
	sc.Step(`^the user denies the kill$`, func() error {
		w.real = true
		w.approve = func(_ context.Context, c chat.ToolCall) Approval {
			w.asked.Add(1)
			return Approval{Allow: c.Name != "shell_kill"}
		}
		return nil
	})

	// Moving the running Tool to the background.
	sc.Step(`^a Provider that calls the blocked detachable Tool "([^"]*)" and then answers "([^"]*)"$`, func(name, final string) error {
		p := w.probeRunner()
		p.add(name, false, true)
		p.detachable[name] = true
		w.callAll(final, name)
		return nil
	})
	sc.Step(`^an idle Agent$`, func() error { return nil })
	sc.Step(`^the user moves the running Tool to the background$`, func() error {
		if err := w.begin(); err != nil {
			return err
		}
		w.moved = w.ag.Background()
		return nil
	})

	// Results.
	sc.Step(`^the ToolResult of the shell ToolCall says the command started in the background as "([^"]*)"$`, func(id string) error {
		if err := w.settle(); err != nil {
			return err
		}
		res := w.toolMessagesOf("shell")
		if len(res) != 1 || !strings.Contains(res[0], "started in background as "+id) {
			return fmt.Errorf("shell ToolResults = %q, want one starting %s in the background", res, id)
		}
		return nil
	})
	sc.Step(`^the ToolResult of the read contains "([^"]*)"$`, func(text string) error {
		return w.wantRead(0, func(s string) bool { return strings.Contains(s, text) }, "to contain "+text)
	})
	sc.Step(`^the ToolResult of the second read does not contain "([^"]*)"$`, func(text string) error {
		return w.wantRead(1, func(s string) bool { return !strings.Contains(s, text) }, "not to contain "+text)
	})
	sc.Step(`^the ToolResult of the read is an error naming "([^"]*)"$`, func(id string) error {
		return w.wantRead(0, func(s string) bool { return strings.HasPrefix(s, "error:") && strings.Contains(s, id) }, "an error naming "+id)
	})
	sc.Step(`^the BackgroundJob "([^"]*)" is still running$`, func(id string) error { return w.wantJobRunning(id, true) })
	sc.Step(`^the BackgroundJob "([^"]*)" is not running$`, func(id string) error { return w.wantJobRunning(id, false) })
	sc.Step(`^the ToolResult of "([^"]*)" says it moved to the background$`, func(name string) error {
		if err := w.settle(); err != nil {
			return err
		}
		res := w.toolMessagesOf(name)
		if len(res) != 1 || !strings.Contains(res[0], "moved to the background") {
			return fmt.Errorf("ToolResults of %s = %q, want one that moved to the background", name, res)
		}
		return nil
	})
	sc.Step(`^the Provider receives a second request containing the ToolResult of "([^"]*)"$`, func(name string) error {
		if err := w.settle(); err != nil {
			return err
		}
		own := w.toolMessagesOf(name)
		sent, err := w.toolResults(1)
		if err != nil {
			return err
		}
		if len(own) != 1 || len(sent) != 1 || sent[0] != own[0] {
			return fmt.Errorf("second request ToolResults = %q, want the result of %s %q", sent, name, own)
		}
		return nil
	})
	sc.Step(`^no Tool was moved to the background$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if w.moved {
			return fmt.Errorf("Background reported a moved Tool")
		}
		return nil
	})
}

// gateFile is created by realRunner just before it waits for a job (bgWait), releasing a
// command that printed nothing until then: its output can never land before the job was
// adopted (the start result returns whatever was printed by then).
const gateFile = "bg-gate"

// gatedEcho is a command that prints text only after gateFile exists.
func gatedEcho(text string) string {
	if runtime.GOOS == "windows" {
		return "while (!(Test-Path " + gateFile + ")) { Start-Sleep -Milliseconds 10 }; echo " + text
	}
	return "until [ -e " + gateFile + " ]; do sleep 0.01; done; echo " + text
}
