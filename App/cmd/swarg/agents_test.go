package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"swarg/internal/agents"
	"swarg/internal/config"
	"swarg/internal/prompt"
	"swarg/internal/tools"
)

// ---------------------------------------------------------------------------
// Sub-agents in the app (R-120..R-123, I-40)
// ---------------------------------------------------------------------------

type hostRig struct {
	h    *agentHost
	set  *agents.Set
	ps   *prompt.Set
	cfg  *config.Config
	prof string
}

func newHostRig(t *testing.T) *hostRig {
	t.Helper()
	isolateKeys(t)
	cfg := config.Defaults()
	ts, err := tools.NewSet(tools.All(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ps, _, err := prompt.Load(t.TempDir(), t.TempDir(), prompt.Env{Model: "m"}, ts.Blocks()...)
	if err != nil {
		t.Fatal(err)
	}
	set, _ := agents.Load(t.TempDir(), t.TempDir(), nil)
	srcs, _ := testSources(t, &cfg)
	r := &hostRig{set: set, ps: ps, cfg: &cfg, prof: "standard"}
	r.h = &agentHost{set: set, cfg: &cfg, ps: ps, srcs: srcs, runner: toolRunner{set: ts},
		profile: func() string { return r.prof }, model: func() string { return "session-model" }}
	return r
}

// isolateKeys keeps the user's real keys file out of a test.
func isolateKeys(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	os.Unsetenv("OPENROUTER_API_KEY")
}

func TestAgentHost_CompactOffersNoTypes(t *testing.T) {
	r := newHostRig(t)
	r.prof = "compact"
	if got := r.h.Types(); len(got) != 0 {
		t.Fatalf("compact offers %d types, want none", len(got))
	}
	r.prof = "standard"
	if len(r.h.Types()) == 0 {
		t.Fatal("standard offers no types")
	}
	r.prof = "full"
	if len(r.h.Types()) == 0 {
		t.Fatal("full offers no types")
	}
}

func TestAgentHost_DisabledTypeIsNotOffered(t *testing.T) {
	r := newHostRig(t)
	if err := r.set.SetEnabled("review", false); err != nil {
		t.Fatal(err)
	}
	for _, ty := range r.h.Types() {
		if ty.Name == "review" {
			t.Fatal("a disabled type is offered")
		}
	}
}

func TestAgentHost_OpenInheritsTheSessionSourceAndModel(t *testing.T) {
	r := newHostRig(t)
	run, err := r.h.Open(r.h.Types()[0])
	if err != nil {
		t.Fatal(err)
	}
	if run.Source.Name != r.cfg.Provider || run.Source.Model != "session-model" {
		t.Fatalf("source %q model %q", run.Source.Name, run.Source.Model)
	}
}

func TestAgentHost_OpenOtherServerNeedsItsKey(t *testing.T) {
	r := newHostRig(t)
	ty := r.h.Types()[0]
	ty.Server = "openrouter"
	if _, err := r.h.Open(ty); err == nil || !strings.Contains(err.Error(), "no key") {
		t.Fatalf("err = %v, want the missing key named", err)
	}
	ty.Server = "nowhere"
	if _, err := r.h.Open(ty); err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("err = %v, want an undefined provider", err)
	}
}

func TestAgentHost_OpenExplicitModel(t *testing.T) {
	r := newHostRig(t)
	ty := r.h.Types()[0]
	ty.Model = "other-model"
	run, err := r.h.Open(ty)
	if err != nil || run.Source.Model != "other-model" {
		t.Fatalf("run %+v err %v", run.Source, err)
	}
}

func TestAgentHost_PromptCarriesOnlyAllowedToolLines(t *testing.T) {
	r := newHostRig(t)
	var explore, general string
	for _, ty := range r.h.Types() {
		run, err := r.h.Open(ty)
		if err != nil {
			t.Fatal(err)
		}
		switch ty.Name {
		case "explore":
			explore = run.System
		case "general":
			general = run.System
		}
	}
	if !strings.Contains(general, "edit_file") || strings.Contains(explore, "edit_file") || !strings.Contains(explore, "read_file") {
		t.Fatalf("tool lines: explore has edit_file=%v read_file=%v, general has edit_file=%v",
			strings.Contains(explore, "edit_file"), strings.Contains(explore, "read_file"), strings.Contains(general, "edit_file"))
	}
	for _, s := range []string{explore, general} {
		if strings.Contains(s, "Sub-agents:") {
			t.Error("an agent prompt carries the sub-agent policy")
		}
	}
}

func TestAgentHost_SlotsFollowTheProvider(t *testing.T) {
	r := newHostRig(t)
	if r.h.Slots("local") != 1 || r.h.Slots("openrouter") != 4 {
		t.Fatalf("slots local %d openrouter %d, want 1 and 4", r.h.Slots("local"), r.h.Slots("openrouter"))
	}
	if r.h.Deps().Max != 4 {
		t.Fatalf("Max = %d", r.h.Deps().Max)
	}
}

func TestAgentsView_SwitchRewritesTheLayerAndSaves(t *testing.T) {
	r := newHostRig(t)
	dir, cwd := t.TempDir(), t.TempDir()
	v := agentsView{set: r.set, ps: r.ps, sc: newScopes(dir, cwd), cwd: cwd, dir: filepath.Join(dir, "agents")}
	if err := r.ps.SetText(subagentsBlock, r.set.Block()); err != nil {
		t.Fatal(err)
	}
	_ = r.ps.SetProfile(prompt.Standard)
	if !strings.Contains(r.ps.Build(), "- review:") {
		t.Fatal("review is missing from the layer")
	}
	if err := v.SetEnabled("review", false, "global"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.ps.Build(), "- review:") {
		t.Error("a disabled type is still described in the prompt")
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || !strings.Contains(string(data), `"review"`) {
		t.Fatalf("config.json = %q, %v", data, err)
	}
}

func TestAgentsView_NewWritesInTheScopeFolder(t *testing.T) {
	r := newHostRig(t)
	dir, cwd := t.TempDir(), t.TempDir()
	v := agentsView{set: r.set, ps: r.ps, sc: newScopes(dir, cwd), cwd: cwd, dir: filepath.Join(dir, "agents")}
	p, err := v.New("mine", "project")
	if err != nil || !strings.HasPrefix(p, cwd) || !strings.HasSuffix(p, filepath.Join(".swarg", "agents", "mine.md")) {
		t.Fatalf("project path %q, %v", p, err)
	}
	g, err := v.New("mine", "")
	if err != nil || !strings.HasPrefix(g, dir) {
		t.Fatalf("global path %q, %v", g, err)
	}
	if _, err := v.New("mine", "elsewhere"); err == nil {
		t.Error("an unknown scope must be refused")
	}
}

func TestStandardPrompt_ListsEnabledTypesAndCompactDoesNot(t *testing.T) {
	r := newHostRig(t)
	if err := r.ps.SetText(subagentsBlock, r.set.Block()); err != nil {
		t.Fatal(err)
	}
	compact := r.ps.Build()
	_ = r.ps.SetProfile(prompt.Standard)
	std := r.ps.Build()
	if strings.Contains(compact, "Sub-agents:") || strings.Contains(compact, "explore") {
		t.Error("compact describes sub-agents (I-40)")
	}
	if !strings.Contains(std, "- explore:") || !strings.Contains(std, "agent tool") {
		t.Error("standard does not list the types")
	}
}

// ---------------------------------------------------------------------------
// Global agents (R-128, R-129)
// ---------------------------------------------------------------------------

func globalRig(t *testing.T) (*hostRig, agentsView, string, string) {
	r := newHostRig(t)
	dir, cwd := t.TempDir(), t.TempDir()
	set, _ := agents.Load(filepath.Join(dir, "agents"), cwd, nil)
	r.set, r.h.set = set, set
	return r, agentsView{set: set, ps: r.ps, sc: newScopes(dir, cwd), cwd: cwd, dir: filepath.Join(dir, "agents")}, dir, cwd
}

func TestAgentsView_GlobalSwitchWritesGlobalFileEvenWithProjectFolder(t *testing.T) {
	_, v, dir, cwd := globalRig(t)
	if err := os.MkdirAll(filepath.Join(cwd, ".swarg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := v.SetEnabled("plan", false, "global"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "config.json")); !strings.Contains(string(data), `"plan"`) {
		t.Fatalf("global config.json = %q", data)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".swarg", "config.json")); err == nil {
		t.Error("the project config was written")
	}
}

func TestAgentsView_EditCopiesBuiltinOnceAndNeverOverwrites(t *testing.T) {
	_, v, dir, _ := globalRig(t)
	path, created, err := v.Edit("explore")
	want := filepath.Join(dir, "agents", "explore.md")
	if err != nil || !created || path != want {
		t.Fatalf("got %q %v %v", path, created, err)
	}
	if err := os.WriteFile(path, []byte("---\ndescription: mine\n---\nmy body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path2, created2, err := v.Edit("explore")
	if err != nil || created2 || path2 != want {
		t.Fatalf("second edit: %q %v %v", path2, created2, err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "my body") {
		t.Fatalf("overwritten: %q", b)
	}
	if _, _, err := v.Edit("nope"); err == nil {
		t.Error("an unknown type must be refused")
	}
}

func TestAgentsView_EditOfAFileTypeReturnsItsPath(t *testing.T) {
	_, v, _, _ := globalRig(t)
	mine, err := v.New("mine", "")
	if err != nil {
		t.Fatal(err)
	}
	got, created, err := v.Edit("mine")
	if err != nil || created || got != mine {
		t.Fatalf("got %q %v %v", got, created, err)
	}
}

func TestAgentsView_NewDefaultsToTheGlobalFolder(t *testing.T) {
	_, v, dir, _ := globalRig(t)
	p, err := v.New("mine", "")
	if err != nil || p != filepath.Join(dir, "agents", "mine.md") {
		t.Fatalf("path %q, %v", p, err)
	}
}

func TestAgentsView_EditedGlobalFileAppliesWithoutRestart(t *testing.T) {
	r, v, _, _ := globalRig(t)
	_ = r.ps.SetProfile(prompt.Standard)
	path, _, err := v.Edit("explore")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\ndescription: tuned explorer\ntools: readonly\n---\nTUNED BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var body, desc string
	for _, ty := range r.h.Types() { // the next agent run
		if ty.Name == "explore" {
			body, desc = ty.Body, ty.Description
		}
	}
	if body != "TUNED BODY" || desc != "tuned explorer" {
		t.Fatalf("body %q desc %q", body, desc)
	}
	if !strings.Contains(r.ps.Build(), "- explore: tuned explorer") {
		t.Error("the prompt layer was not rewritten")
	}
	for _, row := range v.List() {
		if row.Name == "explore" && row.Source != agents.SourceGlobal {
			t.Errorf("source = %s, want global", row.Source)
		}
	}
}
