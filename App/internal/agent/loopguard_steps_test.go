package agent

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Steps for loop_guard.feature: numbered lists streamed item by item.
// ---------------------------------------------------------------------------

// loopItem is the item the owner's model repeated.
const loopItem = "**Сборка и тестирование** — могу собирать и тестировать проекты."

// listChunks streams n numbered items, one chunk each; identical items differ only in number.
func listChunks(n int, identical bool) (out []string) {
	for i := 1; i <= n; i++ {
		item := loopItem
		if !identical {
			// letters instead of digits: the guard ignores digits
			item = fmt.Sprintf("**Шаг %c%c%c** — сделать действие %c%c.", 'а'+rune(i%20), 'а'+rune(i/20), 'а'+rune(i*7%20), 'а'+rune(i*3%20), 'а'+rune(i/3%20))
		}
		out = append(out, fmt.Sprintf("%d. %s\n\n", 66+i, item))
	}
	return out
}

// loopPrefix and loopPhrase are the owner's case: a tool list whose last item repeats a run
// of commands inside one line.
const (
	loopPrefix = "6. **shell** — выполнение команд (кроме `sudo`, `wget`, `curl`, `rm`, "
	loopPhrase = "`go install`, `go build`, `go test`, `go mod download`, `go mod tidy`, `go mod vendor`, `go get -u`, "
)

// tokenChunks splits s into small chunks of a few runes, as a model streams tokens.
func tokenChunks(s string) (out []string) {
	r := []rune(s)
	for i := 0; i < len(r); i += 3 {
		out = append(out, string(r[i:min(i+3, len(r))]))
	}
	return out
}

// lineChunks is one line: the prefix and n copies of the phrase.
func lineChunks(n int) []string {
	return tokenChunks(loopPrefix + strings.Repeat(loopPhrase, n))
}

// differentItems is one line of n different comma-separated items.
func differentItems(n int) []string {
	var b strings.Builder
	b.WriteString(loopPrefix)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "`cmd-%c%c%c`, ", 'a'+rune(i%26), 'a'+rune(i/26), 'a'+rune(i*7%26))
	}
	return tokenChunks(b.String() + "end.\n")
}

func registerLoopGuardSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Provider that streams one line repeating a phrase (\d+) times and keeps answering$`, func(n int) error {
		chunks := lineChunks(n)
		w.reply(w.gated(chunks, len(chunks)))
		return nil
	})
	sc.Step(`^a Provider that streams one line repeating a phrase (\d+) times and finishes normally$`, func(n int) error {
		w.reply(llmtest.Reply{Chunks: append(lineChunks(n), "\n")})
		return nil
	})
	sc.Step(`^a Provider that streams one line of (\d+) different items and finishes normally$`, func(n int) error {
		w.reply(llmtest.Reply{Chunks: differentItems(n)})
		return nil
	})
	sc.Step(`^the user saw at most (\d+) copies of the phrase$`, func(n int) error {
		if err := w.settle(); err != nil {
			return err
		}
		if got := strings.Count(w.deltas(), loopPhrase); got > n {
			return fmt.Errorf("the user saw %d copies of the phrase, want at most %d", got, n)
		}
		return nil
	})
	sc.Step(`^the last assistant Message keeps at most one copy of the phrase and is marked truncated$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		m, err := w.lastAssistant()
		if err != nil {
			return err
		}
		if got := strings.Count(m.Content, loopPhrase); got > 1 || !m.Truncated || !strings.HasPrefix(m.Content, loopPrefix) {
			return fmt.Errorf("copies = %d, truncated = %v, content %q", got, m.Truncated, m.Content)
		}
		return nil
	})
	sc.Step(`^the Turn does not end as cut off by a limit$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		ev, err := w.lastTurnEnd()
		if err != nil {
			return err
		}
		if ev.Finish == chat.FinishLength {
			return fmt.Errorf("the Turn ended with finish %q", ev.Finish)
		}
		return nil
	})
	sc.Step(`^a Provider that streams a numbered list of (\d+) identical items and keeps answering$`, func(n int) error {
		chunks := listChunks(n, true)
		w.reply(w.gated(chunks, len(chunks)))
		return nil
	})
	sc.Step(`^a Provider that streams a numbered list of (\d+) (identical|different) items and finishes normally$`, func(n int, kind string) error {
		w.reply(llmtest.Reply{Chunks: listChunks(n, kind == "identical")})
		return nil
	})
	sc.Step(`^the last assistant Message keeps one copy of the item and is marked truncated$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		m, err := w.lastAssistant()
		if err != nil {
			return err
		}
		if n := strings.Count(m.Content, loopItem); n != 1 || !m.Truncated {
			return fmt.Errorf("copies of the item = %d, truncated = %v, want 1 and true", n, m.Truncated)
		}
		return nil
	})
	sc.Step(`^no loop warning is shown$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		for _, s := range w.warnings() {
			if s == loopWarning {
				return fmt.Errorf("warning %q was shown", s)
			}
		}
		return nil
	})
}
