package agent

import (
	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Steps for provider_stream.feature (the final steps are shared).
// ---------------------------------------------------------------------------

func registerProviderStreamSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Provider whose stream starts with (\d+) keepalive lines and then the answer "([^"]*)"$`, func(n int, ans string) error {
		w.reply(llmtest.Reply{Keepalives: n, Chunks: []string{ans}})
		return nil
	})
	sc.Step(`^a Provider whose stream ends with a usage chunk after the answer "([^"]*)"$`, func(ans string) error {
		w.reply(llmtest.Reply{Chunks: []string{ans}, Usage: &chat.Usage{PromptTokens: 10, CompletionTokens: 5}})
		return nil
	})
	sc.Step(`^a Provider whose stream contains a malformed chunk$`, func() error {
		w.reply(llmtest.Reply{Chunks: []string{"Hel"}, Malformed: true})
		return nil
	})
}
