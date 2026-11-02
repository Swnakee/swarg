package main

import (
	"fmt"
	"sync"

	"swarg/internal/agent"
	"swarg/internal/config"
	"swarg/internal/prompt"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// Prompt profiles in the app (R-115..R-118): which profile the SystemPrompt carries
// ---------------------------------------------------------------------------

// profileNotes are the one-line descriptions /prompt shows.
var profileNotes = map[prompt.Profile]string{
	prompt.Low:    "minimal: for small local models",
	prompt.Medium: "workflow, scope, edits, verification, git and shell safety",
	prompt.High:   "medium plus detailed standards, procedures and examples",
}

// profiles decides the prompt profile and applies it to the prompt Set. Until the user chooses one
// (/prompt, the /config row) or a resumed Session carries one, the config decides and follows the
// Source and the model; a choice is recorded in the SessionFile and holds for the Session.
type profiles struct {
	set     *prompt.Set
	cfg     *config.Config
	sc      scopes
	record  func(name string) error // writes the choice to the SessionFile (nil = nowhere)
	current func() string           // the active Source name

	mu       sync.Mutex
	explicit bool
}

// resolve is the profile the config names for a Source and a model; a name the prompt package
// does not know falls back to low (the config check should have refused it).
func (p *profiles) resolve(model string) prompt.Profile {
	name := p.cfg.ProfileFor(p.current(), model)
	pr, err := prompt.ParseProfile(name)
	if err != nil {
		return prompt.Low
	}
	return pr
}

// Restore applies a recorded choice ("" = none: the config decides again). Used at start (the resumed Session's choice) and when
// the app switches to another saved Session.
func (p *profiles) Restore(saved, model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pr, err := prompt.ParseProfile(saved); err == nil && saved != "" {
		p.explicit = true
		_ = p.set.SetProfile(pr)
		return
	}
	p.explicit = false
	_ = p.set.SetProfile(p.resolve(model))
}

// OnEvent follows a Model choice (a /model, a /server switch) while the user has not chosen.
func (p *profiles) OnEvent(ev agent.Event) {
	if ev.Kind != agent.EvModel {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.explicit {
		_ = p.set.SetProfile(p.resolve(ev.Text))
	}
}

// Profile is the profile in force.
func (p *profiles) Profile() string { return string(p.set.Profile()) }

// Profiles lists the profiles for /prompt.
func (p *profiles) Profiles() []ui.PromptRow {
	now := p.set.Profile()
	var rows []ui.PromptRow
	for _, pr := range prompt.Profiles() {
		rows = append(rows, ui.PromptRow{Name: string(pr), Tokens: p.set.ProfileTokens(pr), Note: profileNotes[pr], Active: pr == now})
	}
	return rows
}

// Choose applies a profile from the next request, makes the choice explicit and records it. A
// scope also writes prompt.profile there; a failed write is a ui.NotSavedError (the choice stays).
func (p *profiles) Choose(name, scope string) error {
	pr, err := prompt.ParseProfile(name)
	if err != nil {
		return err
	}
	p.mu.Lock()
	_ = p.set.SetProfile(pr)
	p.explicit = true
	p.mu.Unlock()
	if p.record != nil {
		if err := p.record(string(pr)); err != nil {
			return &ui.NotSavedError{Err: fmt.Errorf("session file: %w", err)}
		}
	}
	if err := p.sc.set(scope, "prompt.profile", string(pr)); err != nil {
		return &ui.NotSavedError{Err: err}
	}
	return nil
}

var _ ui.PromptAPI = (*profiles)(nil)
