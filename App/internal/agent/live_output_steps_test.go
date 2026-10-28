package agent

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"
)

// ---------------------------------------------------------------------------
// Steps for live_output.feature: probe Tools that report output while they run.
// ---------------------------------------------------------------------------

// reporter adds a read-only probe Tool that reports text (may be empty), returns result
// (may be empty for "<name> done") and may fail; then it scripts one call and the answer.
func (w *world) reporter(name, text, result string, fail bool, final string) {
	p := w.probeRunner()
	p.add(name, true, false)
	p.reports[name], p.results[name], p.fails[name] = text, result, fail
	w.callAll(final, name)
}

func registerLiveOutputSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a Provider that calls the Tool "([^"]*)", which reports "([^"]*)" while it runs, and then answers "([^"]*)"$`, func(name, text, final string) error {
		w.reporter(name, text, "", false, final)
		return nil
	})
	sc.Step(`^a Provider that calls the Tool "([^"]*)", which reports "([^"]*)" and returns "([^"]*)", and then answers "([^"]*)"$`, func(name, text, result, final string) error {
		w.reporter(name, text, result, false, final)
		return nil
	})
	sc.Step(`^a Provider that calls the Tool "([^"]*)", which reports nothing, and then answers "([^"]*)"$`, func(name, final string) error {
		w.reporter(name, "", "", false, final)
		return nil
	})
	sc.Step(`^a Provider that calls the Tool "([^"]*)", which reports "([^"]*)" and then fails, and then answers "([^"]*)"$`, func(name, text, final string) error {
		w.reporter(name, text, "", true, final)
		return nil
	})

	sc.Step(`^the output "([^"]*)" of the ToolCall was reported before the ToolCall ended$`, func(text string) error {
		if err := w.settle(); err != nil {
			return err
		}
		out, end := -1, -1
		var id string
		for i, e := range w.snapshotEvents() {
			switch {
			case e.Kind == EvToolOutput && out < 0 && strings.Contains(e.Text, text):
				out, id = i, e.Call.ID
			case e.Kind == EvToolEnd && id != "" && e.Call.ID == id:
				end = i
			}
		}
		if out < 0 || end < 0 || out > end {
			return fmt.Errorf("output at event %d, ToolCall end at %d; want output first", out, end)
		}
		return nil
	})
	sc.Step(`^the ToolResult of "([^"]*)" is an error$`, func(name string) error {
		if err := w.settle(); err != nil {
			return err
		}
		res := w.toolMessagesOf(name)
		if len(res) != 1 || !strings.HasPrefix(res[0], "error:") {
			return fmt.Errorf("ToolResults of %s = %q, want one error", name, res)
		}
		return nil
	})
	sc.Step(`^the Turn ended without reported output$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		evs := w.snapshotEvents()
		if n := countKind(evs, EvToolOutput); n != 0 {
			return fmt.Errorf("%d output events, want none", n)
		}
		_, err := w.lastTurnEnd()
		return err
	})
}
