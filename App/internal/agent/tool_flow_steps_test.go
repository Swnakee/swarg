package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// probeTools: a ToolRunner for the scheduling features. Every Tool is named by
// the scenario; Run reports <name> done. A Tool can be held until released
// (started/release channels), wait for the other Tools of its batch (barrier)
// or wait for another Tool to finish (after). The overlap counters tell how
// many Tools ran at once.
// ---------------------------------------------------------------------------

type probeKind struct{ readOnly, confirm bool }

type probeTools struct {
	mu       sync.Mutex
	kinds    map[string]probeKind
	order    []string
	started  map[string]chan struct{}
	release  map[string]chan struct{} // held Tools only
	finished map[string]chan struct{}
	after    map[string]string // Tool -> the Tool it waits for
	barrier  int               // > 0: each Tool waits until this many are running
	arrived  int
	allIn    chan struct{}
	cur, max int
	done     []string // finish order
	released bool

	detachable map[string]bool   // Tools that can move to the background
	reports    map[string]string // Tools that report this text while they run
	results    map[string]string // Tools that return this text instead of "<name> done"
	fails      map[string]bool   // Tools that end with an error
}

func newProbe() *probeTools {
	return &probeTools{kinds: map[string]probeKind{}, started: map[string]chan struct{}{}, release: map[string]chan struct{}{},
		finished: map[string]chan struct{}{}, after: map[string]string{}, allIn: make(chan struct{}),
		detachable: map[string]bool{}, reports: map[string]string{}, results: map[string]string{}, fails: map[string]bool{}}
}

// probe returns the world's probe, creating it on first use.
func (w *world) probeRunner() *probeTools {
	if w.probe == nil {
		w.probe = newProbe()
	}
	return w.probe
}

// add registers a Tool; held Tools block in Run until released.
func (p *probeTools) add(name string, readOnly, held bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kinds[name] = probeKind{readOnly: readOnly}
	p.order = append(p.order, name)
	p.started[name], p.finished[name] = make(chan struct{}), make(chan struct{})
	if held {
		p.release[name] = make(chan struct{})
	}
}

// holdsAny reports whether a Tool blocks until released; nil-safe.
func (p *probeTools) holdsAny() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.release) > 0
}

// releaseAll lets every held Tool finish; safe to call twice.
func (p *probeTools) releaseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.released {
		return
	}
	p.released = true
	for _, ch := range p.release {
		close(ch)
	}
}

// firstHeld returns the first held Tool.
func (p *probeTools) firstHeld() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, n := range p.order {
		if p.release[n] != nil {
			return n
		}
	}
	return ""
}

func (p *probeTools) Specs() []chat.ToolSpec {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []chat.ToolSpec
	for _, n := range p.order {
		out = append(out, chat.ToolSpec{Name: n, Description: n, Schema: []byte(`{"type":"object"}`)})
	}
	return out
}

func (p *probeTools) Kind(name string) (known, readOnly, confirm bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	k, ok := p.kinds[name]
	return ok, k.readOnly, k.confirm
}

var _ ToolRunner = (*probeTools)(nil)

func (p *probeTools) Detachable(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.detachable[name]
}

func (p *probeTools) Label(c chat.ToolCall) string { return c.Name }

func (p *probeTools) Run(ctx context.Context, c chat.ToolCall, h ToolHooks) (string, string, error) {
	p.enter(c.Name)
	defer p.leave(c.Name)
	p.mu.Lock()
	hold, barrier, after := p.release[c.Name], p.barrier, p.after[c.Name]
	report, result, fail := p.reports[c.Name], p.results[c.Name], p.fails[c.Name]
	var waitFor <-chan struct{}
	if after != "" {
		waitFor = p.finished[after]
	}
	p.mu.Unlock()
	if report != "" && h.Progress != nil {
		h.Progress(report)
	}
	switch {
	case hold != nil:
		select {
		case <-hold:
		case <-h.Detach: // nil unless the Tool is detachable
			return "moved to the background by the user", "background", nil
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	case barrier > 0:
		p.mu.Lock()
		p.arrived++
		if p.arrived == barrier {
			close(p.allIn)
		}
		p.mu.Unlock()
		if err := p.await(ctx, p.allIn, "the other Tools to start"); err != nil {
			return "", "", err
		}
	case waitFor != nil:
		if err := p.await(ctx, waitFor, after+" to finish"); err != nil {
			return "", "", err
		}
	default: // give a wrongly parallel scheduler the chance to overlap
		for i := 0; i < 100; i++ {
			runtime.Gosched()
		}
	}
	if fail {
		return "", "", errors.New(c.Name + " failed")
	}
	if result != "" {
		return result, result, nil
	}
	return c.Name + " done", c.Name, nil
}

// await waits for ch; the deadline only fails the Tool.
func (p *probeTools) await(ctx context.Context, ch <-chan struct{}, what string) error {
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(waitDeadline):
		return fmt.Errorf("timed out waiting for %s", what)
	}
}

func (p *probeTools) enter(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cur++
	p.max = max(p.max, p.cur)
	select {
	case <-p.started[name]:
	default:
		close(p.started[name])
	}
}

func (p *probeTools) leave(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cur--
	p.done = append(p.done, name)
	select {
	case <-p.finished[name]:
	default:
		close(p.finished[name])
	}
}

// waitStarted blocks until the Tool is inside Run.
func (p *probeTools) waitStarted(name string) error {
	p.mu.Lock()
	ch := p.started[name]
	p.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("no probe Tool %q", name)
	}
	return waitChan("the Tool "+name+" to start", ch)
}

// stats returns the highest number of Tools that ran at once and the finish order.
func (p *probeTools) stats() (int, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.max, append([]string(nil), p.done...)
}

// ---------------------------------------------------------------------------
// Steps
// ---------------------------------------------------------------------------

// quoted splits a list like `"a", "b", "c"` into its names.
func quoted(list string) (out []string) {
	for _, s := range strings.Split(list, `"`) {
		if s = strings.Trim(s, ", "); s != "" && s != "and" {
			out = append(out, s)
		}
	}
	return out
}

// callAll scripts one answer that calls every named probe Tool, then a final text.
func (w *world) callAll(final string, names ...string) {
	var cs [][2]string
	for _, n := range names {
		cs = append(cs, [2]string{n, "{}"})
	}
	w.calls(final, cs...)
}

func registerToolFlowSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Provider that calls three read-only Tools in one answer$`, func() error {
		p := w.probeRunner()
		for _, n := range []string{"r1", "r2", "r3"} {
			p.add(n, true, false)
		}
		p.barrier = 3
		w.callAll("ok", "r1", "r2", "r3")
		return nil
	})
	sc.Step(`^a Provider that calls three mutating Tools in one answer$`, func() error {
		p := w.probeRunner()
		for _, n := range []string{"m1", "m2", "m3"} {
			p.add(n, false, false)
		}
		w.callAll("ok", "m1", "m2", "m3")
		return nil
	})
	sc.Step(`^a Provider that calls the mutating Tools ((?:"[^"]*"(?:, | and )?)+) in one answer$`, func(list string) error {
		p := w.probeRunner()
		names := quoted(list)
		for _, n := range names {
			p.add(n, false, false)
		}
		w.callAll("ok", names...)
		return nil
	})
	sc.Step(`^a Provider that calls the read-only Tools "slow" and "fast" in one answer$`, func() error {
		p := w.probeRunner()
		p.add("slow", true, false)
		p.add("fast", true, false)
		p.after["slow"] = "fast" // slow can only finish once fast has: the results must still keep call order
		w.callAll("ok", "slow", "fast")
		return nil
	})
	sc.Step(`^a Provider that calls a blocked Tool(?: and then answers "([^"]*)")?$`, func(final string) error {
		w.probeRunner().add("blk", true, true)
		w.callAll(final, "blk")
		return nil
	})
	sc.Step(`^a Provider that calls the blocked mutating Tools "([^"]*)" and "([^"]*)" in one answer$`, func(a, b string) error {
		p := w.probeRunner()
		p.add(a, false, true)
		p.add(b, false, true)
		w.callAll("ok", a, b)
		return nil
	})
	sc.Step(`^a Provider that calls the blocked read-only Tool "([^"]*)" and then answers "([^"]*)"$`, func(name, final string) error {
		w.probeRunner().add(name, true, true)
		w.callAll(final, name)
		return nil
	})
	sc.Step(`^the user submitted "([^"]*)" and the Tool(?: "([^"]*)")? is running$`, func(text, name string) error {
		w.act(func() error {
			if name == "" {
				name = w.probe.firstHeld()
			}
			w.submit(text)
			return w.probe.waitStarted(name)
		})
		return nil
	})

	// Step limit.
	sc.Step(`^the step limit is (\d+) and a Provider that always calls a Tool$`, func(n int) error {
		w.maxSteps = n
		w.probeRunner().add("ping", true, false)
		for i := 0; i <= n; i++ {
			w.callAll("", "ping")
		}
		return nil
	})
	sc.Step(`^the step limit is (\d+) and a Provider that calls a Tool once and then answers "([^"]*)"$`, func(n int, final string) error {
		w.maxSteps = n
		w.probeRunner().add("ping", true, false)
		w.callAll(final, "ping")
		return nil
	})
	sc.Step(`^a warning "([^"]*)" is shown$`, func(text string) error {
		return w.wantWarning(func(s string) bool { return s == text })
	})
	sc.Step(`^a warning starting with "([^"]*)" is shown$`, func(prefix string) error {
		return w.wantWarning(func(s string) bool { return strings.HasPrefix(s, prefix) })
	})
	sc.Step(`^no warning is shown$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if got := w.warnings(); len(got) != 0 {
			return fmt.Errorf("warnings = %v, want none", got)
		}
		return nil
	})
	sc.Step(`^every ToolCall in the Session history has a ToolResult$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		return validToolHistory(w.ag.History())
	})

	// Scheduling results.
	sc.Step(`^the three Tools were running at the same time$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if m, _ := w.probe.stats(); m != 3 {
			return fmt.Errorf("at most %d Tools ran at once, want 3", m)
		}
		return nil
	})
	sc.Step(`^at most one Tool was running at any time$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if m, done := w.probe.stats(); m != 1 || len(done) != 3 {
			return fmt.Errorf("max running %d, finished %v; want 1 and 3 Tools", m, done)
		}
		return nil
	})
	sc.Step(`^the Tools finished in the order ((?:[a-z]+(?:, )?)+)$`, func(list string) error {
		if err := w.settle(); err != nil {
			return err
		}
		want := strings.Split(list, ", ")
		if _, done := w.probe.stats(); !reflect.DeepEqual(done, want) {
			return fmt.Errorf("finish order = %v, want %v", done, want)
		}
		return nil
	})
	sc.Step(`^the ToolResults in the history are in the order ((?:[a-z]+(?:, )?)+)$`, func(list string) error {
		if err := w.settle(); err != nil {
			return err
		}
		var names []string
		for _, m := range w.toolMessages() {
			if m.Content != m.Name+" done" {
				return fmt.Errorf("ToolResult of %s = %q", m.Name, m.Content)
			}
			names = append(names, m.Name)
		}
		if want := strings.Split(list, ", "); !reflect.DeepEqual(names, want) {
			return fmt.Errorf("ToolResults in the order %v, want %v", names, want)
		}
		return nil
	})
	sc.Step(`^the ToolResult of "([^"]*)" is "([^"]*)"$`, func(name, want string) error {
		return w.wantToolResult(name, want)
	})
	sc.Step(`^the ToolResult of "([^"]*)" is its real output$`, func(name string) error {
		return w.wantToolResult(name, name+" done")
	})
	sc.Step(`^the second request ends with the ToolResult followed by the user Message "([^"]*)"$`, func(text string) error {
		if err := w.settle(); err != nil {
			return err
		}
		msgs, err := w.requestMessages(1)
		if err != nil {
			return err
		}
		n := len(msgs)
		if n < 2 || msgs[n-2]["role"] != "tool" || msgs[n-1]["role"] != "user" || msgs[n-1]["content"] != text {
			return fmt.Errorf("second request ends with %v, want a tool message and the user Message %q", msgs[max(0, n-2):], text)
		}
		return nil
	})
	sc.Step(`^exactly one Turn ran$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		evs := w.snapshotEvents()
		if s, e := countKind(evs, EvTurnStarted), countKind(evs, EvTurnEnded); s != 1 || e != 1 {
			return fmt.Errorf("%d Turns started and %d ended, want 1", s, e)
		}
		return nil
	})
}

// wantToolResult settles the Session and checks the result of the call to the named Tool.
func (w *world) wantToolResult(name, want string) error {
	if err := w.settle(); err != nil {
		return err
	}
	for _, m := range w.toolMessages() {
		if name == "" || m.Name == name {
			if m.Content != want {
				return fmt.Errorf("ToolResult of %s = %q, want %q", m.Name, m.Content, want)
			}
			return nil
		}
	}
	return fmt.Errorf("no ToolResult of %q in %v", name, w.ag.History())
}

// wantWarning waits for a warning accepted by ok (the Health warning arrives late).
func (w *world) wantWarning(ok func(string) bool) error {
	if err := w.begin(); err != nil {
		return err
	}
	return w.waitFor("a warning", func(evs []Event) bool {
		for _, e := range evs {
			if e.Kind == EvWarning && ok(e.Text) {
				return true
			}
		}
		return false
	})
}

// validToolHistory checks I-3: every assistant ToolCall is answered by exactly one tool Message.
func validToolHistory(h []chat.Message) error {
	answered := map[string]int{}
	for _, m := range h {
		if m.Role == chat.RoleTool {
			answered[m.ToolCallID]++
		}
	}
	for _, m := range h {
		for _, c := range m.ToolCalls {
			if answered[c.ID] != 1 {
				return fmt.Errorf("ToolCall %s has %d ToolResults", c.ID, answered[c.ID])
			}
			delete(answered, c.ID)
		}
	}
	if len(answered) > 0 {
		return fmt.Errorf("ToolResults without a ToolCall: %v", answered)
	}
	return nil
}
