package agent

import (
	"context"
	"fmt"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Steps for approval.feature. The approval function counts its calls in w.asked.
// ---------------------------------------------------------------------------

// answer sets an approval function that always gives the same Approval.
func (w *world) answer(ap Approval) {
	w.real = true
	w.approve = func(context.Context, chat.ToolCall) Approval {
		w.asked.Add(1)
		return ap
	}
}

func registerApprovalSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^approval is granted$`, func() error { w.answer(Approval{Allow: true}); return nil })
	sc.Step(`^approval is denied$`, func() error { w.answer(Approval{}); return nil })
	sc.Step(`^approval is denied with the note "([^"]*)"$`, func(note string) error {
		w.answer(Approval{Note: note})
		return nil
	})
	sc.Step(`^a Provider that keeps calling "write_file" for "([^"]*)" with "([^"]*)"$`, func(path, content string) error {
		w.real = true
		for i := 0; i < 3; i++ {
			w.calls("", [2]string{"write_file", jsonArgs("path", path, "content", content)})
		}
		return nil
	})
	sc.Step(`^no approval function is set$`, func() error {
		w.real, w.approve = true, nil
		return nil
	})
	sc.Step(`^approval was asked (\d+) times?$`, func(n int) error {
		if err := w.settle(); err != nil {
			return err
		}
		if got := int(w.asked.Load()); got != n {
			return fmt.Errorf("approval asked %d times, want %d", got, n)
		}
		return nil
	})
}
