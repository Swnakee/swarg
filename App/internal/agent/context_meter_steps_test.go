package agent

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Steps for context_meter.feature. Used tokens are carried in the reply usage.
// ---------------------------------------------------------------------------

// usageReply answers with chunks and a usage chunk totalling used tokens.
func usageReply(used int, chunks ...string) llmtest.Reply {
	return llmtest.Reply{Chunks: chunks, Usage: &chat.Usage{PromptTokens: used - 10, CompletionTokens: 10}}
}

// warnings80 counts the logged 80 percent warnings.
func (w *world) warnings80() (n int) {
	for _, s := range w.warnings() {
		if strings.HasPrefix(s, "context 80%") {
			n++
		}
	}
	return n
}

func registerContextMeterSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a context limit of (\d+) tokens$`, func(n int) error { w.limit = n; return nil })
	sc.Step(`^a Provider whose answer reports (\d+) tokens used$`, func(n int) error {
		w.reply(usageReply(n, "ok"))
		return nil
	})
	sc.Step(`^a Provider whose answers report (\d+) tokens used and then answer "([^"]*)"$`, func(n int, ans string) error {
		w.reply(usageReply(n, "ok"))
		w.reply(usageReply(n, ans))
		return nil
	})
	sc.Step(`^a Provider whose answers report (\d+) and then (\d+) tokens used$`, func(a, b int) error {
		w.reply(usageReply(a, "ok"))
		w.reply(usageReply(b, "ok"))
		return nil
	})
	sc.Step(`^the context warning for 80 percent was shown$`, func() error {
		w.reply(usageReply(800, "ok"))
		w.act(func() error {
			w.submit("Hi")
			if n := w.warnings80(); n != 1 {
				return fmt.Errorf("80 percent warnings = %d, want 1", n)
			}
			return nil
		})
		return nil
	})
	sc.Step(`^the user cleared the Session$`, func() error {
		w.act(func() error { return w.ag.Clear() })
		return nil
	})
	sc.Step(`^a context warning for 80 percent is shown( again)?$`, func(again string) error {
		if err := w.settle(); err != nil {
			return err
		}
		want := 1
		if again != "" {
			want = 2
		}
		if n := w.warnings80(); n != want {
			return fmt.Errorf("80 percent warnings = %d, want %d", n, want)
		}
		return nil
	})
	sc.Step(`^no context warning is shown$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if got := w.warnings(); len(got) != 0 {
			return fmt.Errorf("warnings = %v, want none", got)
		}
		return nil
	})
	sc.Step(`^exactly one context warning is shown$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if got := w.warnings(); len(got) != 1 {
			return fmt.Errorf("warnings = %v, want exactly one", got)
		}
		return nil
	})
}
