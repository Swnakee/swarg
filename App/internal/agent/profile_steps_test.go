package agent

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/config"
	"swarg/internal/prompt"
)

// ---------------------------------------------------------------------------
// Steps for prompt_profiles.feature. The Profile layers come from the real prompt package; the
// markers below are headings of the built-in layers.
// ---------------------------------------------------------------------------

const (
	standardMarker = "Scope discipline"
	fullMarker     = "Autonomy and persistence"
)

// profileWorld is the state of the Profile scenarios.
type profileWorld struct {
	replayed string // the Profile of the replayed SessionFile
	err      error  // result of the last refused switch
}

func (w *world) profiles() *profileWorld {
	if w.prf == nil {
		w.prf = &profileWorld{}
	}
	return w.prf
}

func registerProfileSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the prompt Profile is "([^"]*)"$`, func(name string) error {
		w.act(func() error { return w.set.SetProfile(prompt.Profile(name)) })
		return nil
	})
	sc.Step(`^the user switches the prompt Profile to "([^"]*)"$`, func(name string) error {
		if err := w.begin(); err != nil {
			return err
		}
		w.profiles().err = w.set.SetProfile(prompt.Profile(name))
		return nil
	})
	sc.Step(`^the switch is refused and the known Profiles are listed$`, func() error {
		err := w.profiles().err
		if err == nil {
			return fmt.Errorf("the switch was not refused")
		}
		for _, p := range prompt.Profiles() {
			if !strings.Contains(err.Error(), string(p)) {
				return fmt.Errorf("error %q does not list %q", err, p)
			}
		}
		return nil
	})
	sc.Step(`^the SystemPrompt sent to the Provider has no Profile layer$`, func() error {
		if err := w.wantSystem(false, standardMarker); err != nil {
			return err
		}
		return w.wantSystem(false, fullMarker)
	})
	sc.Step(`^the SystemPrompt sent to the Provider carries the standard and the full layers$`, func() error {
		if err := w.wantSystem(true, standardMarker); err != nil {
			return err
		}
		return w.wantSystem(true, fullMarker)
	})

	// config.feature's world: the effective Profile of a Source and a model
	profileKey := func(project bool, key string, name any) error {
		c := w.cfgWorld()
		path := c.global()
		if project {
			path = c.project()
		}
		return config.Set(path, key, name, false)
	}
	sc.Step(`^the global config names the Profile "([^"]*)"$`, func(name string) error {
		return profileKey(false, "prompt.profile", name)
	})
	sc.Step(`^the global config names the Profile "([^"]*)" for the models "([^"]*)"$`, func(name, glob string) error {
		return profileKey(false, "prompt.profiles."+glob, name)
	})
	sc.Step(`^the global config names the Profile "([^"]*)" for the Source "([^"]*)"$`, func(name, source string) error {
		// a provider entry replaces the default as a whole: write a complete one
		for k, v := range map[string]any{"type": "openai", "base_url": "http://127.0.0.1:8080/v1", "model": "m",
			"context_limit": 1000, "max_tokens": 10, "profile": name} {
			if err := profileKey(false, "providers."+source+"."+k, v); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^the config picks the Profile "([^"]*)" for the model "([^"]*)" on the Source "([^"]*)"$`, func(want, model, source string) error {
		c := w.cfgWorld()
		if c.err != nil {
			return c.err
		}
		if got := c.cfg.ProfileFor(source, model); got != want {
			return fmt.Errorf("Profile %q, want %q", got, want)
		}
		return nil
	})

	// the SessionFile
	sc.Step(`^the user chose the prompt Profile "([^"]*)"$`, func(name string) error {
		w.act(func() error { return w.sessions().writer.Profile(name) })
		return nil
	})
	sc.Step(`^the replayed prompt Profile is (none|"[^"]*")$`, func(want string) error {
		want = strings.Trim(want, `"`)
		if want == "none" {
			want = ""
		}
		if got := w.profiles().replayed; got != want {
			return fmt.Errorf("replayed Profile %q, want %q", got, want)
		}
		return nil
	})
}
