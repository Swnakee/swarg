package prompt

import (
	"swarg/internal/chat"

	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const testBase = "You are swarg, a coding assistant running in the user's terminal.\n" +
	"Use the tools to read, edit and run things in the project folder; do not guess file contents.\n" +
	"Be concise. Use Markdown; put code in fenced blocks with a language tag.\n" +
	"Answer in the language the user writes in.\n" +
	"The user already sees tool output. Do not repeat it; summarize in 1-2 sentences."

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testEnv(dir string) Env {
	return Env{OS: "darwin", Shell: "zsh", Dir: dir, Date: "2026-10-26", Model: "qwen"}
}

func envBlock(dir string) string {
	return "OS: darwin\nShell: zsh\nWorking directory: " + dir +
		"\nDate: 2026-10-26\nModel: qwen"
}

func blockNames(s *Set) []string {
	var names []string
	for _, b := range s.List() {
		names = append(names, b.Name)
	}
	return names
}

func mustLoad(t *testing.T, globalDir, cwd string) (*Set, []string) {
	t.Helper()
	s, warnings, err := Load(globalDir, cwd, testEnv(cwd))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s, warnings
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

func TestLoad_BaseOnly(t *testing.T) {
	cwd := t.TempDir()
	missing := filepath.Join(t.TempDir(), "no-such-dir")

	s, warnings := mustLoad(t, missing, cwd)

	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	want := testBase + "\n\n" + envBlock(cwd)
	if got := s.Build(); got != want {
		t.Errorf("Build() =\n%q\nwant\n%q", got, want)
	}
	if got := strings.Join(blockNames(s), ","); got != "base,env" {
		t.Errorf("names = %s, want base,env", got)
	}
}

func TestBuild_OrderAndFormat(t *testing.T) {
	global := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(global, "10-a.md"), "A text")
	writeFile(t, filepath.Join(global, "02-b.md"), "B text\n")
	writeFile(t, filepath.Join(global, "20-c.md"), "C text\n\n")
	writeFile(t, filepath.Join(cwd, "SWARG.md"), "Project text\n")

	s, _ := mustLoad(t, global, cwd)

	if got := strings.Join(blockNames(s), ","); got != "base,b,a,c,project,env" {
		t.Errorf("order = %s, want base,b,a,c,project,env", got)
	}
	want := testBase +
		"\n\nB text" +
		"\n\nA text" +
		"\n\nC text" +
		"\n\nProject text" +
		"\n\n" + envBlock(cwd)
	if got := s.Build(); got != want {
		t.Errorf("Build() =\n%q\nwant\n%q", got, want)
	}
}

func TestLoad_SkipsBadNames(t *testing.T) {
	global := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(global, "30-ok.md"), "OK")
	writeFile(t, filepath.Join(global, "notes.md"), "x")
	writeFile(t, filepath.Join(global, "1-x.md"), "x")
	writeFile(t, filepath.Join(global, "10-.md"), "x")
	writeFile(t, filepath.Join(global, "10-A.md"), "x")
	writeFile(t, filepath.Join(global, "10-a.txt"), "x")
	if err := os.Mkdir(filepath.Join(global, "20-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	s, warnings := mustLoad(t, global, cwd)

	if len(warnings) != 6 {
		t.Errorf("warnings = %d, want 6: %v", len(warnings), warnings)
	}
	for _, bad := range []string{"notes.md", "1-x.md", "10-.md", "10-A.md", "10-a.txt", "20-dir"} {
		found := false
		for _, w := range warnings {
			if strings.Contains(w, bad) {
				found = true
			}
		}
		if !found {
			t.Errorf("no warning mentions %q: %v", bad, warnings)
		}
	}
	if got := strings.Join(blockNames(s), ","); got != "base,ok,env" {
		t.Errorf("names = %s, want base,ok,env", got)
	}
}

func TestLoad_DuplicateNamesError(t *testing.T) {
	tests := []struct {
		name  string
		files []string
	}{
		{"same name twice", []string{"10-a.md", "20-a.md"}},
		{"collides with base", []string{"05-base.md"}},
		{"collides with project", []string{"10-project.md"}},
		{"collides with env", []string{"10-env.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			global := t.TempDir()
			for _, f := range tt.files {
				writeFile(t, filepath.Join(global, f), "x")
			}
			s, _, err := Load(global, t.TempDir(), testEnv(""))
			if err == nil {
				t.Fatalf("Load() error = nil, want duplicate error (set=%v)", s)
			}
			if !strings.Contains(err.Error(), "duplicate") {
				t.Errorf("error = %v, want it to mention duplicate", err)
			}
			if !strings.HasPrefix(err.Error(), "prompt: ") {
				t.Errorf("error = %v, want prefix \"prompt: \"", err)
			}
		})
	}
}

func TestLoad_ProjectFileOnlyFromCwd(t *testing.T) {
	tests := []struct {
		name      string
		placeIn   string // directory that gets SWARG.md: "parent", "cwd" or "child"
		wantBlock bool
	}{
		{"in parent folder", "parent", false},
		{"in cwd", "cwd", true},
		{"in child folder", "child", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			cwd := filepath.Join(root, "parent", "cwd")
			dirs := map[string]string{
				"parent": filepath.Join(root, "parent"),
				"cwd":    cwd,
				"child":  filepath.Join(cwd, "child"),
			}
			writeFile(t, filepath.Join(dirs[tt.placeIn], "SWARG.md"), "Project rule")

			s, _ := mustLoad(t, filepath.Join(root, "no-global"), cwd)

			hasProject := strings.Contains(strings.Join(blockNames(s), ","), "project")
			if hasProject != tt.wantBlock {
				t.Errorf("project block present = %v, want %v", hasProject, tt.wantBlock)
			}
		})
	}
}

func TestLoad_Utf8AndCRLF(t *testing.T) {
	global := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(global, "10-ru.md"), "Привет\r\nмир\r\n")

	s, _ := mustLoad(t, global, cwd)

	want := "Привет\nмир\n\nOS: "
	got := s.Build()
	if !strings.Contains(got, want) {
		t.Errorf("Build() = %q, want it to contain %q", got, want)
	}
	if strings.Contains(got, "\r") {
		t.Errorf("Build() contains CR: %q", got)
	}
}

// ---------------------------------------------------------------------------
// Extra blocks
// ---------------------------------------------------------------------------

func TestLoad_ExtraBlocksSortedWithRest(t *testing.T) {
	global := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(global, "10-a.md"), "A text")
	extra := []Block{
		{Name: "tool-z", Source: SourceTool, Order: 500, Text: "Z tool", Enabled: false},
		{Name: "tool-b", Source: SourceTool, Order: 5, Text: "B tool"},
		{Name: "tool-m", Source: SourceTool, Order: 500, Text: "M tool"},
	}

	s, _, err := Load(global, cwd, testEnv(cwd), extra...)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := strings.Join(blockNames(s), ","); got != "base,tool-b,a,tool-m,tool-z,env" {
		t.Errorf("order = %s, want base,tool-b,a,tool-m,tool-z,env", got)
	}
	for _, b := range s.List() {
		if strings.HasPrefix(b.Name, "tool-") && (b.Source != SourceTool || !b.Enabled) {
			t.Errorf("extra %s = %+v, want source tool and enabled", b.Name, b)
		}
	}
	if !strings.Contains(s.Build(), "Z tool") {
		t.Errorf("Build() misses the disabled-as-given extra block: %q", s.Build())
	}
}

func TestLoad_ExtraDuplicateNamesError(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		extra []Block
	}{
		{"extra repeats a global name", []string{"10-tool-x.md"}, []Block{{Name: "tool-x"}}},
		{"two extras", nil, []Block{{Name: "tool-x"}, {Name: "tool-x"}}},
		{"extra is base", nil, []Block{{Name: "base"}}},
		{"extra is project", nil, []Block{{Name: "project"}}},
		{"extra is env", nil, []Block{{Name: "env"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			global := t.TempDir()
			for _, f := range tt.files {
				writeFile(t, filepath.Join(global, f), "x")
			}
			_, _, err := Load(global, t.TempDir(), testEnv(""), tt.extra...)
			if err == nil || !strings.Contains(err.Error(), "duplicate") {
				t.Fatalf("Load() error = %v, want duplicate error", err)
			}
		})
	}
}

func TestSetEnabled_ToolBlock(t *testing.T) {
	cwd := t.TempDir()
	extra := []Block{{Name: "tool-read_file", Source: SourceTool, Order: 500, Text: "Use read_file"}}
	s, _, err := Load(t.TempDir(), cwd, testEnv(cwd), extra...)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := s.SetEnabled("tool-read_file", false); err != nil {
		t.Fatalf("SetEnabled off: %v", err)
	}
	if strings.Contains(s.Build(), "Use read_file") {
		t.Error("Build() still contains the switched-off tool block")
	}
}

// ---------------------------------------------------------------------------
// Toggling and listing
// ---------------------------------------------------------------------------

func TestSetEnabled(t *testing.T) {
	global := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(global, "10-style.md"), "Be terse")
	s, _ := mustLoad(t, global, cwd)

	if err := s.SetEnabled("style", false); err != nil {
		t.Fatalf("SetEnabled off: %v", err)
	}
	if strings.Contains(s.Build(), "Be terse") {
		t.Error("Build() still contains the disabled block")
	}
	if err := s.SetEnabled("style", false); err != nil {
		t.Errorf("SetEnabled off again: %v, want idempotent", err)
	}
	if err := s.SetEnabled("style", true); err != nil {
		t.Fatalf("SetEnabled on: %v", err)
	}
	if !strings.Contains(s.Build(), "Be terse") {
		t.Error("Build() does not contain the re-enabled block")
	}

	err := s.SetEnabled("nope", false)
	if err == nil {
		t.Fatal("SetEnabled unknown name: error = nil")
	}
	for _, known := range []string{"base", "style", "env"} {
		if !strings.Contains(err.Error(), known) {
			t.Errorf("error = %v, want it to list %q", err, known)
		}
	}
}

func TestList_Tokens(t *testing.T) {
	global := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(global, "10-style.md"), "abcdefgh") // 8 bytes -> 2 tokens
	writeFile(t, filepath.Join(cwd, "SWARG.md"), "12345")          // 5 bytes -> 2 tokens
	s, _ := mustLoad(t, global, cwd)
	if err := s.SetEnabled("style", false); err != nil {
		t.Fatal(err)
	}

	got := map[string]BlockInfo{}
	for _, b := range s.List() {
		got[b.Name] = b
	}
	if len(got) != 4 {
		t.Fatalf("List() has %d blocks, want 4", len(got))
	}
	checks := []struct {
		name    string
		source  Source
		enabled bool
		tokens  int
	}{
		{"base", SourceBase, true, chat.EstimateTokens(testBase)},
		{"style", SourceGlobal, false, 2},
		{"project", SourceProject, true, 2},
		{"env", SourceEnv, true, chat.EstimateTokens(strings.TrimPrefix(envBlock(cwd), ""))},
	}
	for _, c := range checks {
		b, ok := got[c.name]
		if !ok {
			t.Errorf("block %q missing from List()", c.name)
			continue
		}
		if b.Source != c.source || b.Enabled != c.enabled || b.Tokens != c.tokens {
			t.Errorf("%s = %+v, want source=%s enabled=%v tokens=%d",
				c.name, b, c.source, c.enabled, c.tokens)
		}
	}
}

// ---------------------------------------------------------------------------
// Tokens
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

func TestSet_ConcurrentToggleAndBuild(t *testing.T) {
	global := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(global, "10-a.md"), "A")
	writeFile(t, filepath.Join(global, "20-b.md"), "B")
	s, _ := mustLoad(t, global, cwd)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if err := s.SetEnabled("a", j%2 == 0); err != nil {
					t.Errorf("SetEnabled: %v", err)
				}
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = s.Build()
				_ = s.List()
			}
		}()
	}
	wg.Wait()
}

func TestBase_RuleC1_NoRepeatingToolOutput(t *testing.T) {
	s, _ := mustLoad(t, t.TempDir(), t.TempDir())
	if !strings.Contains(s.Build(), "The user already sees tool output. Do not repeat it; summarize in 1-2 sentences.") {
		t.Error("base block lost rule C1")
	}
}

// Bug (found 2026-10-27): base.md still said the agent could not run commands or edit files,
// while every request carries the tools. The base text must never deny the tools.
func TestBase_DoesNotDenyTools(t *testing.T) {
	for _, bad := range []string{"cannot run commands", "answer with text only", "edit files yet"} {
		if strings.Contains(baseText, bad) {
			t.Errorf("base.md says %q, but the agent has tools", bad)
		}
	}
}

// Bug (e2e 2026-10-27, Qwen3.5-2B): the system prompt labelled each block "<!-- BLOCK: tool-read_file -->";
// the model then called "tool-read_file" and "tool-grep", which do not exist. Block names are
// internal: the built prompt must not contain them.
func TestBuild_NoInternalBlockNames(t *testing.T) {
	cwd := t.TempDir()
	extra := []Block{{Name: "tool-read_file", Source: SourceTool, Order: 5, Text: "read_file: read a file"}}
	s, _, err := Load(t.TempDir(), cwd, testEnv(cwd), extra...)
	if err != nil {
		t.Fatal(err)
	}
	got := s.Build()
	for _, bad := range []string{"BLOCK:", "tool-read_file", "<!--"} {
		if strings.Contains(got, bad) {
			t.Errorf("system prompt contains %q:\n%s", bad, got)
		}
	}
}

// ---------------------------------------------------------------------------
// SetModel (R-30): the env block names the chosen model
// ---------------------------------------------------------------------------

func TestSetModel_UpdatesEnvBlock(t *testing.T) {
	cwd := t.TempDir()
	s, _ := mustLoad(t, filepath.Join(t.TempDir(), "none"), cwd)
	before := s.Build()
	s.SetModel("big-model")
	after := s.Build()
	if !strings.Contains(after, "Model: big-model") || strings.Contains(after, "Model: "+testEnv(cwd).Model+"\n") {
		t.Fatalf("env block not updated:\n%s", after)
	}
	if strings.Replace(before, "Model: "+testEnv(cwd).Model, "Model: big-model", 1) != after {
		t.Errorf("something besides the model line changed:\n%s\n---\n%s", before, after)
	}
}

func TestSetModel_KeepsDisabledEnvBlockOff(t *testing.T) {
	cwd := t.TempDir()
	s, _ := mustLoad(t, filepath.Join(t.TempDir(), "none"), cwd)
	if err := s.SetEnabled("env", false); err != nil {
		t.Fatal(err)
	}
	s.SetModel("big-model")
	if strings.Contains(s.Build(), "big-model") {
		t.Errorf("a disabled env block came back:\n%s", s.Build())
	}
}
