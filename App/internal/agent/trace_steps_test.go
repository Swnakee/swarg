package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/trace"
)

// ---------------------------------------------------------------------------
// Steps for trace.feature: the real trace.Writer in a temp dir, read back as JSONL.
// ---------------------------------------------------------------------------

// traceWorld is the Trace side of a scenario; nil in scenarios that do not mention it.
type traceWorld struct {
	on  bool // record to a real file
	bad bool // a Trace that cannot be written
	w   *trace.Writer
}

func (w *world) traces() *traceWorld {
	if w.trc == nil {
		w.trc = &traceWorld{}
	}
	return w.trc
}

func (w *world) tracePath() string { return filepath.Join(w.root, "traces", "t.trace.jsonl") }

// brokenTrace stands for a Trace whose file cannot be written: it drops records and reports the error.
type brokenTrace struct{}

func (brokenTrace) Record(string, any) {}
func (brokenTrace) Err() error         { return errors.New("no space left on device") }

// plugTrace wires the Trace into the Deps; called by begin.
func (w *world) plugTrace(d *Deps) error {
	t := w.trc
	switch {
	case t == nil:
	case t.bad:
		d.Trace = brokenTrace{}
	case t.on:
		tw, err := trace.Open(w.tracePath())
		if err != nil {
			return err
		}
		w.tb.Cleanup(func() { tw.Close() })
		t.w, d.Trace = tw, tw
	}
	return nil
}

// records settles the Agent and reads the trace file.
func (w *world) records(kind string) ([]traceRec, error) {
	if err := w.settle(); err != nil {
		return nil, err
	}
	f, err := os.Open(w.tracePath())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []traceRec
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 64<<20)
	for sc.Scan() {
		var e struct {
			Kind string
			D    map[string]any
		}
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return nil, err
		}
		if kind == "" || e.Kind == kind {
			out = append(out, traceRec{e.Kind, e.D})
		}
	}
	return out, sc.Err()
}

// untracedRequests runs the scenario again in a fresh world without a Trace and returns its
// request messages (without the system prompt, which names the temp dir).
func (w *world) untracedRequests() ([][]map[string]any, error) {
	w2 := &world{tb: w.tb, root: suiteT.TempDir(), limit: w.limit, changed: make(chan struct{}),
		replies: slices.Clone(w.replies), real: w.real, callN: w.callN}
	for _, d := range []string{w2.globalDir(), w2.cwd()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	for name, text := range w.files {
		if err := w2.writeFile(name, text); err != nil {
			return nil, err
		}
	}
	if err := w2.begin(); err != nil {
		return nil, err
	}
	w.tb.Cleanup(w2.cancel)
	w2.submit(w.lastUser)
	return w2.messagesOfRequests(true)
}

// messagesOfRequests returns the messages of every request after the system prompt.
func (w *world) messagesOfRequests(settle bool) (out [][]map[string]any, err error) {
	if settle {
		if err := w.settle(); err != nil {
			return nil, err
		}
	}
	for i := range w.srv.Requests() {
		msgs, err := w.requestMessages(i)
		if err != nil {
			return nil, err
		}
		out = append(out, msgs[1:])
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Step definitions
// ---------------------------------------------------------------------------

func registerTraceSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the Trace is recorded$`, func() error { w.traces().on = true; return nil })
	sc.Step(`^tracing is switched off$`, func() error { return nil })
	sc.Step(`^a Trace that cannot be written$`, func() error { w.traces().bad = true; return nil })
	sc.Step(`^no Trace exists$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Dir(w.tracePath())); err == nil {
			return errors.New("a Trace directory exists")
		}
		return nil
	})
	sc.Step(`^the Trace holds the read_file ToolCall for "([^"]*)" with its full arguments and the capped output the Provider received$`, func(path string) error {
		starts, err := w.records("tool_start")
		if err != nil {
			return err
		}
		ends, err := w.records("tool_end")
		if err != nil {
			return err
		}
		if len(starts) != 1 || len(ends) != 1 {
			return fmt.Errorf("%d tool_start and %d tool_end records, want one each", len(starts), len(ends))
		}
		if got, want := starts[0].D["args"], jsonArgs("path", path); got != want {
			return fmt.Errorf("tool_start args = %v, want %v", got, want)
		}
		tools := w.toolMessages()
		if len(tools) != 1 || ends[0].D["output"] != tools[0].Content {
			return fmt.Errorf("tool_end output is not the ToolResult the Provider received")
		}
		if ends[0].D["capped"] != true || !strings.Contains(tools[0].Content, "bytes cut") {
			return fmt.Errorf("tool_end is not marked capped: %v", ends[0].D["capped"])
		}
		return nil
	})
	sc.Step(`^the request records hold the user Message, then the assistant Message with its ToolResult$`, func() error {
		reqs, err := w.records("request")
		if err != nil {
			return err
		}
		want := [][]string{{"user"}, {"assistant", "tool:" + w.firstCallID()}}
		if len(reqs) != len(want) {
			return fmt.Errorf("%d request records, want %d", len(reqs), len(want))
		}
		for i := range want {
			if got := addedOf(reqs[i]); !reflect.DeepEqual(got, want[i]) {
				return fmt.Errorf("request record %d added %v, want %v", i+1, got, want[i])
			}
		}
		return nil
	})
	sc.Step(`^the Trace holds a usage record with (\d+) prompt and (\d+) cached tokens$`, func(p, c int) error {
		us, err := w.records("usage")
		if err != nil {
			return err
		}
		if len(us) != 1 || us[0].D["prompt"] != float64(p) || us[0].D["cached"] != float64(c) {
			return fmt.Errorf("usage records %+v", us)
		}
		return nil
	})
	sc.Step(`^the Trace holds a stub batch naming the read_file ToolResult$`, func() error {
		bs, err := w.records("stub_batch")
		if err != nil {
			return err
		}
		tools := w.toolMessages()
		if len(bs) != 1 || len(tools) != 1 {
			return fmt.Errorf("%d stub batches, %d ToolResults", len(bs), len(tools))
		}
		if got := bs[0].D["stubbed"]; !reflect.DeepEqual(got, []any{float64(tools[0].Seq)}) {
			return fmt.Errorf("stub batch names %v, want the read_file ToolResult %d", got, tools[0].Seq)
		}
		return nil
	})
	sc.Step(`^the Trace holds one reply record with the text "([^"]*)"$`, func(text string) error {
		rs, err := w.records("delta")
		if err != nil {
			return err
		}
		if len(rs) != 1 || rs[0].D["text"] != text {
			return fmt.Errorf("reply records %+v", rs)
		}
		return nil
	})
	sc.Step(`^the requests are identical to the requests of a run with tracing off$`, func() error {
		with, err := w.messagesOfRequests(true)
		if err != nil {
			return err
		}
		without, err := w.untracedRequests()
		if err != nil {
			return err
		}
		if len(with) == 0 || !reflect.DeepEqual(with, without) {
			return fmt.Errorf("requests differ:\nwith trace: %v\nwithout:    %v", with, without)
		}
		return nil
	})
}

// firstCallID is the id of the first ToolCall in the history.
func (w *world) firstCallID() string {
	for _, m := range w.ag.History() {
		for _, c := range m.ToolCalls {
			return c.ID
		}
	}
	return ""
}
