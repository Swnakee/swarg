package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
	"swarg/internal/llm/openai"
)

// ---------------------------------------------------------------------------
// Steps for compaction.feature. Summary requests go to their own fake server (a request with
// no tools and the fixed prompt is routed there), so the replies of Turns and of summaries are
// scripted separately; a laneProvider around both records the order of requests.
// ---------------------------------------------------------------------------

// compactWorld is the Compaction side of a scenario; nil in scenarios that do not mention it.
type compactWorld struct {
	auto    int             // Deps.AutoCompact
	sum     []llmtest.Reply // scripted replies of the summary server, in order
	srv     *llmtest.Server
	lane    *laneProvider
	before  []chat.Message // the history when the user compacted
	asked   bool
	turnsOf int // Turns scripted by "next N Turns" steps, for numbering their users
}

func (w *world) compaction() *compactWorld {
	if w.cmp == nil {
		w.cmp = &compactWorld{}
	}
	return w.cmp
}

// routeProvider sends summary requests to the summary server and the rest to the Turn server.
type routeProvider struct{ turn, sum Provider }

func (r routeProvider) Stream(ctx context.Context, req chat.Request) (<-chan chat.StreamEvent, error) {
	if isSummaryRequest(req) {
		return r.sum.Stream(ctx, req)
	}
	return r.turn.Stream(ctx, req)
}

// plugCompaction wires the routing, the lane recorder and AutoCompact; called by begin.
func (w *world) plugCompaction(d *Deps) {
	c := w.cmp
	if c == nil {
		return
	}
	c.srv = llmtest.New(w.tb, c.sum...)
	d.Provider = routeProvider{turn: d.Provider, sum: openai.New(c.srv.URL, "", nil)}
	withLane(&c.lane)(d)
	d.AutoCompact = c.auto
}

// sumTimes scripts n copies of a summary reply.
func (c *compactWorld) sumTimes(n int, r llmtest.Reply) {
	for i := 0; i < n; i++ {
		c.sum = append(c.sum, r)
	}
}

// ---------------------------------------------------------------------------
// Scripting Turns
// ---------------------------------------------------------------------------

// workStep scripts one Step: a note of noteBytes, then the call.
func (w *world) workStep(noteBytes int, call [2]string) {
	w.real = true
	r := w.toolReply(call)
	if noteBytes > 0 {
		r.Chunks = []string{noteText(w.callN, noteBytes)}
	}
	w.reply(r)
}

// userTurn runs "work N" after the build and waits until the Agent is idle.
func (w *world) userTurn(text string) {
	w.act(func() error {
		w.lastUser = text
		w.ag.Submit(text)
		w.ag.Wait()
		w.snapshot = w.ag.History()
		return nil
	})
}

func (w *world) scriptSession(turns int, kind string) error {
	for t := 1; t <= turns; t++ {
		a, b, c := fmt.Sprintf("a%d.go", t), fmt.Sprintf("b%d.go", t), fmt.Sprintf("c%d.go", t)
		switch kind {
		case "Tools", "a read_file Tool":
			if err := w.writeFile(a, "x"); err != nil {
				return err
			}
			w.workStep(3000, readArgsOf(a))
		case "three Tools":
			for _, f := range []string{a, b, c} {
				if err := w.writeFile(f, "x"); err != nil {
					return err
				}
				w.workStep(1200, readArgsOf(f))
			}
		case "a read_file and an edit_file Tool":
			if err := w.writeFile(a, linesOf(a, 3)); err != nil {
				return err
			}
			w.workStep(1500, readArgsOf(a))
			w.workStep(1500, editOf(a))
		case "three small edits":
			for _, f := range []string{a, b, c} {
				text := strings.Repeat("e", 400)
				if err := w.writeFile(f, text); err != nil {
					return err
				}
				w.workStep(0, w.send("edit_file", jsonArgs("path", f, "old", text, "new", "b")))
			}
		case "a small read_file Tool":
			if err := w.writeFile(a, "x"); err != nil {
				return err
			}
			w.workStep(0, readArgsOf(a))
		default:
			return fmt.Errorf("unknown Tool set %q", kind)
		}
		w.reply(llmtest.Reply{Chunks: []string{"Done"}})
		w.userTurn(fmt.Sprintf("work %d", t))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Actions and readers
// ---------------------------------------------------------------------------

// compactNow remembers the history and asks for a Compaction; it waits for the end unless a
// gated reply is pending.
func (w *world) compactNow() error {
	c := w.compaction()
	if err := w.begin(); err != nil {
		return err
	}
	c.before, c.asked = w.ag.History(), true
	if err := w.ag.Compact(context.Background()); err != nil {
		return err
	}
	if len(w.gates) == 0 {
		w.ag.Wait()
	}
	return nil
}

func (w *world) summaryRequests() (int, error) {
	if err := w.settle(); err != nil {
		return 0, err
	}
	return len(w.compaction().srv.Requests()), nil
}

func (w *world) workSummaries() ([]chat.Message, error) {
	if err := w.settle(); err != nil {
		return nil, err
	}
	return summaries(w.ag.History()), nil
}

// finalsAndUsers lists the user Messages and the final answers (assistant, no calls, not a summary).
func finalsAndUsers(h []chat.Message) (out []chat.Message) {
	for _, m := range h {
		if m.Role == chat.RoleUser || (m.Role == chat.RoleAssistant && len(m.ToolCalls) == 0 && !m.Summary) {
			out = append(out, m)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Step definitions
// ---------------------------------------------------------------------------

func registerCompactionSteps(sc *godog.ScenarioContext, w *world) {
	registerCompactionGivens(sc, w)
	sc.Step(`^the user compacts the Session$`, w.compactNow)
	registerCompactionThens(sc, w)
	registerAutoCompactionSteps(sc, w)
}

func registerCompactionGivens(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Session with (one|two) finished Turns? that used (a read_file Tool|three Tools|a read_file and an edit_file Tool|three small edits|a small read_file Tool|Tools)$`,
		func(n, kind string) error {
			w.compaction()
			turns := 1
			if n == "two" {
				turns = 2
			}
			return w.scriptSession(turns, kind)
		})
	sc.Step(`^a Session with one finished Turn whose Tool results are larger than half the limit$`, func() error {
		w.compaction()
		for i := 1; i <= 6; i++ {
			name := fmt.Sprintf("big%d.txt", i)
			if err := w.writeFile(name, linesOf(name, 150)); err != nil {
				return err
			}
			w.workStep(500, readArgsOf(name))
		}
		w.reply(llmtest.Reply{Chunks: []string{"Done"}})
		w.userTurn("work 1")
		return nil
	})
	sc.Step(`^a Provider that summarizes each WorkSpan as "([^"]*)"$`, func(text string) error {
		w.compaction().sumTimes(20, llmtest.Reply{Chunks: []string{text}})
		return nil
	})
	sc.Step(`^a Provider that summarizes each WorkSpan with a very long text$`, func() error {
		w.compaction().sumTimes(20, llmtest.Reply{Chunks: []string{strings.Repeat("long ", 6000)}})
		return nil
	})
	sc.Step(`^a Provider that answers the summary request with nothing$`, func() error {
		w.compaction().sumTimes(20, llmtest.Reply{})
		return nil
	})
	sc.Step(`^a Provider that summarizes every chunk$`, func() error {
		c := w.compaction()
		for i := 1; i <= 20; i++ {
			c.sum = append(c.sum, llmtest.Reply{Chunks: []string{fmt.Sprintf("part %d", i)}})
		}
		return nil
	})
	sc.Step(`^a Provider whose summary requests fail$`, func() error {
		w.compaction().sumTimes(20, llmtest.Reply{Status: 500})
		return nil
	})
	sc.Step(`^a Provider whose first (\d+) summary requests fail and the rest answer "([^"]*)"$`, func(n int, text string) error {
		c := w.compaction()
		c.sumTimes(n, llmtest.Reply{Status: 500})
		c.sumTimes(20, llmtest.Reply{Chunks: []string{text}})
		return nil
	})
	sc.Step(`^a Provider whose summary request is still running$`, func() error {
		c := w.compaction()
		r := w.gated([]string{"part", " rest"}, 1)
		c.sum = append(c.sum, r)
		return nil
	})
	sc.Step(`^the user started a Compaction$`, func() error {
		w.act(func() error {
			if err := w.compactNow(); err != nil {
				return err
			}
			return w.cmp.lane.await("the summary request", func(o string) bool { return strings.Contains(o, "summary:start") })
		})
		return nil
	})
	sc.Step(`^the user submitted "([^"]*)" and its answer is streaming$`, func(text string) error {
		w.act(func() error {
			seen := countKind(w.snapshotEvents(), EvDelta)
			w.submit(text)
			return w.waitFor("the answer to stream", func(evs []Event) bool { return countKind(evs, EvDelta) > seen })
		})
		return nil
	})
}

func registerCompactionThens(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^every user Message and every final answer in the history is unchanged$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		c := w.compaction()
		if want, got := finalsAndUsers(c.before), finalsAndUsers(w.ag.History()); len(summaries(w.ag.History())) == 0 || !reflect.DeepEqual(got, want) {
			return fmt.Errorf("Messages after %+v, before %+v (summaries %d)", got, want, len(summaries(w.ag.History())))
		}
		return nil
	})
	sc.Step(`^no summary request was sent$`, func() error {
		n, err := w.summaryRequests()
		if err == nil && n != 0 {
			err = fmt.Errorf("%d summary requests were sent", n)
		}
		return err
	})
	sc.Step(`^the WorkSpan is replaced by one WorkSummary containing "([^"]*)"$`, func(text string) error {
		s, err := w.workSummaries()
		if err != nil {
			return err
		}
		if len(s) != 1 || !strings.HasPrefix(s[0].Content, "[work summary] ") || !strings.Contains(s[0].Content, text) {
			return fmt.Errorf("WorkSummaries %+v", s)
		}
		for _, m := range w.ag.History() {
			if m.Role == chat.RoleTool || len(m.ToolCalls) > 0 {
				return fmt.Errorf("the history still holds %+v", m)
			}
		}
		return nil
	})
	sc.Step(`^the WorkSummary lists "([^"]*)" and "([^"]*)"( and a warning is shown)?$`, func(a, b, warn string) error {
		s, err := w.workSummaries()
		if err != nil {
			return err
		}
		if len(s) != 1 || !strings.Contains(s[0].Content, a) || !strings.Contains(s[0].Content, b) {
			return fmt.Errorf("WorkSummaries %+v", s)
		}
		if warn != "" && !w.hasWarning("summary request failed") {
			return fmt.Errorf("no failure warning in %v", w.warnings())
		}
		return nil
	})
	sc.Step(`^the WorkSummary lists "([^"]*)" and a warning is shown$`, func(a string) error {
		s, err := w.workSummaries()
		if err != nil {
			return err
		}
		if len(s) != 1 || !strings.Contains(s[0].Content, a) || !w.hasWarning("summary request failed") {
			return fmt.Errorf("WorkSummaries %+v, warnings %v", s, w.warnings())
		}
		return nil
	})
	sc.Step(`^the WorkSummary is the generated list$`, func() error {
		s, err := w.workSummaries()
		if err != nil {
			return err
		}
		if len(s) != 1 || !strings.HasPrefix(s[0].Content, "[work summary] read_file ") || strings.Contains(s[0].Content, "long") {
			return fmt.Errorf("WorkSummaries %+v", s)
		}
		return nil
	})
	sc.Step(`^the WorkSpan is replaced by a generated list and no summary request was sent$`, func() error {
		s, err := w.workSummaries()
		if err != nil {
			return err
		}
		n, _ := w.summaryRequests()
		if len(s) != 1 || n != 0 || !strings.Contains(s[0].Content, "edit_file") {
			return fmt.Errorf("WorkSummaries %+v, %d summary requests", s, n)
		}
		return nil
	})
	sc.Step(`^more than one summary request was sent and the history holds one WorkSummary for the WorkSpan$`, func() error {
		n, err := w.summaryRequests()
		if err != nil {
			return err
		}
		if s := summaries(w.ag.History()); n < 2 || len(s) != 1 {
			return fmt.Errorf("%d summary requests, WorkSummaries %+v", n, s)
		}
		return nil
	})
	sc.Step(`^the Turn request is sent only after the summary request was fully read$`, func() error {
		return w.orderHolds("summary:end", "turn:start", "summary:start")
	})
	sc.Step(`^the summary request is sent only after the Turn request was fully read$`, func() error {
		return w.orderHolds("turn:end", "summary:start", "turn:start")
	})
	sc.Step(`^replaying the SessionFile gives the compacted history$`, func() error {
		got, err := w.replayed()
		if err != nil {
			return err
		}
		if want := w.ag.History(); len(summaries(got)) != 1 || !sameMessages(got, want) {
			return fmt.Errorf("replay %+v, live %+v", got, want)
		}
		return nil
	})
	sc.Step(`^the Trace holds the summary request input and a record that the fallback was used$`, func() error {
		rs, err := w.records("compact")
		if err != nil {
			return err
		}
		if len(rs) != 1 || rs[0].D["fallback"] != true || rs[0].D["applied"] != true {
			return fmt.Errorf("compact records %+v", rs)
		}
		in, _ := rs[0].D["inputs"].([]any)
		if s, _ := in[0].(string); len(in) != 1 || !strings.Contains(s, "read_file") || rs[0].D["prompt"] != summaryPrompt {
			return fmt.Errorf("inputs %v", in)
		}
		return nil
	})
	sc.Step(`^the Compaction was not applied to the running Turn$`, func() error {
		h := w.ag.History()
		if len(summaries(h)) != 0 || len(h) == 0 || h[len(h)-1].Role != chat.RoleUser {
			return fmt.Errorf("history %+v", h)
		}
		return nil
	})
}

// hasWarning reports whether a shown warning contains text.
func (w *world) hasWarning(text string) bool {
	for _, x := range w.warnings() {
		if strings.Contains(x, text) {
			return true
		}
	}
	return false
}

// orderHolds settles the Agent and checks that, from the first `from` entry of the request
// order log on, `first` occurs before the next `second`.
func (w *world) orderHolds(first, second, from string) error {
	if err := w.settle(); err != nil {
		return err
	}
	log := strings.Fields(w.compaction().lane.order())
	start := -1
	for i, e := range log {
		if e == from {
			start = i
			break
		}
	}
	if from == "turn:start" { // the request of the running Turn: the last turn:start before the first summary:start
		start = -1
		for i, e := range log {
			if e == "summary:start" {
				break
			}
			if e == from {
				start = i
			}
		}
	}
	if start < 0 {
		return fmt.Errorf("no %s in %v", from, log)
	}
	for _, e := range log[start:] {
		switch e {
		case first:
			return nil
		case second:
			return fmt.Errorf("%s came before %s: %v", second, first, log)
		}
	}
	return fmt.Errorf("no %s after %s: %v", first, from, log)
}

// ---------------------------------------------------------------------------
// Auto-compaction
// ---------------------------------------------------------------------------

func registerAutoCompactionSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^auto-compaction at (\d+) percent$`, func(p int) error { w.compaction().auto = p; return nil })
	sc.Step(`^a Provider whose answers report (\d+) tokens used$`, func(n int) error {
		w.reply(llmtest.Reply{Chunks: []string{"ok"}, Usage: &chat.Usage{PromptTokens: n - 5, CompletionTokens: 5}})
		return nil
	})
	sc.Step(`^a Provider whose next (\d+) Turns each read "([^"]*)" and report (\d+) tokens used$`, func(n int, file string, used int) error {
		if err := w.writeFile(file, "x"); err != nil {
			return err
		}
		for i := 0; i < n; i++ {
			w.workStep(3000, readArgsOf(file))
			w.reply(llmtest.Reply{Chunks: []string{"Done"}, Usage: &chat.Usage{PromptTokens: used - 5, CompletionTokens: 5}})
		}
		return nil
	})
	sc.Step(`^a Provider whose next Turn reads "([^"]*)" and then streams an answer reporting (\d+) tokens used and "([^"]*)" after it$`, func(file string, used int, next string) error {
		if err := w.writeFile(file, "x"); err != nil {
			return err
		}
		w.workStep(3000, readArgsOf(file))
		r := w.gated([]string{"answer", " rest"}, 1)
		r.Usage = &chat.Usage{PromptTokens: used - 5, CompletionTokens: 5}
		w.reply(r)
		w.reply(llmtest.Reply{Chunks: []string{next}})
		return nil
	})
	sc.Step(`^the user submits (\d+) Turns in a row$`, func(n int) error {
		return w.submitTurns(n)
	})
	sc.Step(`^auto-compaction was switched off by two failures$`, func() error {
		w.act(func() error { return w.submitTurns(3) })
		return nil
	})
	sc.Step(`^the user submitted "([^"]*)" and "([^"]*)" while the answer was streaming$`, func(a, b string) error {
		w.act(func() error {
			seen := countKind(w.snapshotEvents(), EvDelta)
			w.submit(a)
			if err := w.waitFor("the answer to stream", func(evs []Event) bool { return countKind(evs, EvDelta) > seen }); err != nil {
				return err
			}
			w.submit(b)
			return nil
		})
		return nil
	})
	sc.Step(`^the history holds one WorkSummary$`, func() error {
		s, err := w.workSummaries()
		if err == nil && len(s) != 1 {
			err = fmt.Errorf("WorkSummaries %+v", s)
		}
		return err
	})
	sc.Step(`^exactly (\d+) summary requests were sent and a warning says auto-compaction is off$`, func(n int) error {
		got, err := w.summaryRequests()
		if err != nil {
			return err
		}
		if got != n || !w.hasWarning("auto-compaction is off") {
			return fmt.Errorf("%d summary requests, warnings %v", got, w.warnings())
		}
		return nil
	})
	sc.Step(`^the next Turn at (\d+) percent compacts again$`, func(int) error {
		if err := w.settle(); err != nil {
			return err
		}
		before, _ := w.summaryRequests()
		w.submit("work again")
		if err := w.settle(); err != nil {
			return err
		}
		after, _ := w.summaryRequests()
		if after != before+1 {
			return errors.New("the Turn did not start an automatic Compaction")
		}
		return nil
	})
}

// submitTurns submits "turn 1".."turn n" one after the other, each waiting for the Agent to be idle.
func (w *world) submitTurns(n int) error {
	if err := w.begin(); err != nil {
		return err
	}
	for i := 1; i <= n; i++ {
		w.lastUser = fmt.Sprintf("turn %d", i)
		w.ag.Submit(w.lastUser)
		w.ag.Wait()
	}
	return nil
}
