package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
	"swarg/internal/tools"
)

// ---------------------------------------------------------------------------
// Steps for tools.feature: the real tools.Set on the scenario cwd, plus the
// helpers every tool feature shares (scripted ToolCalls, request decoding,
// file steps). The agent may not import tools, this test package may.
// ---------------------------------------------------------------------------

// realRunner mirrors the adapter in cmd/swarg: a tools.Set bound to one project.
type realRunner struct {
	set  *tools.Set
	env  tools.Env
	wait bool // shell_output first waits until its job is done (a channel, not a sleep)
}

func (r realRunner) Specs() []chat.ToolSpec { return r.set.Specs() }

func (r realRunner) Kind(name string) (known, readOnly, confirm bool) { return r.set.Kind(name) }

var _ ToolRunner = realRunner{}

func (r realRunner) Detachable(name string) bool { return name == "shell" }

func (r realRunner) Label(c chat.ToolCall) string {
	return r.set.Label(c.Name, json.RawMessage(c.Args))
}

func (r realRunner) Run(ctx context.Context, c chat.ToolCall, h ToolHooks) (string, string, error) {
	env := r.env
	if r.wait && c.Name == "shell_output" {
		var a struct{ ID string }
		if json.Unmarshal([]byte(c.Args), &a) == nil {
			if err := os.WriteFile(filepath.Join(env.Dir, gateFile), nil, 0o600); err != nil {
				return "", "", err
			}
			if err := waitChan("the BackgroundJob "+a.ID+" to finish", env.Jobs.Done(a.ID)); err != nil {
				return "", "", err
			}
		}
	}
	env.Progress, env.Detach = h.Progress, h.Detach
	res, err := r.set.Run(ctx, env, c.Name, json.RawMessage(c.Args))
	return res.Output, res.Brief, err
}

// Stub and Touched make realRunner a ToolStubber, as the adapter in cmd/swarg is.
func (r realRunner) Stub(c chat.ToolCall, output, brief string) string {
	return r.set.Stub(c.Name, json.RawMessage(c.Args), output, brief)
}

func (r realRunner) Touched(c chat.ToolCall) string {
	return r.set.Touched(c.Name, json.RawMessage(c.Args))
}

var _ ToolStubber = realRunner{}

// toolRunner returns the runner the scenario asked for, or nil for none.
func (w *world) toolRunner() (ToolRunner, error) {
	if w.probe != nil {
		return w.probe, nil
	}
	if !w.real {
		return nil, nil
	}
	root, err := os.OpenRoot(w.cwd())
	if err != nil {
		return nil, err
	}
	w.tb.Cleanup(func() { root.Close() })
	set, err := tools.NewSet(tools.All(), w.disabled)
	if err != nil {
		return nil, err
	}
	jobs := tools.NewJobs()
	w.jobs = jobs
	w.tb.Cleanup(jobs.KillAll)
	env := tools.Env{Root: root, Dir: w.cwd(), OS: runtime.GOOS, ShellTimeout: waitDeadline, Jobs: jobs}
	return realRunner{set: set, env: env, wait: w.bgWait}, nil
}

// ---------------------------------------------------------------------------
// Scripting ToolCalls
// ---------------------------------------------------------------------------

// toolReply is a reply that calls the given tools (name, raw args pairs) with fresh ids.
func (w *world) toolReply(calls ...[2]string) llmtest.Reply {
	var cs []chat.ToolCall
	for _, c := range calls {
		w.callN++
		cs = append(cs, chat.ToolCall{ID: fmt.Sprintf("call_%d", w.callN), Name: c[0], Args: c[1]})
	}
	return llmtest.Reply{ToolCalls: cs}
}

// calls scripts one answer with the calls, then the final text answer (if any).
func (w *world) calls(final string, calls ...[2]string) {
	w.reply(w.toolReply(calls...))
	if final != "" {
		w.reply(llmtest.Reply{Chunks: []string{final}})
	}
}

// argKey is the argument that the single-value "for" steps fill.
func argKey(tool string) string {
	switch tool {
	case "shell":
		return "command"
	case "web_fetch":
		return "url"
	case "web_search":
		return "query"
	}
	return "path"
}

// jsonArgs encodes key/value pairs as a JSON object.
func jsonArgs(kv ...string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// ---------------------------------------------------------------------------
// Reading what the Provider received
// ---------------------------------------------------------------------------

// request returns request n of the Provider (negative: from the end).
func (w *world) request(n int) (map[string]any, error) {
	reqs := w.srv.Requests()
	if n < 0 {
		n += len(reqs)
	}
	if n < 0 || n >= len(reqs) {
		return nil, fmt.Errorf("request %d does not exist (%d sent)", n, len(reqs))
	}
	return reqs[n], nil
}

// requestMessages returns the messages of request n.
func (w *world) requestMessages(n int) ([]map[string]any, error) {
	r, err := w.request(n)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	raw, _ := r["messages"].([]any)
	for _, m := range raw {
		mm, _ := m.(map[string]any)
		out = append(out, mm)
	}
	return out, nil
}

// toolResults returns the content of every tool message of request n.
func (w *world) toolResults(n int) (out []string, err error) {
	msgs, err := w.requestMessages(n)
	if err != nil {
		return nil, err
	}
	for _, m := range msgs {
		if m["role"] == "tool" {
			s, _ := m["content"].(string)
			out = append(out, s)
		}
	}
	return out, nil
}

// secondRequestHasResult settles the Session and checks that some ToolResult of
// the second request satisfies ok.
func (w *world) secondRequestHasResult(what string, ok func(string) bool) error {
	if w.symlinkSkipped {
		return nil // noted: symlinks are unsupported here, nothing to refuse
	}
	if err := w.settle(); err != nil {
		return err
	}
	res, err := w.toolResults(1)
	if err != nil {
		return err
	}
	for _, r := range res {
		if ok(r) {
			return nil
		}
	}
	return fmt.Errorf("no ToolResult %s in the second request: %q", what, res)
}

// toolMessages returns the tool messages of the history.
func (w *world) toolMessages() (out []chat.Message) {
	for _, m := range w.ag.History() {
		if m.Role == chat.RoleTool {
			out = append(out, m)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Files under the scenario cwd
// ---------------------------------------------------------------------------

// writeFile creates a file under the cwd and remembers its text.
func (w *world) writeFile(name, text string) error {
	if w.files == nil {
		w.files = map[string]string{}
	}
	w.files[name] = text
	p := filepath.Join(w.cwd(), filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(text), 0o644)
}

// readFile settles the Session and returns the file text.
func (w *world) readFile(name string) (string, error) {
	if err := w.settle(); err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(w.cwd(), filepath.FromSlash(name)))
	return string(b), err
}

func registerToolsSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a file "([^"]*)" with the text "([^"]*)"$`, func(name, text string) error {
		w.real = true
		return w.writeFile(name, text)
	})
	sc.Step(`^a file "([^"]*)" with Windows line endings and the lines "([^"]*)" and "([^"]*)"$`, func(name, a, b string) error {
		w.real = true
		return w.writeFile(name, a+"\r\n"+b+"\r\n")
	})
	sc.Step(`^a symlink "([^"]*)" to a file outside the project folder$`, func(name string) error {
		w.real = true
		target := filepath.Join(w.parentDir(), "outside.txt")
		if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
			return err
		}
		if err := os.Symlink(target, filepath.Join(w.cwd(), name)); err != nil {
			w.symlinkSkipped = true // symlinks unsupported here: the Then step passes with a note
		}
		return nil
	})
	sc.Step(`^the Tool "([^"]*)" is disabled$`, func(name string) error {
		w.real = true
		w.disabled = append(w.disabled, name)
		return nil
	})

	// Scripted ToolCalls of the real tools.
	sc.Step(`^a Provider that calls (?:the Tool )?"([^"]*)" for "([^"]*)" and then answers "([^"]*)"$`, func(tool, arg, final string) error {
		w.real = true
		w.calls(final, [2]string{tool, jsonArgs(argKey(tool), arg)})
		return nil
	})
	sc.Step(`^a Provider that calls (?:the Tool )?"([^"]*)" and then answers "([^"]*)"$`, func(tool, final string) error {
		w.real = true
		w.calls(final, [2]string{tool, "{}"})
		return nil
	})
	sc.Step(`^a Provider that calls "([^"]*)" and then "([^"]*)" and then answers "([^"]*)"$`, func(a, b, final string) error {
		w.real = true
		w.calls("", [2]string{a, jsonArgs("path", ".")})
		w.calls(final, [2]string{b, jsonArgs("path", "missing.txt")})
		return nil
	})
	sc.Step(`^a Provider that calls "([^"]*)" with the arguments "([^"]*)" and then answers "([^"]*)"$`, func(tool, args, final string) error {
		w.real = true
		w.calls(final, [2]string{tool, args})
		return nil
	})
	sc.Step(`^a Provider that calls "edit_file" replacing "([^"]*)" with "([^"]*)" in "([^"]*)" and then answers "([^"]*)"$`, func(old, repl, path, final string) error {
		w.real = true
		w.calls(final, [2]string{"edit_file", jsonArgs("path", path, "old", old, "new", repl)})
		return nil
	})
	sc.Step(`^a Provider that calls "write_file" for "([^"]*)" with "([^"]*)" and then answers "([^"]*)"$`, func(path, content, final string) error {
		w.real = true
		w.calls(final, [2]string{"write_file", jsonArgs("path", path, "content", content)})
		return nil
	})

	// ToolResults the Provider received.
	sc.Step(`^the second request (?:to the Provider )?contains a ToolResult (?:with the text|mentioning) "([^"]*)"$`, func(text string) error {
		return w.secondRequestHasResult("containing "+text, func(r string) bool { return strings.Contains(r, text) })
	})
	sc.Step(`^the second request contains a ToolResult starting with "([^"]*)"$`, func(prefix string) error {
		return w.secondRequestHasResult("starting with "+prefix, func(r string) bool { return strings.HasPrefix(r, prefix) })
	})
	sc.Step(`^the second request contains a ToolResult "([^"]*)"$`, func(text string) error {
		return w.secondRequestHasResult("starting with "+text, func(r string) bool { return strings.HasPrefix(r, text) })
	})
	sc.Step(`^the Session history roles are ([a-z, ]+)$`, func(list string) error {
		if err := w.settle(); err != nil {
			return err
		}
		var got []string
		for _, m := range w.ag.History() {
			got = append(got, string(m.Role))
		}
		if want := strings.Join(strings.Split(list, ", "), ","); strings.Join(got, ",") != want {
			return fmt.Errorf("history roles = %v, want %s", got, want)
		}
		return nil
	})
	sc.Step(`^the request to the Provider offers no Tool named "([^"]*)"$`, func(name string) error {
		if err := w.settle(); err != nil {
			return err
		}
		req, err := w.request(0)
		if err != nil {
			return err
		}
		offered, _ := req["tools"].([]any)
		if len(offered) == 0 {
			return errors.New("the request offers no Tools at all")
		}
		for _, t := range offered {
			fn, _ := t.(map[string]any)["function"].(map[string]any)
			if fn["name"] == name {
				return fmt.Errorf("the Tool %q is offered", name)
			}
		}
		return nil
	})

	// Files.
	sc.Step(`^the file "([^"]*)" contains "([^"]*)"$`, func(name, text string) error {
		got, err := w.readFile(name)
		if err != nil {
			return err
		}
		if !strings.Contains(got, text) {
			return fmt.Errorf("file %s = %q, want it to contain %q", name, got, text)
		}
		return nil
	})
	sc.Step(`^the file "([^"]*)" is unchanged$`, func(name string) error {
		got, err := w.readFile(name)
		if err != nil {
			return err
		}
		if got != w.files[name] {
			return fmt.Errorf("file %s = %q, want %q", name, got, w.files[name])
		}
		return nil
	})
	sc.Step(`^the file "([^"]*)" does not exist$`, func(name string) error {
		if _, err := w.readFile(name); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("file %s: error = %v, want not exist", name, err)
		}
		return nil
	})
	sc.Step(`^the file "([^"]*)" still has Windows line endings$`, func(name string) error {
		got, err := w.readFile(name)
		if err != nil {
			return err
		}
		if !strings.Contains(got, "\r\n") || strings.Count(got, "\n") != strings.Count(got, "\r\n") {
			return fmt.Errorf("file %s = %q, want only CRLF line endings", name, got)
		}
		return nil
	})
}

// waitChan waits for ch; the deadline only fails the scenario.
func waitChan(what string, ch <-chan struct{}) error {
	select {
	case <-ch:
		return nil
	case <-time.After(waitDeadline):
		return fmt.Errorf("timed out waiting for %s", what)
	}
}
