package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/session"
)

// ---------------------------------------------------------------------------
// session_usage.feature
// ---------------------------------------------------------------------------

// tokensOfLoaded converts the usage lines of a loaded SessionFile (what main does).
func tokensOfLoaded(l session.Loaded) Tokens {
	t := l.Tokens()
	return Tokens{Prompt: t.In, Cached: t.Cached, Completion: t.Out}
}

func registerUsageSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the reply used (\d+) prompt tokens, (\d+) of them cached, and (\d+) completion tokens$`, func(in, cached, out int) error {
		if len(w.replies) == 0 {
			return fmt.Errorf("no scripted reply to put the usage on")
		}
		w.replies[len(w.replies)-1].Usage = &chat.Usage{PromptTokens: in, CachedTokens: cached, CompletionTokens: out}
		return nil
	})
	sc.Step(`^a saved Session with a usage of (\d+) prompt, (\d+) cached and (\d+) completion tokens$`, func(in, cached, out int) error {
		return w.saveSession(twoTurns(), fmt.Sprintf(`{"usage":{"in":%d,"cached":%d,"out":%d}}`+"\n", in, cached, out))
	})
	sc.Step(`^the Session usage is (\d+) prompt, (\d+) cached and (\d+) completion tokens$`, func(in, cached, out int) error {
		if err := w.settle(); err != nil {
			return err
		}
		return wantTokens("Session usage", w.ag.Tokens(), Tokens{in, cached, out})
	})
	sc.Step(`^the Session spend is 0 USD and the Session usage is (\d+) prompt, (\d+) cached and (\d+) completion tokens$`, func(in, cached, out int) error {
		if err := w.settle(); err != nil {
			return err
		}
		if t := w.ag.Spend().Total; t != 0 {
			return fmt.Errorf("Session spend %v, want 0", t)
		}
		return wantTokens("Session usage", w.ag.Tokens(), Tokens{in, cached, out})
	})
	sc.Step(`^the Session usage is zero$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		return wantTokens("Session usage", w.ag.Tokens(), Tokens{})
	})
	sc.Step(`^the Session usage is zero and the SessionFile holds no usage line$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if err := wantTokens("Session usage", w.ag.Tokens(), Tokens{}); err != nil {
			return err
		}
		b, err := os.ReadFile(w.sessions().path)
		if err == nil && strings.Contains(string(b), `"usage"`) {
			err = fmt.Errorf("the SessionFile holds a usage line")
		}
		return err
	})
	sc.Step(`^the Session usage and the replayed SessionFile usage are (\d+) prompt, (\d+) cached and (\d+) completion tokens$`, func(in, cached, out int) error {
		if err := w.settle(); err != nil {
			return err
		}
		want := Tokens{in, cached, out}
		if err := wantTokens("Session usage", w.ag.Tokens(), want); err != nil {
			return err
		}
		l, err := session.Load(w.sessions().path)
		if err != nil {
			return err
		}
		return wantTokens("replayed usage", tokensOfLoaded(l), want)
	})
}

func wantTokens(what string, got, want Tokens) error {
	if got != want {
		return fmt.Errorf("%s %+v, want %+v", what, got, want)
	}
	return nil
}
