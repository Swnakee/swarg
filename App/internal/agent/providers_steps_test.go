package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/config"
	"swarg/internal/llm/llmtest"
	"swarg/internal/llm/openai"
	"swarg/internal/session"
)

// ---------------------------------------------------------------------------
// Steps for providers.feature: a second llmtest server as the other Source, switched through
// the real config seams (Ready, Recent.First) and Agent.SetSource.
// ---------------------------------------------------------------------------

type providerWorld struct {
	name, model string
	limit       int
	keyEnv, key string
	replies     []llmtest.Reply
	models      []string
	srv         *llmtest.Server
	recent      *config.Recent
	switchErr   error
	active      *llmtest.Server // the server whose model list /model shows
}

func (w *world) providers() *providerWorld {
	if w.prv == nil {
		w.prv = &providerWorld{limit: 16384}
	}
	return w.prv
}

// second is the second Source's server, started on first use with its scripted replies.
func (w *world) second() *llmtest.Server {
	p := w.providers()
	if p.srv == nil {
		p.srv = llmtest.New(w.tb, p.replies...)
		for _, id := range p.models {
			p.srv.Models = append(p.srv.Models, llmtest.Model{ID: id})
		}
	}
	return p.srv
}

// switchSource is the user's switch: the config decides whether the Source is ready and which
// model it starts with (its last used, else configured); the Agent takes the Source.
func (w *world) switchSource() error {
	if err := w.begin(); err != nil {
		return err
	}
	p := w.providers()
	entry := config.Provider{Type: "openai", BaseURL: w.second().URL, Model: p.model, ContextLimit: p.limit, MaxTokens: 100, APIKeyEnv: p.keyEnv}
	cfg := config.Config{Provider: "first", Providers: map[string]config.Provider{p.name: entry}}
	if _, p.switchErr = cfg.Ready(p.name); p.switchErr != nil {
		return nil
	}
	model := p.model
	if p.recent != nil {
		model = p.recent.First(p.name, p.model)
	}
	p.switchErr = w.ag.SetSource(Source{Name: p.name, Provider: openai.New(entry.BaseURL, p.key, nil), Model: model,
		MaxTokens: entry.MaxTokens, ContextLimit: entry.ContextLimit})
	if p.switchErr == nil {
		p.active = p.srv
	}
	return nil
}

// secondRequests are the requests the second Source received.
func (w *world) secondRequests() ([]map[string]any, error) {
	if err := w.settle(); err != nil {
		return nil, err
	}
	if w.providers().srv == nil {
		return nil, errors.New("the second source was never started")
	}
	return w.providers().srv.Requests(), nil
}

func registerProviderSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a second source "([^"]*)" with the model "([^"]*)"$`, func(name, model string) error {
		p := w.providers()
		p.name, p.model = name, model
		return nil
	})
	sc.Step(`^the second source answers "([^"]*)"$`, func(text string) error {
		w.providers().replies = append(w.providers().replies, llmtest.Reply{Chunks: []string{text}})
		return nil
	})
	sc.Step(`^the second source answers "([^"]*)" and then "([^"]*)"$`, func(a, b string) error {
		p := w.providers()
		p.replies = append(p.replies, llmtest.Reply{Chunks: []string{a}}, llmtest.Reply{Chunks: []string{b}})
		return nil
	})
	sc.Step(`^the second source has a context limit of (\d+) tokens$`, func(n int) error { w.providers().limit = n; return nil })
	sc.Step(`^the second source needs the key variable "([^"]*)"$`, func(env string) error {
		os.Unsetenv(env)
		w.providers().keyEnv = env
		return nil
	})
	sc.Step(`^the second source has the key "([^"]*)"$`, func(key string) error { w.providers().key = key; return nil })
	sc.Step(`^the second source refuses the key$`, func() error {
		w.providers().replies = append(w.providers().replies, llmtest.Reply{Status: 401, Body: `{"error":{"message":"invalid key"}}`})
		return nil
	})
	sc.Step(`^the second source is rate limited once for (\d+) seconds? and then answers "([^"]*)"$`, func(sec int, text string) error {
		p := w.providers()
		p.replies = append(p.replies, llmtest.Reply{Status: 429, RetryAfter: sec}, llmtest.Reply{Chunks: []string{text}})
		return nil
	})
	sc.Step(`^the first source lists the models "([^"]*)" and the second the models "([^"]*)"$`, func(a, b string) error {
		w.listed = strings.Split(strings.ReplaceAll(a, " ", ""), ",")
		w.providers().models = strings.Split(strings.ReplaceAll(b, " ", ""), ",")
		return nil
	})
	sc.Step(`^the model "([^"]*)" was used before on the source "([^"]*)"$`, func(model, name string) error {
		r, err := config.LoadRecent(w.tb.TempDir())
		if err != nil {
			return err
		}
		w.providers().recent = r
		return r.Use(name, model)
	})
	sc.Step(`^a Provider that calls a held Tool$`, func() error {
		w.probeRunner().add("hold", true, true)
		w.reply(w.toolReply([2]string{"hold", "{}"}))
		return nil
	})
	sc.Step(`^a saved Session whose last source is the second one$`, func() error {
		p := w.providers()
		return w.saveSession(twoTurns(), fmt.Sprintf(`{"provider":%q}`+"\n"+`{"model":%q}`+"\n", p.name, p.model))
	})
	sc.Step(`^the user resumed it on its recorded source$`, func() error {
		w.act(func() error {
			if err := w.resume(); err != nil {
				return err
			}
			l, err := session.Load(w.sessions().path)
			if err != nil {
				return err
			}
			if l.Provider() == w.providers().name {
				return w.switchSource()
			}
			return nil
		})
		return nil
	})

	sc.Step(`^the user switched to the second source$`, func() error { w.act(w.switchSource); return nil })
	sc.Step(`^the user switches to the second source$`, func() error {
		if err := w.switchSource(); err != nil {
			return err
		}
		if w.probe != nil {
			w.probe.releaseAll()
		}
		return nil
	})

	sc.Step(`^the request goes to the second source with the model "([^"]*)"$`, func(want string) error {
		reqs, err := w.secondRequests()
		if err != nil {
			return err
		}
		if len(reqs) == 0 {
			return errors.New("the second source got no request")
		}
		if got := reqs[len(reqs)-1]["model"]; got != want {
			return fmt.Errorf("the request names %v, want %q", got, want)
		}
		return nil
	})
	sc.Step(`^the first request went to the first source and the second to the second source$`, func() error {
		reqs, err := w.secondRequests()
		if err != nil {
			return err
		}
		if a, b := len(w.srv.Requests()), len(reqs); a != 1 || b != 1 {
			return fmt.Errorf("first source got %d requests, second %d; want 1 and 1", a, b)
		}
		return nil
	})
	sc.Step(`^the first request to the second source is a summary request and the Turn request follows$`, func() error {
		reqs, err := w.secondRequests()
		if err != nil {
			return err
		}
		if len(reqs) < 2 {
			return fmt.Errorf("the second source got %d requests, want a summary and the Turn", len(reqs))
		}
		first, last := roleContents(reqs[0]), roleContents(reqs[len(reqs)-1])
		if !strings.Contains(first[0], "work log") {
			return fmt.Errorf("the first request is not a summary request: %q", first[0])
		}
		if strings.Contains(last[0], "work log") || !strings.Contains(last[len(last)-1], "more") {
			return fmt.Errorf("the last request is not the Turn: %q", last)
		}
		if n := len(w.srv.Requests()); n != 4 { // the two scripted Turns only
			return fmt.Errorf("the first source got %d requests after the switch, want none more", n-4)
		}
		return nil
	})
	sc.Step(`^the model list shows only "([^"]*)"$`, func(want string) error {
		p := w.providers()
		if p.active == nil {
			return errors.New("no active source")
		}
		ids, err := openai.New(p.active.URL, "", nil).Models(context.Background())
		if err != nil {
			return err
		}
		if got := strings.Join(ids, ","); got != want {
			return fmt.Errorf("model list %q, want %q", got, want)
		}
		return nil
	})
	sc.Step(`^the switch is refused naming "([^"]*)"$`, func(want string) error {
		err := w.providers().switchErr
		if err == nil || !strings.Contains(err.Error(), want) {
			return fmt.Errorf("switch error %v, want %q", err, want)
		}
		if w.ag.Source().Name != "" {
			return fmt.Errorf("the source changed to %q", w.ag.Source().Name)
		}
		return nil
	})
	sc.Step(`^the Turn ends with an error naming "([^"]*)" and not "([^"]*)"$`, func(want, secret string) error {
		if err := w.settle(); err != nil {
			return err
		}
		end, err := w.lastTurnEnd()
		if err != nil {
			return err
		}
		if end.Err == nil || !strings.Contains(end.Err.Error(), want) {
			return fmt.Errorf("Turn error %v, want %q", end.Err, want)
		}
		if strings.Contains(end.Err.Error(), secret) {
			return fmt.Errorf("the error carries the key: %v", end.Err)
		}
		return nil
	})
}

// roleContents are the message contents of a request body, in order.
func roleContents(req map[string]any) (out []string) {
	msgs, _ := req["messages"].([]any)
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		c, _ := mm["content"].(string)
		out = append(out, c)
	}
	return out
}
