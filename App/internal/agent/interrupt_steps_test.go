package agent

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Steps for interrupt.feature. A gated reply streams its first chunk, holds
// until released, then sends restText.
// ---------------------------------------------------------------------------

// restText is the part of a gated answer that arrives after the release.
const restText = " and more"

func registerInterruptSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Provider that has streamed "([^"]*)" and is still answering(?: and then will answer "([^"]*)")?$`, func(part, next string) error {
		w.reply(w.gated([]string{part, restText}, 1))
		if next != "" {
			w.reply(llmtest.Reply{Chunks: []string{next}})
		}
		w.act(func() error {
			w.submit("Hi")
			return w.waitFor("the first Delta", func(evs []Event) bool {
				return countKind(evs, EvDelta) >= 1
			})
		})
		return nil
	})
	sc.Step(`^a Provider that is answering but has streamed no text yet$`, func() error {
		w.reply(w.gated([]string{"x"}, 0))
		w.act(func() error {
			w.submit("Hi")
			return w.waitFor("TurnStarted", func(evs []Event) bool {
				return countKind(evs, EvTurnStarted) >= 1
			})
		})
		return nil
	})
	sc.Step(`^the user interrupted the Turn$`, func() error {
		w.act(func() error { w.ag.Interrupt(); return nil })
		return nil
	})
	sc.Step(`^the user interrupts the Turn$`, func() error {
		if err := w.begin(); err != nil {
			return err
		}
		w.ag.Interrupt()
		return nil
	})
	sc.Step(`^the Provider finishes the rest of its answer(?: "([^"]*)")?$`, func(rest string) error {
		if rest != "" && rest != restText {
			return fmt.Errorf("the gated answer ends with %q, not %q", restText, rest)
		}
		return w.settle()
	})
	sc.Step(`^the last assistant Message is "([^"]*)" and is marked interrupted$`, func(want string) error {
		if err := w.settle(); err != nil {
			return err
		}
		m, err := w.lastAssistant()
		if err != nil {
			return err
		}
		if m.Content != want || !m.Interrupted {
			return fmt.Errorf("last assistant Message = %+v, want %q interrupted", m, want)
		}
		return nil
	})
	sc.Step(`^the Provider response is fully read$`, func() error {
		if got := w.srv.Completed(); got != 1 {
			return fmt.Errorf("responses fully read = %d, want 1", got)
		}
		return nil
	})
	sc.Step(`^no output contains "([^"]*)"$`, func(text string) error {
		if got := w.deltas(); strings.Contains(got, text) {
			return fmt.Errorf("output %q contains %q", got, text)
		}
		return nil
	})
	sc.Step(`^the second request is sent only after the first response was fully read$`, func() error {
		at := w.srv.CompletedAt()
		if len(at) != 2 || at[1] < 1 {
			return fmt.Errorf("Completed() at request arrival = %v, want 2 requests and the second at >= 1", at)
		}
		return nil
	})
}
