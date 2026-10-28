package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Steps for context_economy.feature: scripted ToolCalls of the real tools, and
// readers of what the Provider received (the wire Messages of every request).
// ---------------------------------------------------------------------------

// lineText is the text of line i of a generated file.
func lineText(name string, i int) string {
	return fmt.Sprintf("line %d of %s: the quick brown fox jumps over the lazy dog", i, name)
}

// linesOf returns n lines for a generated file, one per line, newline terminated.
func linesOf(name string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString(lineText(name, i) + "\n")
	}
	return b.String()
}

// send records the raw args of a call under its Tool and returns the call.
func (w *world) send(tool, args string) [2]string {
	if w.sent == nil {
		w.sent = map[string]string{}
	}
	w.sent[tool] = args
	return [2]string{tool, args}
}

// answers scripts the final answer of the first Turn and the answer of the next one.
func (w *world) answers(first, second string) {
	w.reply(llmtest.Reply{Chunks: []string{first}})
	if second != "" {
		w.reply(llmtest.Reply{Chunks: []string{second}})
	}
}

// step scripts one reply that makes the calls; the answers come last.
func (w *world) step(calls ...[2]string) {
	w.real = true
	w.reply(w.toolReply(calls...))
}

func readArgsOf(path string) [2]string { return [2]string{"read_file", jsonArgs("path", path)} }

// editOf edits the first generated line of a file.
func editOf(path string) [2]string {
	return [2]string{"edit_file", jsonArgs("path", path, "old", fmt.Sprintf("line 1 of %s:", path), "new", fmt.Sprintf("line one of %s:", path))}
}

// printLines is a shell command that prints n lines "line i" and exits with code.
func printLines(n, code int) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("1..%d | ForEach-Object { \"line $_\" }; exit %d", n, code)
	}
	return fmt.Sprintf("i=1; while [ $i -le %d ]; do echo \"line $i\"; i=$((i+1)); done; exit %d", n, code)
}

// ---------------------------------------------------------------------------
// Reading the requests
// ---------------------------------------------------------------------------

// wireRequests returns the Messages of every request the Provider received.
func (w *world) wireRequests() ([][]map[string]any, error) {
	if err := w.settle(); err != nil {
		return nil, err
	}
	var out [][]map[string]any
	for i := range w.srv.Requests() {
		msgs, err := w.requestMessages(i)
		if err != nil {
			return nil, err
		}
		out = append(out, msgs)
	}
	return out, nil
}

// wirePrefix reports whether a is a prefix of b.
func wirePrefix(a, b []map[string]any) bool {
	return len(a) <= len(b) && reflect.DeepEqual(a, b[:len(a)])
}

// firstBatch returns the index of the first request that is not an append to the one before.
func firstBatch(reqs [][]map[string]any) (int, bool) {
	for n := 1; n < len(reqs); n++ {
		if !wirePrefix(reqs[n-1], reqs[n]) {
			return n, true
		}
	}
	return 0, false
}

// wireResults returns the content of the tool Messages of one request.
func wireResults(msgs []map[string]any) (out []string) {
	for _, m := range msgs {
		if m["role"] == "tool" {
			s, _ := m["content"].(string)
			out = append(out, s)
		}
	}
	return out
}

// named is a ToolResult of a request with the Tool it answers and its history Message.
type named struct {
	name, content string
	history       chat.Message
}

// lastResults pairs the tool Messages of the last request with the history, in order.
func (w *world) lastResults() ([]named, error) {
	reqs, err := w.wireRequests()
	if err != nil {
		return nil, err
	}
	got := wireResults(reqs[len(reqs)-1])
	hist := w.toolMessages()
	if len(got) > len(hist) {
		return nil, fmt.Errorf("the last request has %d ToolResults, the history %d", len(got), len(hist))
	}
	out := make([]named, len(got))
	for i, c := range got {
		out[i] = named{name: hist[i].Name, content: c, history: hist[i]}
	}
	return out, nil
}

// resultOf returns the first (or last) ToolResult of a Tool in the last request.
func (w *world) resultOf(tool string, last bool) (named, error) {
	rs, err := w.lastResults()
	if err != nil {
		return named{}, err
	}
	var found []named
	for _, r := range rs {
		if r.name == tool {
			found = append(found, r)
		}
	}
	if len(found) == 0 {
		return named{}, fmt.Errorf("the last request has no ToolResult of %q", tool)
	}
	if last {
		return found[len(found)-1], nil
	}
	return found[0], nil
}

// lastCallArgs returns the arguments of the first ToolCall of a Tool in the last request.
func (w *world) lastCallArgs(tool string) (string, error) {
	reqs, err := w.wireRequests()
	if err != nil {
		return "", err
	}
	for _, m := range reqs[len(reqs)-1] {
		calls, _ := m["tool_calls"].([]any)
		for _, c := range calls {
			fn, _ := c.(map[string]any)["function"].(map[string]any)
			if fn["name"] == tool {
				s, _ := fn["arguments"].(string)
				return s, nil
			}
		}
	}
	return "", fmt.Errorf("the last request has no ToolCall of %q", tool)
}

// ---------------------------------------------------------------------------
// Step definitions
// ---------------------------------------------------------------------------

func registerEconomySteps(sc *godog.ScenarioContext, w *world) {
	registerEconomyGivens(sc, w)
	registerEconomyScripts(sc, w)
	registerEconomyThens(sc, w)
	registerEconomyResults(sc, w)
}

func registerEconomyGivens(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a file "([^"]*)" with (\d+) lines$`, func(name string, n int) error {
		w.real = true
		return w.writeFile(name, linesOf(name, n))
	})
	sc.Step(`^a Provider that reads (\d+) (small|large) files in turn and then answers "([^"]*)"$`, func(n int, size, final string) error {
		lines := 3
		if size == "large" {
			lines = 150
		}
		for i := 1; i <= n; i++ {
			name := fmt.Sprintf("%s%d.txt", size, i)
			if err := w.writeFile(name, linesOf(name, lines)); err != nil {
				return err
			}
			w.step(readArgsOf(name))
		}
		w.answers(final, "")
		return nil
	})
	sc.Step(`^a Provider that reads a missing file, then reads an existing one and then answers "([^"]*)"$`, func(final string) error {
		if err := w.writeFile("a.go", linesOf("a.go", 3)); err != nil {
			return err
		}
		w.step(readArgsOf("missing.go"))
		w.step(readArgsOf("a.go"))
		w.answers(final, "")
		return nil
	})
	sc.Step(`^a Provider that searches "([^"]*)" (\d+) times in turn and then answers "([^"]*)"$`, func(path string, n int, final string) error {
		for i := 0; i < n; i++ {
			// a unique pattern per call (all match every "line N") so the calls differ: the
			// repeat guard (R-24) stops identical repeated calls, which these must not be.
			w.step([2]string{"grep", jsonArgs("pattern", fmt.Sprintf("line|q%d", i), "path", path)})
		}
		w.answers(final, "")
		return nil
	})
	sc.Step(`^a Provider whose answer reports (\d+) prompt tokens of which (\d+) were cached$`, func(p, c int) error {
		w.reply(cachedReply(p, c))
		return nil
	})
	sc.Step(`^a Provider whose answer reports (\d+) prompt tokens and no cached tokens$`, func(p int) error {
		w.reply(cachedReply(p, 0))
		return nil
	})
	sc.Step(`^a Provider whose answer reports (\d+) prompt tokens of which (\d+) were cached and then fails with a server error$`, func(p, c int) error {
		w.reply(cachedReply(p, c))
		w.reply(llmtest.Reply{Status: 500})
		return nil
	})
}

// cachedReply answers "ok" with a usage chunk that reports cached prompt tokens.
func cachedReply(prompt, cached int) llmtest.Reply {
	return llmtest.Reply{Chunks: []string{"ok"}, Usage: &chat.Usage{PromptTokens: prompt, CompletionTokens: 5, CachedTokens: cached}}
}

// registerEconomyScripts registers the "<script> and answers X, and then answers Y" Providers.
func registerEconomyScripts(sc *godog.ScenarioContext, w *world) {
	const tail = ` and answers "([^"]*)", and then answers "([^"]*)"$`
	sc.Step(`^a Provider that reads "([^"]*)"`+tail, func(path, a, b string) error {
		w.step(readArgsOf(path))
		w.answers(a, b)
		return nil
	})
	sc.Step(`^a Provider that reads "([^"]*)" twice`+tail, func(path, a, b string) error {
		w.step(readArgsOf(path))
		w.step(readArgsOf(path))
		w.answers(a, b)
		return nil
	})
	sc.Step(`^a Provider that reads "([^"]*)", edits it`+tail, func(path, a, b string) error {
		w.step(readArgsOf(path))
		w.step(editOf(path))
		w.answers(a, b)
		return nil
	})
	sc.Step(`^a Provider that reads "([^"]*)", then edits "([^"]*)"`+tail, func(read, edit, a, b string) error {
		w.step(readArgsOf(read))
		w.step([2]string{"edit_file", jsonArgs("path", edit, "old", "package b", "new", "package c")})
		w.answers(a, b)
		return nil
	})
	sc.Step(`^a Provider that reads "([^"]*)", then edits it with a text that is not in the file`+tail, func(path, a, b string) error {
		w.step(readArgsOf(path))
		w.step([2]string{"edit_file", jsonArgs("path", path, "old", "text that is nowhere", "new", "x")})
		w.answers(a, b)
		return nil
	})
	sc.Step(`^a Provider that writes "([^"]*)" with (\d+) lines`+tail, func(path string, n int, a, b string) error {
		w.step(w.send("write_file", jsonArgs("path", path, "content", linesOf(path, n))))
		w.answers(a, b)
		return nil
	})
	sc.Step(`^a Provider that writes "([^"]*)" with 3 short lines`+tail, func(path, a, b string) error {
		w.step(w.send("write_file", jsonArgs("path", path, "content", "a\nb\nc\n")))
		w.answers(a, b)
		return nil
	})
	sc.Step(`^a Provider that rewrites "([^"]*)" with (\d+) other lines`+tail, func(path string, n int, a, b string) error {
		w.step(w.send("edit_file", jsonArgs("path", path, "old", w.files[path], "new", linesOf("other", n))))
		w.answers(a, b)
		return nil
	})
	sc.Step(`^a Provider that runs the shell command "([^"]*)"`+tail, func(cmd, a, b string) error {
		w.step(w.send("shell", jsonArgs("command", cmd)))
		w.answers(a, b)
		return nil
	})
	sc.Step(`^a Provider that runs a shell command printing 40 lines and exiting with 1, and then answers "([^"]*)" and "([^"]*)"$`, func(a, b string) error {
		w.step([2]string{"shell", jsonArgs("command", printLines(40, 1))})
		w.answers(a, b)
		return nil
	})
}

func registerEconomyThens(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^every request starts with the Messages of the previous request$`, func() error {
		reqs, err := w.wireRequests()
		if err != nil {
			return err
		}
		if len(reqs) < 3 {
			return fmt.Errorf("%d requests, want a Turn of several Steps", len(reqs))
		}
		if n, ok := firstBatch(reqs); ok {
			return fmt.Errorf("request %d does not start with the Messages of request %d", n+1, n)
		}
		return nil
	})
	sc.Step(`^the last request carries all ToolResults in full$`, func() error {
		rs, err := w.lastResults()
		if err != nil {
			return err
		}
		if len(rs) != len(w.toolMessages()) || len(rs) == 0 {
			return fmt.Errorf("the last request carries %d of %d ToolResults", len(rs), len(w.toolMessages()))
		}
		for i, r := range rs {
			if r.content != r.history.Content {
				return fmt.Errorf("ToolResult %d (%s) is not in full: %.60q", i+1, r.name, r.content)
			}
		}
		return nil
	})
	sc.Step(`^the request after the batch carries a Stub for the oldest ToolResult and the last 2 ToolResults in full$`, func() error {
		reqs, err := w.wireRequests()
		if err != nil {
			return err
		}
		n, ok := firstBatch(reqs)
		if !ok {
			return errors.New("no request changed the earlier Messages: no batch ran")
		}
		got, hist := wireResults(reqs[n]), w.toolMessages()
		if len(got) < 3 {
			return fmt.Errorf("the batch request has %d ToolResults, want at least 3", len(got))
		}
		if !strings.HasPrefix(got[0], "[grep ") {
			return fmt.Errorf("the oldest ToolResult is %.60q, want a Stub", got[0])
		}
		for i := len(got) - 2; i < len(got); i++ {
			if got[i] != hist[i].Content {
				return fmt.Errorf("ToolResult %d of %d is not in full", i+1, len(got))
			}
		}
		return nil
	})
	sc.Step(`^the requests after the batch each start with the Messages of the request before them$`, func() error {
		reqs, err := w.wireRequests()
		if err != nil {
			return err
		}
		b, ok := firstBatch(reqs)
		if !ok || b+1 >= len(reqs) {
			return fmt.Errorf("batch found: %v, requests: %d; want a batch with Steps after it", ok, len(reqs))
		}
		for n := b + 1; n < len(reqs); n++ {
			if !wirePrefix(reqs[n-1], reqs[n]) {
				return fmt.Errorf("request %d does not start with request %d", n+1, n)
			}
		}
		return nil
	})
	sc.Step(`^no request carries a Stub$`, func() error {
		reqs, err := w.wireRequests()
		if err != nil {
			return err
		}
		hist := w.toolMessages()
		for n, req := range reqs {
			for i, c := range wireResults(req) {
				if c != hist[i].Content {
					return fmt.Errorf("request %d carries ToolResult %d as %.60q", n+1, i+1, c)
				}
			}
		}
		return nil
	})
}

func registerEconomyResults(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the ToolResult of "([^"]*)" in the last request is "([^"]*)"$`, func(tool, want string) error {
		r, err := w.resultOf(tool, true)
		if err != nil {
			return err
		}
		if r.content != want {
			return fmt.Errorf("ToolResult of %s = %q, want %q", tool, r.content, want)
		}
		return nil
	})
	sc.Step(`^the ToolResult of "([^"]*)" in the last request is its real output$`, func(tool string) error {
		r, err := w.resultOf(tool, true)
		if err != nil {
			return err
		}
		if r.content != tool+" done" && r.content != r.history.Content {
			return fmt.Errorf("ToolResult of %s = %q, want its real output", tool, r.content)
		}
		return nil
	})
	sc.Step(`^the ToolResult of "([^"]*)" in the last request starts with "([^"]*)"$`, func(tool, prefix string) error {
		r, err := w.resultOf(tool, true)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(r.content, prefix) {
			return fmt.Errorf("ToolResult of %s = %.80q, want it to start with %q", tool, r.content, prefix)
		}
		return nil
	})
	sc.Step(`^the ToolResult of "([^"]*)" in the Session history is longer than its Stub$`, func(tool string) error {
		if err := w.settle(); err != nil {
			return err
		}
		for _, m := range w.toolMessages() {
			if m.Name == tool {
				if m.Stub == "" || len(m.Content) <= len(m.Stub) {
					return fmt.Errorf("history ToolResult of %s: Content %d bytes, Stub %q", tool, len(m.Content), m.Stub)
				}
				return nil
			}
		}
		return fmt.Errorf("no ToolResult of %q in the history", tool)
	})
	sc.Step(`^the older ToolResult of "([^"]*)" in the last request says "([^"]*)"$`, func(tool, word string) error {
		r, err := w.resultOf(tool, false)
		if err != nil {
			return err
		}
		if !strings.Contains(r.content, word) {
			return fmt.Errorf("the older ToolResult of %s = %.80q, want %q in it", tool, r.content, word)
		}
		return nil
	})
	sc.Step(`^the ToolResult of "([^"]*)" in the last request does not say "([^"]*)"$`, func(tool, word string) error {
		r, err := w.resultOf(tool, false)
		if err != nil {
			return err
		}
		if strings.Contains(r.content, word) {
			return fmt.Errorf("ToolResult of %s = %.80q, must not say %q", tool, r.content, word)
		}
		return nil
	})
	sc.Step(`^the Stub of the shell ToolResult in the last request has the line "([^"]*)" and "([^"]*)" and not the line "([^"]*)"$`, func(last, status, first string) error {
		r, err := w.resultOf("shell", true)
		if err != nil {
			return err
		}
		lines := strings.Split(strings.ReplaceAll(r.content, "\r\n", "\n"), "\n")
		has := func(l string) bool {
			for _, x := range lines {
				if strings.TrimSpace(x) == l {
					return true
				}
			}
			return false
		}
		if !has(last) || !strings.Contains(r.content, status) || has(first) {
			return fmt.Errorf("shell Stub = %q, want line %q, %q and no line %q", r.content, last, status, first)
		}
		return nil
	})
	sc.Step(`^the last request contains the user Message "([^"]*)" and the assistant Message "([^"]*)" unchanged$`, func(user, answer string) error {
		reqs, err := w.wireRequests()
		if err != nil {
			return err
		}
		var u, a bool
		for _, m := range reqs[len(reqs)-1] {
			u = u || (m["role"] == "user" && m["content"] == user)
			a = a || (m["role"] == "assistant" && m["content"] == answer)
		}
		if !u || !a {
			return fmt.Errorf("user Message found: %v, assistant Message found: %v", u, a)
		}
		return nil
	})
	sc.Step(`^the (write_file|edit_file) ToolCall in the last request has valid JSON arguments for "([^"]*)" omitted from history with (\d+) lines$`, func(tool, path string, n int) error {
		raw, err := w.lastCallArgs(tool)
		if err != nil {
			return err
		}
		var args map[string]string
		if !json.Valid([]byte(raw)) || json.Unmarshal([]byte(raw), &args) != nil {
			return fmt.Errorf("arguments are not a valid JSON object of strings: %s", raw)
		}
		keys := []string{"content"}
		if tool == "edit_file" {
			keys = []string{"old", "new"}
		}
		want := fmt.Sprintf("(omitted from history: %d lines,", n)
		for _, k := range keys {
			if !strings.HasPrefix(args[k], want) {
				return fmt.Errorf("argument %q = %.80q, want it to start with %q", k, args[k], want)
			}
		}
		if args["path"] != path {
			return fmt.Errorf("path = %q, want %q", args["path"], path)
		}
		return nil
	})
	sc.Step(`^the (write_file|shell) ToolCall in the last request has the arguments it was sent with$`, func(tool string) error {
		raw, err := w.lastCallArgs(tool)
		if err != nil {
			return err
		}
		if raw != w.sent[tool] {
			return fmt.Errorf("arguments = %.100q, want the ones sent: %.100q", raw, w.sent[tool])
		}
		return nil
	})
	sc.Step(`^the meter shows a cache share of (\d+) percent$`, func(pct int) error {
		if err := w.settle(); err != nil {
			return err
		}
		var last *Event
		for _, e := range w.snapshotEvents() {
			if e.Kind == EvMeter {
				e := e
				last = &e
			}
		}
		if last == nil || !last.HasCache || last.CachePct != pct {
			return fmt.Errorf("last Meter = %+v, want a cache share of %d percent", last, pct)
		}
		return nil
	})
}
