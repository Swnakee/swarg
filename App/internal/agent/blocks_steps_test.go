package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"
)

// ---------------------------------------------------------------------------
// Steps for blocks.feature. Files are written before the build; the Set is
// loaded by prompt.Load in begin.
// ---------------------------------------------------------------------------

func registerBlocksSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a global Block "([^"]*)" with the text "([^"]*)"$`, func(name, text string) error {
		return os.WriteFile(filepath.Join(w.globalDir(), name+".md"), []byte(text+"\n"), 0o644)
	})
	sc.Step(`^a SWARG\.md with the text "([^"]*)" in the working directory$`, func(text string) error {
		return os.WriteFile(filepath.Join(w.cwd(), "SWARG.md"), []byte(text+"\n"), 0o644)
	})
	sc.Step(`^a SWARG\.md with the text "([^"]*)" in the parent of the working directory$`, func(text string) error {
		return os.WriteFile(filepath.Join(w.parentDir(), "SWARG.md"), []byte(text+"\n"), 0o644)
	})
	sc.Step(`^a rules file "([^"]*)" with the text "([^"]*)" in the working directory$`, func(name, text string) error {
		return os.WriteFile(filepath.Join(w.cwd(), name), []byte(text+"\n"), 0o644)
	})
	sc.Step(`^the working directory is inside a git work tree$`, func() error {
		return os.Mkdir(filepath.Join(w.cwd(), ".git"), 0o755)
	})
	sc.Step(`^a working directory without a SWARG\.md$`, func() error { return nil })
	sc.Step(`^the Block "([^"]*)" is (?:switched )?off$`, func(name string) error {
		w.act(func() error { return w.set.SetEnabled(name, false) })
		return nil
	})
	sc.Step(`^the user switches the Block "([^"]*)" off$`, func(name string) error {
		if err := w.begin(); err != nil {
			return err
		}
		w.switchErr = w.set.SetEnabled(name, false)
		return nil
	})
	sc.Step(`^the switch is refused and the known Block names are listed$`, func() error {
		if w.switchErr == nil {
			return fmt.Errorf("the switch was not refused")
		}
		for _, name := range []string{"base", "style", "env"} {
			if !strings.Contains(w.switchErr.Error(), name) {
				return fmt.Errorf("error %q does not list %q", w.switchErr, name)
			}
		}
		return nil
	})
	sc.Step(`^the SystemPrompt sent to the Provider (does not contain|contains) "([^"]*)"$`, func(verb, text string) error {
		return w.wantSystem(verb == "contains", text)
	})
	sc.Step(`^the SystemPrompt sent to the Provider contains the base Block$`, func() error {
		return w.wantSystem(true, "You are swarg")
	})
	sc.Step(`^the SystemPrompt sent to the Provider contains "([^"]*)" instead of the base Block$`, func(text string) error {
		if err := w.wantSystem(true, text); err != nil {
			return err
		}
		return w.wantSystem(false, "You are swarg")
	})
}

// wantSystem checks the SystemPrompt of the last request.
func (w *world) wantSystem(contains bool, text string) error {
	if err := w.settle(); err != nil {
		return err
	}
	sys, err := w.systemPrompt(-1)
	if err != nil {
		return err
	}
	if strings.Contains(sys, text) != contains {
		return fmt.Errorf("SystemPrompt contains %q = %v, want %v:\n%s", text, !contains, contains, sys)
	}
	return nil
}
