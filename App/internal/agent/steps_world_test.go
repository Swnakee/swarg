package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
	"swarg/internal/llm/openai"
	"swarg/internal/prompt"
	"swarg/internal/tools"
)

// ---------------------------------------------------------------------------
// Scenario world: the real stack (llmtest server, openai client, agent, prompt
// set) driven by steps. Given steps record replies and actions; the first When
// builds everything and replays the actions in order.
// ---------------------------------------------------------------------------

// waitDeadline only fails a scenario; it is never the synchronization.
const waitDeadline = 10 * time.Second

// suiteT is the TestFeatures test; scenario servers and temp dirs hang off it.
var suiteT testing.TB

// scenarioTB gives llmtest.New a per-scenario Cleanup run by the After hook.
type scenarioTB struct {
	testing.TB
	cleanups []func()
}

func (s *scenarioTB) Cleanup(f func()) { s.cleanups = append(s.cleanups, f) }

type world struct {
	tb      *scenarioTB
	root    string // temp dir; global blocks in root/blocks, cwd is root/parent/work
	limit   int
	replies []llmtest.Reply
	gates   []chan struct{}
	actions []func() error // recorded by Given steps, replayed after the build

	srv    *llmtest.Server
	ag     *Agent
	set    *prompt.Set
	cancel context.CancelFunc

	mu      sync.Mutex
	events  []Event
	changed chan struct{} // closed and replaced on every event

	// tools features: the real tools.Set (real), or the probe, never both
	real           bool
	disabled       []string          // real Tools switched off in the config
	files          map[string]string // file name -> text written by a Given step
	probe          *probeTools
	jobs           *tools.Jobs // the real runner's BackgroundJobs
	bgWait         bool        // shell_output waits for its job to finish
	moved          bool        // result of the last Agent.Background call
	callN          int         // numbers the ToolCall ids of scripted replies
	maxSteps       int
	symlinkSkipped bool // symlinks unsupported on this system
	approve        func(context.Context, chat.ToolCall) Approval
	asked          atomic.Int32 // approval function calls
	health         func(context.Context) error
	healthRan      chan struct{} // closed when the health check returned

	sent      map[string]string // economy: raw arguments of the last scripted call per Tool
	lastUser  string
	snapshot  []chat.Message
	ses       *sessionWorld // sessions.feature
	trc       *traceWorld   // trace.feature
	cmp       *compactWorld // compaction.feature
	thk       *thinkWorld   // thinking.feature
	mdl       *modelWorld   // model_choice.feature, generation_speed.feature
	clearErr  error
	switchErr error

	skl    *skillsWorld   // skills.feature
	mc     *mcpWorld      // mcp.feature
	cf     *configWorld   // config.feature
	prv    *providerWorld // providers.feature
	listed []string       // providers.feature: the first Source's model list

	tasks   TaskLimits     // task_mode.feature: Deps.Tasks (zero = Tasks off)
	limits  map[string]int // context_limit.feature: Deps.LimitFor entries
	taskErr error          // the last SubmitTask result
}

func (w *world) globalDir() string { return filepath.Join(w.root, "blocks") }
func (w *world) parentDir() string { return filepath.Join(w.root, "parent") }
func (w *world) cwd() string       { return filepath.Join(w.parentDir(), "work") }

// registerWorldSteps resets the world around every scenario.
func registerWorldSteps(sc *godog.ScenarioContext, w *world) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*w = world{tb: &scenarioTB{TB: suiteT}, root: suiteT.TempDir(), limit: 16384, changed: make(chan struct{})}
		for _, d := range []string{w.globalDir(), w.cwd()} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				return ctx, err
			}
		}
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.releaseGates()
		if w.cancel != nil {
			w.cancel()
		}
		for i := len(w.tb.cleanups) - 1; i >= 0; i-- {
			w.tb.cleanups[i]()
		}
		return ctx, nil
	})
}

// ---------------------------------------------------------------------------
// Building and replaying
// ---------------------------------------------------------------------------

// reply records a scripted reply (before the build only).
func (w *world) reply(r llmtest.Reply) { w.replies = append(w.replies, r) }

// gated records a reply that sends chunks[:after] and then waits for release.
func (w *world) gated(chunks []string, after int) llmtest.Reply {
	g := make(chan struct{})
	w.gates = append(w.gates, g)
	return llmtest.Reply{Chunks: chunks, GateAfter: after, Gate: g}
}

// act records a step that runs after the build, in Given order.
func (w *world) act(f func() error) { w.actions = append(w.actions, f) }

// begin builds the stack and replays the recorded actions; it runs once.
func (w *world) begin() error {
	if w.ag != nil {
		return nil
	}
	var extra []prompt.Block // like main: the task block is in the SystemPrompt when Tasks are on
	if w.tasks.MaxDepth > 0 {
		extra = append(extra, prompt.Block{Name: "tool-task", Source: prompt.SourceTool, Order: 599, Text: TaskBlock})
	}
	extra = append(extra, w.plugSkills()...)
	extra = append(extra, w.plugMCP()...)
	set, _, err := prompt.Load(w.globalDir(), w.cwd(), prompt.Env{OS: "test", Shell: "sh", Dir: w.cwd(), Date: "2026-01-01", Model: "m"}, extra...)
	if err != nil {
		return err
	}
	w.set = set
	w.srv = llmtest.New(w.tb, w.replies...)
	for _, id := range w.listed {
		w.srv.Models = append(w.srv.Models, llmtest.Model{ID: id})
	}
	var ctx context.Context
	ctx, w.cancel = context.WithCancel(context.Background())
	deps := Deps{
		Provider:     openai.New(w.srv.URL, "", nil),
		SystemPrompt: set.Build,
		Model:        "m",
		MaxTokens:    100,
		ContextLimit: w.limit,
		OnEvent:      w.record,
		Approve:      w.approve,
		Health:       w.health,
		MaxSteps:     w.maxSteps,
		Tasks:        w.tasks,
	}
	if w.limits != nil {
		deps.LimitFor = func(model string) int {
			if l, ok := w.limits[model]; ok {
				return l
			}
			return w.limit
		}
	}
	if err := w.plugSessions(&deps); err != nil {
		return err
	}
	if err := w.plugTrace(&deps); err != nil {
		return err
	}
	w.plugCompaction(&deps)
	w.plugModel(&deps)
	w.plugThink(&deps)
	if tr, err := w.toolRunner(); err != nil {
		return err
	} else if tr != nil {
		deps.Tools = tr
	}
	w.ag = New(deps)
	w.ag.Start(ctx)
	for _, f := range w.actions {
		if err := f(); err != nil {
			return err
		}
	}
	return nil
}

// releaseGates lets every gated reply finish; safe to call twice.
func (w *world) releaseGates() {
	if w.probe != nil {
		w.probe.releaseAll()
	}
	for _, g := range w.gates {
		select {
		case <-g:
		default:
			close(g)
		}
	}
}

// settle finishes all gated replies and waits until the agent is idle.
func (w *world) settle() error {
	if err := w.begin(); err != nil {
		return err
	}
	w.releaseGates()
	w.ag.Wait()
	return nil
}

// submit queues a message; with no gated reply it also waits for the queue to drain.
func (w *world) submit(text string) {
	w.lastUser = text
	w.ag.Submit(text)
	if len(w.gates) == 0 && !w.probe.holdsAny() {
		w.ag.Wait()
	}
}

// ---------------------------------------------------------------------------
// Event log
// ---------------------------------------------------------------------------

func (w *world) record(e Event) {
	w.mu.Lock()
	w.events = append(w.events, e)
	close(w.changed)
	w.changed = make(chan struct{})
	w.mu.Unlock()
}

// waitFor blocks until pred holds for the event log; it fails after waitDeadline.
func (w *world) waitFor(what string, pred func([]Event) bool) error {
	deadline := time.After(waitDeadline)
	for {
		w.mu.Lock()
		ok, ch := pred(w.events), w.changed
		w.mu.Unlock()
		if ok {
			return nil
		}
		select {
		case <-ch:
		case <-deadline:
			return fmt.Errorf("timed out waiting for %s", what)
		}
	}
}

// countKind returns the number of events of a kind.
func countKind(evs []Event, k EventKind) (n int) {
	for _, e := range evs {
		if e.Kind == k {
			n++
		}
	}
	return n
}

// snapshotEvents returns a copy of the event log.
func (w *world) snapshotEvents() []Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Event(nil), w.events...)
}

// deltas returns all delta text shown so far.
func (w *world) deltas() string {
	var b strings.Builder
	for _, e := range w.snapshotEvents() {
		if e.Kind == EvDelta {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

// warnings returns the logged warning messages.
func (w *world) warnings() (out []string) {
	for _, e := range w.snapshotEvents() {
		if e.Kind == EvWarning {
			out = append(out, e.Text)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Assertion helpers
// ---------------------------------------------------------------------------

// lastAssistant returns the last assistant message of the history.
func (w *world) lastAssistant() (chat.Message, error) {
	h := w.ag.History()
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Role == chat.RoleAssistant {
			return h[i], nil
		}
	}
	return chat.Message{}, errors.New("no assistant Message in the history")
}

// lastTurnEnd returns the last TurnEnded event.
func (w *world) lastTurnEnd() (Event, error) {
	evs := w.snapshotEvents()
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == EvTurnEnded {
			return evs[i], nil
		}
	}
	return Event{}, errors.New("no TurnEnded event")
}

// systemPrompt returns the system message of request n (negative: from the end).
func (w *world) systemPrompt(n int) (string, error) {
	reqs := w.srv.Requests()
	if n < 0 {
		n += len(reqs)
	}
	if n < 0 || n >= len(reqs) {
		return "", fmt.Errorf("request %d does not exist (%d sent)", n, len(reqs))
	}
	msgs, _ := reqs[n]["messages"].([]any)
	if len(msgs) == 0 {
		return "", errors.New("request has no messages")
	}
	m, _ := msgs[0].(map[string]any)
	s, _ := m["content"].(string)
	return s, nil
}

// userTexts returns the last user message content of every request.
func (w *world) userTexts() (out []string) {
	for _, r := range w.srv.Requests() {
		msgs, _ := r["messages"].([]any)
		for i := len(msgs) - 1; i >= 0; i-- {
			m, _ := msgs[i].(map[string]any)
			if m["role"] == "user" {
				s, _ := m["content"].(string)
				out = append(out, s)
				break
			}
		}
	}
	return out
}
