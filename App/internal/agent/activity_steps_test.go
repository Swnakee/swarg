package agent

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Stream activity (R-61): features/stream_activity.feature
// ---------------------------------------------------------------------------

func registerActivitySteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Provider that thinks "([^"]*)" and then answers "([^"]*)"$`, func(think, final string) error {
		w.real = true
		w.reply(llmtest.Reply{Reasoning: []string{think}, Chunks: []string{final}})
		return nil
	})
	sc.Step(`^a Provider that streams the arguments of "([^"]*)" in chunks and then answers "([^"]*)"$`, func(tool, final string) error {
		w.real = true
		r := w.toolReply([2]string{tool, jsonArgs("path", ".")})
		r.SplitArgs = true
		w.reply(r)
		w.say(final)
		return nil
	})
	sc.Step(`^the UI received activity "([^"]*)"$`, func(want string) error {
		if err := w.settle(); err != nil {
			return err
		}
		var got strings.Builder
		w.mu.Lock()
		for _, e := range w.events {
			if e.Kind == EvActivity {
				got.WriteString(e.Text)
			}
		}
		w.mu.Unlock()
		if !strings.Contains(got.String(), want) {
			return fmt.Errorf("activity %q does not contain %q", got.String(), want)
		}
		return nil
	})
}
