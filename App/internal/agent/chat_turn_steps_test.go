package agent

import (
	"fmt"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Steps for chat_turn.feature, plus the submit and history steps shared by
// every feature.
// ---------------------------------------------------------------------------

func registerChatTurnSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Provider that streams the answer "([^"]*)" and finishes normally$`, func(ans string) error {
		w.reply(llmtest.Reply{Chunks: []string{ans}})
		return nil
	})
	sc.Step(`^a Provider that streams no text and finishes normally$`, func() error {
		w.reply(llmtest.Reply{})
		return nil
	})
	sc.Step(`^a Provider that drops the connection after the text "([^"]*)"$`, func(text string) error {
		w.reply(llmtest.Reply{Chunks: []string{text}, NoDone: true})
		return nil
	})
	sc.Step(`^a Provider that streams the answer "([^"]*)" and is cut by the token limit$`, func(ans string) error {
		w.reply(llmtest.Reply{Chunks: []string{ans}, Finish: "length"})
		return nil
	})
	sc.Step(`^a Provider that fails with a server error$`, func() error {
		w.reply(llmtest.Reply{Status: 500})
		return nil
	})
	sc.Step(`^a Provider that fails with a server error and then streams the answer "([^"]*)"$`, func(ans string) error {
		w.reply(llmtest.Reply{Status: 500})
		w.reply(llmtest.Reply{Chunks: []string{ans}})
		return nil
	})
	sc.Step(`^the user submitted "([^"]*)" and the Turn ended with an error$`, func(text string) error {
		w.act(func() error {
			w.submit(text)
			if end, err := w.lastTurnEnd(); err != nil || end.Err == nil {
				return fmt.Errorf("the Turn did not end with an error: %v", end)
			}
			return nil
		})
		return nil
	})

	// Shared by every feature.
	sc.Step(`^the user submitted "([^"]*)"$`, func(text string) error {
		w.act(func() error { w.submit(text); return nil })
		return nil
	})
	sc.Step(`^the user submits "([^"]*)"$`, func(text string) error {
		if err := w.begin(); err != nil {
			return err
		}
		w.submit(text)
		return nil
	})
	sc.Step(`^the last assistant Message is "([^"]*)"$`, func(want string) error {
		if err := w.settle(); err != nil {
			return err
		}
		m, err := w.lastAssistant()
		if err != nil {
			return err
		}
		if m.Content != want {
			return fmt.Errorf("last assistant Message = %q, want %q", m.Content, want)
		}
		return nil
	})
	sc.Step(`^the last assistant Message is (not )?marked truncated$`, func(not string) error {
		if err := w.settle(); err != nil {
			return err
		}
		m, err := w.lastAssistant()
		if err != nil {
			return err
		}
		if m.Truncated != (not == "") {
			return fmt.Errorf("Truncated = %v", m.Truncated)
		}
		return nil
	})
	sc.Step(`^the Session history is the user Message "([^"]*)" followed by the assistant Message "([^"]*)"$`, func(user, ans string) error {
		return w.wantHistory(chat.Message{Role: chat.RoleUser, Content: user}, chat.Message{Role: chat.RoleAssistant, Content: ans})
	})
	sc.Step(`^the Session history is only the user Message(?: "([^"]*)")?$`, func(text string) error {
		if text == "" {
			text = w.lastUser
		}
		return w.wantHistory(chat.Message{Role: chat.RoleUser, Content: text})
	})
	sc.Step(`^the Turn ends with an error and the Session history has no assistant Message$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		end, err := w.lastTurnEnd()
		if err != nil {
			return err
		}
		if end.Err == nil || end.Finish != chat.FinishError {
			return fmt.Errorf("Turn ended with finish %q, err %v", end.Finish, end.Err)
		}
		if _, err := w.lastAssistant(); err == nil {
			return fmt.Errorf("history has an assistant Message: %v", w.ag.History())
		}
		return nil
	})
}

// wantHistory settles the Session and compares the full history by role and content.
func (w *world) wantHistory(want ...chat.Message) error {
	if err := w.settle(); err != nil {
		return err
	}
	got := w.ag.History()
	if len(got) != len(want) {
		return fmt.Errorf("history = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].Role != want[i].Role || got[i].Content != want[i].Content {
			return fmt.Errorf("history[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	return nil
}
