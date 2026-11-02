package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"swarg/internal/agent"
	"swarg/internal/chat"
	"swarg/internal/config"
	"swarg/internal/prompt"
	"swarg/internal/session"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// Prompt profiles in the app (R-116, R-117, R-118)
// ---------------------------------------------------------------------------

type profRig struct {
	pf    *profiles
	set   *prompt.Set
	cfg   *config.Config
	wrote []string // profile names given to record
}

func newProfRig(t *testing.T, edit func(*config.Config)) *profRig {
	t.Helper()
	cfg := config.Defaults()
	if edit != nil {
		edit(&cfg)
	}
	set, _, err := prompt.Load(t.TempDir(), t.TempDir(), prompt.Env{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	r := &profRig{set: set, cfg: &cfg}
	r.pf = &profiles{set: set, cfg: &cfg, sc: newScopes(t.TempDir(), t.TempDir()),
		record:  func(n string) error { r.wrote = append(r.wrote, n); return nil },
		current: func() string { return cfg.Provider }}
	return r
}

func TestProfileNames_MatchThePromptPackage(t *testing.T) {
	var names []string
	for _, p := range prompt.Profiles() {
		names = append(names, string(p))
	}
	if !slices.Equal(names, config.ProfileNames) {
		t.Errorf("config.ProfileNames %v differ from prompt.Profiles %v", config.ProfileNames, names)
	}
	if config.DefaultProfile != string(prompt.Low) {
		t.Errorf("DefaultProfile = %q", config.DefaultProfile)
	}
}

func TestProfiles_StartFollowsTheConfig(t *testing.T) {
	r := newProfRig(t, func(c *config.Config) {
		c.Prompt.Profiles = map[string]string{"*9b*": "high"}
		c.Prompt.Profile = "medium"
	})
	r.pf.Restore("", "qwen-9B")
	if r.set.Profile() != prompt.High {
		t.Errorf("profile %q, want high from the model pattern", r.set.Profile())
	}
	r.pf.Restore("", "other")
	if r.set.Profile() != prompt.Medium {
		t.Errorf("profile %q, want medium from prompt.profile", r.set.Profile())
	}
}

func TestProfiles_NothingConfiguredIsCompact(t *testing.T) {
	r := newProfRig(t, nil)
	r.pf.Restore("", "m")
	if r.set.Profile() != prompt.Low || r.pf.Profile() != "low" {
		t.Errorf("profile %q, want low", r.set.Profile())
	}
}

func TestProfiles_ModelChangeReResolvesUntilTheUserChooses(t *testing.T) {
	r := newProfRig(t, func(c *config.Config) { c.Prompt.Profiles = map[string]string{"big*": "high"} })
	r.pf.Restore("", "small")
	r.pf.OnEvent(agent.Event{Kind: agent.EvModel, Text: "big-1"})
	if r.set.Profile() != prompt.High {
		t.Fatalf("profile %q after a model change, want high", r.set.Profile())
	}
	r.pf.OnEvent(agent.Event{Kind: agent.EvDelta, Text: "big-1"})
	if err := r.pf.Choose("medium", ""); err != nil {
		t.Fatal(err)
	}
	r.pf.OnEvent(agent.Event{Kind: agent.EvModel, Text: "small"})
	r.pf.OnEvent(agent.Event{Kind: agent.EvModel, Text: "big-2"})
	if r.set.Profile() != prompt.Medium {
		t.Errorf("profile %q, an explicit choice must hold across model changes", r.set.Profile())
	}
}

func TestProfiles_RecordedChoiceBeatsTheConfigAndIsExplicit(t *testing.T) {
	r := newProfRig(t, func(c *config.Config) { c.Prompt.Profile = "high" })
	r.pf.Restore("low", "m")
	if r.set.Profile() != prompt.Low {
		t.Fatalf("profile %q, the recorded low must win over the config", r.set.Profile())
	}
	r.cfg.Prompt.Profiles = map[string]string{"x*": "medium"}
	r.pf.OnEvent(agent.Event{Kind: agent.EvModel, Text: "x1"})
	if r.set.Profile() != prompt.Low {
		t.Errorf("a recorded choice is explicit; profile %q after a model change", r.set.Profile())
	}
	r.pf.Restore("", "m") // switching to a Session with no choice: the config decides again
	if r.set.Profile() != prompt.High {
		t.Errorf("profile %q, want the config's high", r.set.Profile())
	}
}

func TestProfiles_ABadRecordedNameFallsBackToTheConfig(t *testing.T) {
	r := newProfRig(t, func(c *config.Config) { c.Prompt.Profile = "medium" })
	r.pf.Restore("gigantic", "m")
	if r.set.Profile() != prompt.Medium {
		t.Errorf("profile %q, want medium", r.set.Profile())
	}
}

func TestProfiles_ChooseRecordsAndRefusesUnknown(t *testing.T) {
	r := newProfRig(t, nil)
	if err := r.pf.Choose("huge", ""); err == nil || !strings.Contains(err.Error(), "low, medium, high") {
		t.Fatalf("unknown profile: %v", err)
	}
	if len(r.wrote) != 0 || r.set.Profile() != prompt.Low {
		t.Errorf("a refused choice recorded %v / changed the profile", r.wrote)
	}
	if err := r.pf.Choose("high", ""); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.wrote, []string{"high"}) || r.set.Profile() != prompt.High {
		t.Errorf("recorded %v, profile %q", r.wrote, r.set.Profile())
	}
	rows := r.pf.Profiles()
	if len(rows) != 3 || !rows[2].Active || rows[0].Active || !(rows[0].Tokens < rows[1].Tokens && rows[1].Tokens < rows[2].Tokens) {
		t.Errorf("rows %+v", rows)
	}
}

func TestProfiles_ChooseWithScopeWritesTheConfigFile(t *testing.T) {
	dir, cwd := t.TempDir(), t.TempDir()
	r := newProfRig(t, nil)
	r.pf.sc = newScopes(dir, cwd)
	if err := r.pf.Choose("medium", "global"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || !strings.Contains(string(b), `"profile": "medium"`) {
		t.Fatalf("config file: %v %s", err, b)
	}
	cfg, err := config.Load(filepath.Join(dir, "config.json"), true)
	if err != nil || cfg.ProfileFor("local", "m") != "medium" {
		t.Errorf("the written file does not load back as medium: %v", err)
	}
}

func TestProfiles_ChooseSaveFailureKeepsTheChoice(t *testing.T) {
	r := newProfRig(t, nil)
	blocker := filepath.Join(t.TempDir(), "blocker") // a file where the project folder should be
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.pf.sc = scopes{project: filepath.Join(blocker, "sub", "config.json")}
	err := r.pf.Choose("high", "project")
	var ns *ui.NotSavedError
	if !errors.As(err, &ns) {
		t.Fatalf("err = %v, want a NotSavedError", err)
	}
	if r.set.Profile() != prompt.High {
		t.Error("a failed save must not undo the choice")
	}
}

func TestSaving_ProfileIsRecordedReplayedAndRestoredOnOpen(t *testing.T) {
	dir := t.TempDir()
	savedSession(t, dir, "bbbbbb", "hi") // a Session with no choice
	sv, cl := newSaving(t, dir)
	if err := sv.RecordProfile("high"); err != nil {
		t.Fatal(err)
	}
	if err := sv.store.Message(chat.Message{Role: chat.RoleUser, Content: "keep me", Seq: 1}); err != nil { // an empty new file is removed
		t.Fatal(err)
	}
	path := sv.w.Path()
	var restored []string
	sv.onProfile = func(n string) { restored = append(restored, n) }
	if _, err := sv.Open("bbb"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(restored, []string{""}) {
		t.Errorf("open of a Session with no choice restored %q, want one empty call", restored)
	}
	cl.Run()

	var cl2 cleanups
	sv2, err := openSaving(dir, session.Info{Path: path}, session.Header{Cwd: "/p", Model: "m"}, true, true, 0, &cl2, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cl2.Run)
	if sv2.profile != "high" {
		t.Errorf("resumed profile %q, want high", sv2.profile)
	}
}

func TestSaving_RecordProfileWithSavingOffIsANoop(t *testing.T) {
	if err := (&saving{}).RecordProfile("high"); err != nil {
		t.Errorf("RecordProfile with no file: %v", err)
	}
}

func TestConfigView_PromptProfileRow(t *testing.T) {
	dir, cwd := t.TempDir(), t.TempDir()
	sc := newScopes(dir, cwd)
	cfg, layers, err := config.LoadLayered(sc.global, false, cwd)
	if err != nil {
		t.Fatal(err)
	}
	v := &configView{sc: sc, cwd: cwd, cfg: &cfg, layers: &layers}
	for _, s := range v.Settings() {
		if s.Key == "prompt.profile" {
			t.Fatal("the row must be absent without a profile source")
		}
	}
	v.profile = func() string { return "medium" }
	var row ui.Setting
	for _, s := range v.Settings() {
		if s.Key == "prompt.profile" {
			row = s
		}
	}
	if row.Value != "medium" || row.Live != "profile" || row.Kind != "string" {
		t.Errorf("row %+v", row)
	}
	if err := v.Set("global", "prompt.profile", "high"); err != nil || cfg.Prompt.Profile != "high" {
		t.Errorf("Set: %v, profile %q", err, cfg.Prompt.Profile)
	}
}
