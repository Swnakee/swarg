package agent

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/cucumber/godog"

	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Steps for session_commands.feature and the shared "idle" setups.
// ---------------------------------------------------------------------------

func registerSessionCommandsSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^an idle Session$`, func() error { return nil })
	sc.Step(`^a Session with one finished Turn$`, func() error {
		w.reply(llmtest.Reply{Chunks: []string{"Done"}})
		w.act(func() error {
			w.submit("Hi")
			w.snapshot = w.ag.History()
			return nil
		})
		return nil
	})
	sc.Step(`^the Session history is unchanged$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if got := w.ag.History(); !reflect.DeepEqual(got, w.snapshot) {
			return fmt.Errorf("history = %v, want %v", got, w.snapshot)
		}
		return nil
	})
	sc.Step(`^the user clears the Session$`, func() error {
		if err := w.begin(); err != nil {
			return err
		}
		w.clearErr = w.ag.Clear()
		return nil
	})
	sc.Step(`^the Session history is empty$`, func() error { return w.wantHistory() })
	sc.Step(`^the clear is refused as busy$`, func() error {
		if !errors.Is(w.clearErr, ErrBusy) {
			return fmt.Errorf("Clear error = %v, want ErrBusy", w.clearErr)
		}
		return nil
	})
}
