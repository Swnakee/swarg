package agent

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/cucumber/godog"
)

// ---------------------------------------------------------------------------
// thinking.feature
// ---------------------------------------------------------------------------

// thinkWorld is the state of the thinking scenarios.
type thinkWorld struct {
	presets  bool // the Provider sends the Qwen presets (Deps.Extra, ThinkExtra, ThinkTokens)
	ollama   bool // the Ollama presets instead: reasoning_effort none / high (R-64)
	replayed bool // the thinking mode of the replayed SessionFile
}

func (w *world) thinks() *thinkWorld {
	if w.thk == nil {
		w.thk = &thinkWorld{}
	}
	return w.thk
}

// plugThink gives the Agent the config's presets; called by begin.
func (w *world) plugThink(d *Deps) {
	if w.thk == nil || !w.thk.presets {
		return
	}
	d.MaxTokens = 2048
	if w.thk.ollama { // mirrors config.Provider{Type: "ollama"}: ExtraBody and ThinkingBody
		d.Extra = map[string]any{"reasoning_effort": "none", "temperature": 0.7}
		d.ThinkExtra = map[string]any{"reasoning_effort": "high", "temperature": 0.6}
		d.ThinkTokens = 8192
		return
	}
	d.Extra = map[string]any{
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"temperature":          0.7,
	}
	d.ThinkExtra = map[string]any{
		"chat_template_kwargs": map[string]any{"enable_thinking": true},
		"temperature":          0.6,
	}
	d.ThinkTokens = 8192
}

// thinkLines counts the thinking lines of the scenario's SessionFile.
func (w *world) thinkLines() (int, error) {
	if err := w.settle(); err != nil {
		return 0, err
	}
	b, err := os.ReadFile(w.sessions().path)
	if err != nil {
		return 0, err
	}
	return strings.Count(string(b), `"think"`), nil
}

// requestThinks checks request i: thinking flag, temperature and max_tokens (tokens < 0 = skip).
func (w *world) requestThinks(i int, on string, temp float64, tokens int) error {
	reqs := w.srv.Requests()
	if i >= len(reqs) {
		return fmt.Errorf("only %d requests were sent", len(reqs))
	}
	r := reqs[i]
	args, _ := r["chat_template_kwargs"].(map[string]any)
	if got, _ := args["enable_thinking"].(bool); got != (on == "on") {
		return fmt.Errorf("request %d: enable_thinking %v, want %s", i+1, args["enable_thinking"], on)
	}
	if got, _ := r["temperature"].(float64); math.Abs(got-temp) > 1e-9 {
		return fmt.Errorf("request %d: temperature %v, want %v", i+1, r["temperature"], temp)
	}
	if got, _ := r["max_tokens"].(float64); int(got) != tokens {
		return fmt.Errorf("request %d: max_tokens %v, want %d", i+1, r["max_tokens"], tokens)
	}
	return nil
}

func registerThinkSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Provider with the thinking presets$`, func() error {
		w.thinks().presets = true
		return nil
	})
	sc.Step(`^an Ollama Provider$`, func() error {
		w.thinks().presets, w.thinks().ollama = true, true
		return nil
	})
	// Ollama is asked through reasoning_effort: "high" = think, "none" = do not think (R-64)
	sc.Step(`^the Provider is asked (not )?to think$`, func(not string) error {
		if err := w.settle(); err != nil {
			return err
		}
		want := map[string]string{"": "high", "not ": "none"}[not]
		reqs := w.srv.Requests()
		if got := reqs[len(reqs)-1]["reasoning_effort"]; got != want {
			return fmt.Errorf("reasoning_effort = %v, want %q", got, want)
		}
		return nil
	})
	sc.Step(`^the user switched thinking (on|off)$`, func(mode string) error {
		w.act(func() error { w.ag.SetThinking(mode == "on"); return nil })
		return nil
	})
	sc.Step(`^the user switches thinking (on|off)$`, func(mode string) error {
		if err := w.begin(); err != nil {
			return err
		}
		w.ag.SetThinking(mode == "on")
		return nil
	})
	sc.Step(`^a saved Session whose thinking is on$`, func() error {
		return w.saveSession(twoTurns(), `{"think":true}`+"\n")
	})
	sc.Step(`^the request has thinking (on|off) with temperature ([\d.]+) and at most (\d+) tokens$`,
		func(mode string, temp float64, tokens int) error {
			if err := w.settle(); err != nil {
				return err
			}
			return w.requestThinks(len(w.srv.Requests())-1, mode, temp, tokens)
		})
	sc.Step(`^the first request has thinking (on|off) and the second thinking (on|off)$`, func(a, b string) error {
		if err := w.settle(); err != nil {
			return err
		}
		for i, mode := range []string{a, b} {
			want := map[string]float64{"on": 0.6, "off": 0.7}[mode]
			tokens := map[string]int{"on": 8192, "off": 2048}[mode]
			if err := w.requestThinks(i, mode, want, tokens); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^the SessionFile holds no thinking change$`, func() error {
		n, err := w.thinkLines()
		if err == nil && n != 0 {
			err = fmt.Errorf("%d thinking changes recorded, want none", n)
		}
		return err
	})
	sc.Step(`^the replayed thinking is (on|off)$`, func(want string) error {
		if got := w.thinks().replayed; got != (want == "on") {
			return fmt.Errorf("replayed thinking %v, want %s", got, want)
		}
		return nil
	})
}
