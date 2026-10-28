package agent

import (
	"fmt"
	"reflect"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Steps for input_queue.feature. The running answer streams its first chunk
// and is held by a gate, so "the Turn is running" is observable.
// ---------------------------------------------------------------------------

func registerInputQueueSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Provider that is answering "([^"]*)"(?: and then will answer "([^"]*)")?$`, func(first, next string) error {
		w.reply(w.gated([]string{first}, 1))
		if next != "" {
			w.reply(llmtest.Reply{Chunks: []string{next}})
		}
		return nil
	})
	sc.Step(`^a Provider that answers "([^"]*)", "([^"]*)" and "([^"]*)" in turn$`, func(a, b, c string) error {
		w.reply(w.gated([]string{a}, 1))
		w.reply(llmtest.Reply{Chunks: []string{b}})
		w.reply(llmtest.Reply{Chunks: []string{c}})
		return nil
	})
	sc.Step(`^the user submitted "([^"]*)" and the Turn is running$`, func(text string) error {
		w.act(func() error {
			w.submit(text)
			return w.waitFor("the first Delta", func(evs []Event) bool {
				return countKind(evs, EvDelta) >= 1
			})
		})
		return nil
	})
	sc.Step(`^the Session history is first, "([^"]*)", second, "([^"]*)" in that order$`, func(a, b string) error {
		return w.wantHistory(
			chat.Message{Role: chat.RoleUser, Content: "first"}, chat.Message{Role: chat.RoleAssistant, Content: a},
			chat.Message{Role: chat.RoleUser, Content: "second"}, chat.Message{Role: chat.RoleAssistant, Content: b})
	})
	sc.Step(`^the running answer is delivered in full$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		h := w.ag.History()
		if len(h) < 2 || h[1].Content != "A" || h[1].Interrupted || h[1].Truncated {
			return fmt.Errorf("first answer = %+v", h)
		}
		if n := countKind(w.snapshotEvents(), EvDraining); n != 0 {
			return fmt.Errorf("the running answer was interrupted (%d Draining events)", n)
		}
		return nil
	})
	sc.Step(`^the Provider received the user Messages in the order one, two, three$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if got, want := w.userTexts(), []string{"one", "two", "three"}; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("user Messages received = %v, want %v", got, want)
		}
		return nil
	})
	sc.Step(`^no request is sent to the Provider$`, func() error {
		if n := len(w.srv.Requests()); n != 0 {
			return fmt.Errorf("%d requests sent, want none", n)
		}
		return nil
	})
}
