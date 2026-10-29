package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
	"swarg/internal/session"
)

// ---------------------------------------------------------------------------
// model_choice.feature and generation_speed.feature
// ---------------------------------------------------------------------------

// modelWorld is the state of the Model choice and Generation speed scenarios.
type modelWorld struct {
	start    string // Deps.Model; "" = "m"
	setErr   error  // the last SetModel result
	replayed string // the model of the replayed SessionFile

	mu     sync.Mutex
	script []float64 // the times (seconds) Deps.Now returns, one per call; then the last one again
	n      int
	steps  int // Steps with scripted times so far (each gets its own 100 s window)
}

func (w *world) models() *modelWorld {
	if w.mdl == nil {
		w.mdl = &modelWorld{}
	}
	return w.mdl
}

// plugModel sets the starting model and the scripted clock; called by begin.
func (w *world) plugModel(d *Deps) {
	if w.mdl == nil {
		return
	}
	if w.mdl.start != "" {
		d.Model = w.mdl.start
	}
	if len(w.mdl.script) > 0 {
		d.Now = w.mdl.now
	}
}

// now is the scripted clock: the agent asks it once at the request and once per stream event.
func (m *modelWorld) now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := min(m.n, len(m.script)-1)
	m.n++
	return time.Unix(0, 0).Add(time.Duration(m.script[i] * float64(time.Second)))
}

// timed scripts the clock of one Step: the request, then one time per stream event.
func (m *modelWorld) timed(events ...float64) {
	base := float64(100 * m.steps)
	m.steps++
	m.script = append(m.script, base) // the request
	for _, t := range events {
		m.script = append(m.script, base+t)
	}
}

// textAnswer scripts a text answer with a usage report and its times: the first text at
// first, the usage and the end at end.
func (w *world) textAnswer(text string, tokens int, first, end float64) {
	w.reply(llmtest.Reply{Chunks: []string{text}, Usage: &chat.Usage{CompletionTokens: tokens}})
	w.models().timed(first, end, end) // delta, usage, done
}

// toolOnlyAnswer scripts an answer with one ToolCall (no text) after one prompt-reading
// progress event: progress at read, usage and the end at end.
func (w *world) toolOnlyAnswer(tokens int, read, end float64) {
	w.probeRunner().add("ping", true, false)
	r := w.toolReply([2]string{"ping", "{}"})
	r.Keepalives, r.Usage = 1, &chat.Usage{CompletionTokens: tokens}
	w.reply(r)
	w.models().timed(read, end, end, end) // progress, tool-call arguments (R-61), usage, done (the tool call comes with the final chunk)
}

// requestModel is the "model" field of request i (0-based).
func (w *world) requestModel(i int) (string, error) {
	reqs := w.srv.Requests()
	if i >= len(reqs) {
		return "", fmt.Errorf("only %d requests were sent", len(reqs))
	}
	s, _ := reqs[i]["model"].(string)
	return s, nil
}

// modelLines counts the Model choice lines of the scenario's SessionFile.
func (w *world) modelLines() (int, error) {
	if err := w.settle(); err != nil {
		return 0, err
	}
	b, err := os.ReadFile(w.sessions().path)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return 0, err
		}
		if _, header := r["v"]; !header && r["model"] != nil {
			n++
		}
	}
	return n, nil
}

// speeds are the Generation speeds reported so far, rounded to 0.1.
func (w *world) speeds() []float64 {
	var out []float64
	for _, e := range w.snapshotEvents() {
		if e.Kind == EvSpeed {
			out = append(out, math.Round(e.Speed*10)/10)
		}
	}
	return out
}

func registerModelSteps(sc *godog.ScenarioContext, w *world) {
	// Model choice.
	sc.Step(`^the user chose the model "([^"]*)"$`, func(name string) error {
		w.act(func() error { w.models().setErr = w.ag.SetModel(name); return nil })
		return nil
	})
	sc.Step(`^the app starts with the model "([^"]*)"$`, func(name string) error {
		w.models().start = name
		return nil
	})
	sc.Step(`^a Provider that calls a held Tool and then answers "([^"]*)"$`, func(final string) error {
		w.probeRunner().add("hold", true, true)
		w.callAll(final, "hold")
		return nil
	})
	sc.Step(`^the user chooses the model "([^"]*)"$`, func(name string) error {
		if err := w.begin(); err != nil {
			return err
		}
		w.models().setErr = w.ag.SetModel(name)
		return nil
	})
	sc.Step(`^a saved Session whose last chosen model is "([^"]*)"$`, func(name string) error {
		return w.saveSession(twoTurns(), fmt.Sprintf(`{"model":%q}`+"\n", name))
	})
	sc.Step(`^the SessionFile is replayed$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		l, err := session.Load(w.sessions().path)
		w.models().replayed = l.Model()
		w.thinks().replayed = l.Thinking()
		return err
	})

	sc.Step(`^the request names the model "([^"]*)"$`, func(want string) error {
		if err := w.settle(); err != nil {
			return err
		}
		got, err := w.requestModel(len(w.srv.Requests()) - 1)
		if err == nil && got != want {
			err = fmt.Errorf("the request names %q, want %q", got, want)
		}
		return err
	})
	sc.Step(`^the first request names the model "([^"]*)" and the second the model "([^"]*)"$`, func(a, b string) error {
		if err := w.settle(); err != nil {
			return err
		}
		for i, want := range []string{a, b} {
			got, err := w.requestModel(i)
			if err != nil {
				return err
			}
			if got != want {
				return fmt.Errorf("request %d names %q, want %q", i+1, got, want)
			}
		}
		return nil
	})
	sc.Step(`^the SessionFile holds no model change$`, func() error {
		n, err := w.modelLines()
		if err == nil && n != 0 {
			err = fmt.Errorf("%d model changes recorded, want none", n)
		}
		if err == nil && w.models().setErr != nil {
			err = w.models().setErr
		}
		return err
	})
	sc.Step(`^the SessionFile holds exactly one model change$`, func() error {
		n, err := w.modelLines()
		if err == nil && n != 1 {
			err = fmt.Errorf("%d model changes recorded, want the saved one only", n)
		}
		return err
	})
	sc.Step(`^the choice is refused and the model stays "([^"]*)"$`, func(want string) error {
		if err := w.settle(); err != nil {
			return err
		}
		if !errors.Is(w.models().setErr, ErrBlankModel) {
			return fmt.Errorf("SetModel returned %v, want ErrBlankModel", w.models().setErr)
		}
		if got := w.ag.Model(); got != want {
			return fmt.Errorf("model %q, want %q", got, want)
		}
		return nil
	})
	sc.Step(`^the replayed model is "([^"]*)"$`, func(want string) error {
		if got := w.models().replayed; got != want {
			return fmt.Errorf("replayed model %q, want %q", got, want)
		}
		return nil
	})

	// Generation speed.
	sc.Step(`^a Provider that answers "([^"]*)" with (\d+) completion tokens, the first text at ([\d.]+) s and the end at ([\d.]+) s$`,
		func(text string, tokens int, first, end float64) error {
			w.textAnswer(text, tokens, first, end)
			return nil
		})
	sc.Step(`^a Provider that only calls a Tool with (\d+) completion tokens, reading the prompt until ([\d.]+) s and ending at ([\d.]+) s, and then answers "([^"]*)"$`,
		func(tokens int, read, end float64, final string) error {
			w.toolOnlyAnswer(tokens, read, end)
			w.reply(llmtest.Reply{Chunks: []string{final}})
			w.models().timed(5, 6) // delta, done: no usage, no speed
			return nil
		})
	sc.Step(`^a Provider that only calls a Tool with (\d+) completion tokens, reading the prompt until ([\d.]+) s and ending at ([\d.]+) s, and then answers "([^"]*)" with (\d+) completion tokens, the first text at ([\d.]+) s and the end at ([\d.]+) s$`,
		func(tokens int, read, end float64, final string, tokens2 int, first2, end2 float64) error {
			w.toolOnlyAnswer(tokens, read, end)
			w.textAnswer(final, tokens2, first2, end2)
			return nil
		})
	sc.Step(`^a Provider that answers "([^"]*)" without a usage report$`, func(text string) error {
		w.reply(llmtest.Reply{Chunks: []string{text}})
		w.models().timed(1, 2)
		return nil
	})
	sc.Step(`^the Generation speed of the (?:first )?Step is ([\d.]+) tokens per second$`, func(want float64) error {
		if err := w.settle(); err != nil {
			return err
		}
		got := w.speeds()
		if len(got) == 0 || got[0] != want {
			return fmt.Errorf("speeds %v, want %v first", got, want)
		}
		return nil
	})
	sc.Step(`^no Generation speed is reported$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if got := w.speeds(); len(got) != 0 {
			return fmt.Errorf("speeds %v reported, want none", got)
		}
		return nil
	})
	sc.Step(`^the Steps report the speeds ([\d.]+) and ([\d.]+) tokens per second$`, func(a, b float64) error {
		if err := w.settle(); err != nil {
			return err
		}
		if got := w.speeds(); len(got) != 2 || got[0] != a || got[1] != b {
			return fmt.Errorf("speeds %v, want [%v %v]", got, a, b)
		}
		return nil
	})
}
