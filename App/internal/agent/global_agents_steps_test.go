package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/cucumber/godog"

	"swarg/internal/agents"
	"swarg/internal/config"
)

// ---------------------------------------------------------------------------
// Steps for global_agents.feature. They drive the agents and config packages with temporary
// folders; they have their own small state and need no fake model.
// ---------------------------------------------------------------------------

type globalAgentsWorld struct {
	global, project string
	set             *agents.Set
	editErr         error
}

func registerGlobalAgentsSteps(sc *godog.ScenarioContext) {
	w := &globalAgentsWorld{}
	dirs := func() error {
		if w.global != "" {
			return nil
		}
		root, err := os.MkdirTemp("", "swarg-agents-*")
		if err != nil {
			return err
		}
		w.global, w.project = filepath.Join(root, "global"), filepath.Join(root, "project")
		return nil
	}
	file := func(dir, name, desc string) error {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, name+".md"), []byte("---\ndescription: "+desc+"\n---\nbody of "+name+"\n"), 0o644)
	}
	sc.Step(`^the global agent file "([^"]*)" with the description "([^"]*)"$`, func(name, desc string) error {
		if err := dirs(); err != nil {
			return err
		}
		return file(w.global, name, desc)
	})
	sc.Step(`^the project agent file "([^"]*)" with the description "([^"]*)"$`, func(name, desc string) error {
		if err := dirs(); err != nil {
			return err
		}
		return file(filepath.Join(w.project, filepath.FromSlash(agents.ProjectDir)), name, desc)
	})
	sc.Step(`^the agent types are (?:loaded|reloaded)$`, func() error {
		if err := dirs(); err != nil {
			return err
		}
		if w.set == nil {
			w.set, _ = agents.Load(w.global, w.project, nil)
			return nil
		}
		w.set.Reload()
		return nil
	})
	sc.Step(`^the agent type "([^"]*)" comes from the "([^"]*)" file and is described "([^"]*)"$`, func(name, source, desc string) error {
		t, ok := w.set.Get(name)
		if !ok || t.Source != source || t.Description != desc {
			return fmt.Errorf("type %q = %+v (found %v)", name, t, ok)
		}
		return nil
	})
	sc.Step(`^the user edits the built-in agent type "([^"]*)"$`, func(name string) error {
		if err := dirs(); err != nil {
			return err
		}
		_, _, w.editErr = agents.ExportBuiltin(w.global, name)
		return nil
	})
	sc.Step(`^the global file "([^"]*)" exists and loads back as the same description, tools and body$`, func(name string) error {
		if w.editErr != nil {
			return w.editErr
		}
		builtin, _ := agents.Load(filepath.Join(w.global, "none"), w.project, nil)
		want, _ := builtin.Get(name)
		set, warnings := agents.Load(w.global, w.project, nil)
		got, _ := set.Get(name)
		if len(warnings) > 0 || got.Source != "global" || got.Description != want.Description || got.Body != want.Body ||
			!reflect.DeepEqual(got.Tools, want.Tools) {
			return fmt.Errorf("copy differs: %+v vs %+v (%v)", got, want, warnings)
		}
		return nil
	})
	sc.Step(`^the global file "([^"]*)" still has the description "([^"]*)"$`, func(name, desc string) error {
		set, _ := agents.Load(w.global, w.project, nil)
		if got, _ := set.Get(name); got.Description != desc || got.Source != "global" {
			return fmt.Errorf("file changed: %+v", got)
		}
		return nil
	})
	sc.Step(`^the edit is refused$`, func() error {
		if w.editErr == nil {
			return fmt.Errorf("the edit was accepted")
		}
		return nil
	})
	var cfg config.Config
	sc.Step(`^the default config is read$`, func() error { cfg = config.Defaults(); return nil })
	sc.Step(`^the Profile of the Source "([^"]*)" is "([^"]*)" and of the Source "([^"]*)" is "([^"]*)"$`, func(a, pa, b, pb string) error {
		if ga, gb := cfg.ProfileFor(a, "any"), cfg.ProfileFor(b, "any"); ga != pa || gb != pb {
			return fmt.Errorf("profiles %s=%q %s=%q", a, ga, b, gb)
		}
		return nil
	})
}
