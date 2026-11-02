package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"swarg/internal/config"
	"swarg/internal/prompt"
)

// ---------------------------------------------------------------------------
// Old profile names (R-134): accepted from outside, only the new ones in and out
// ---------------------------------------------------------------------------

func TestProfileAliases_ConfigAndPromptAgree(t *testing.T) {
	for _, old := range []string{"compact", "standard", "full"} {
		p, err := prompt.ParseProfile(old)
		if err != nil || string(p) != config.CanonicalProfile(old) {
			t.Errorf("%q: prompt says %q (%v), config says %q", old, p, err, config.CanonicalProfile(old))
		}
	}
}

func TestProfiles_ASessionFileWithAnOldNameResumesAsTheNewOne(t *testing.T) {
	r := newProfRig(t, func(c *config.Config) { c.Prompt.Profile = "high" })
	r.pf.Restore("compact", "m") // a SessionFile written before the rename
	if r.set.Profile() != prompt.Low || r.pf.Profile() != "low" {
		t.Errorf("profile %q, want low", r.set.Profile())
	}
	r.cfg.Prompt.Profiles = map[string]string{"x*": "medium"}
	if r.pf.Restore("", "x1"); r.set.Profile() != prompt.Medium {
		t.Errorf("the config decides again: profile %q, want medium", r.set.Profile())
	}
}

func TestProfiles_ChoosingAnOldNameAppliesRecordsAndShowsTheNewOne(t *testing.T) {
	r := newProfRig(t, nil)
	if err := r.pf.Choose("full", ""); err != nil {
		t.Fatal(err)
	}
	if r.set.Profile() != prompt.High || !slices.Equal(r.wrote, []string{"high"}) {
		t.Errorf("profile %q, recorded %v, want high", r.set.Profile(), r.wrote)
	}
	rows := r.pf.Profiles()
	if len(rows) != 3 || rows[0].Name != "low" || rows[1].Name != "medium" || rows[2].Name != "high" || !rows[2].Active {
		t.Errorf("rows %+v, want low, medium, high with high active", rows)
	}
}

func TestProfiles_AnOldNameSavedToAScopeIsWrittenAsTheNewOne(t *testing.T) {
	dir := t.TempDir()
	r := newProfRig(t, nil)
	r.pf.sc = newScopes(dir, t.TempDir())
	if err := r.pf.Choose("standard", "global"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || !strings.Contains(string(b), `"profile": "medium"`) {
		t.Errorf("config file: %v %s", err, b)
	}
}
