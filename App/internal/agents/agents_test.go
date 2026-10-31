package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeFile(t *testing.T, dir, name, text string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func projectAgents(root string) string { return filepath.Join(root, filepath.FromSlash(ProjectDir)) }

func mustGet(t *testing.T, s *Set, name string) Type {
	t.Helper()
	ty, ok := s.Get(name)
	if !ok {
		t.Fatalf("type %q not found", name)
	}
	return ty
}

func hasWarning(ws []string, sub string) bool {
	for _, w := range ws {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

func TestLoad_BuiltinsPresentAndEnabled(t *testing.T) {
	s, ws := Load(filepath.Join(t.TempDir(), "none"), t.TempDir(), nil)
	if len(ws) != 0 {
		t.Fatalf("warnings: %v", ws)
	}
	for _, name := range []string{"explore", "general", "plan", "review"} {
		ty := mustGet(t, s, name)
		if !ty.Enabled || ty.Source != SourceBuiltin || ty.Path != "" || ty.Description == "" || ty.Body == "" {
			t.Errorf("%s: %+v", name, ty)
		}
	}
	if got := mustGet(t, s, "review").Tools; !reflect.DeepEqual(got, []string{"readonly", "shell"}) {
		t.Errorf("review tools: %v", got)
	}
	if len(s.List()) != 4 {
		t.Errorf("list: %v", s.List())
	}
}

func TestLoad_GlobalFileLoaded(t *testing.T) {
	g := t.TempDir()
	p := writeFile(t, g, "docs.md", "---\ndescription: Writes docs\nserver: other\nmodel: m1\nprofile: full\ntools: read_file, grep,,\nmax_steps: 7\nunknown: x\n---\r\nDo the docs.\r\n")
	s, ws := Load(g, t.TempDir(), nil)
	if len(ws) != 0 {
		t.Fatalf("warnings: %v", ws)
	}
	ty := mustGet(t, s, "docs")
	want := Type{Name: "docs", Description: "Writes docs", Server: "other", Model: "m1", Profile: "full",
		Tools: []string{"read_file", "grep"}, MaxSteps: 7, Body: "Do the docs.", Source: SourceGlobal, Path: p, Enabled: true}
	if !reflect.DeepEqual(ty, want) {
		t.Errorf("got %+v\nwant %+v", ty, want)
	}
}

func TestLoad_ToolsDefaultToEverything(t *testing.T) {
	g := t.TempDir()
	writeFile(t, g, "a.md", "---\ndescription: d\n---\nbody")
	s, _ := Load(g, t.TempDir(), nil)
	if got := mustGet(t, s, "a").Tools; !reflect.DeepEqual(got, []string{"*"}) {
		t.Errorf("tools: %v", got)
	}
}

func TestLoad_ProjectReplacesGlobalReplacesBuiltin(t *testing.T) {
	g, proj := t.TempDir(), t.TempDir()
	writeFile(t, g, "explore.md", "---\ndescription: global explore\n---\nG")
	writeFile(t, g, "plan.md", "---\ndescription: global plan\n---\nG")
	writeFile(t, projectAgents(proj), "plan.md", "---\ndescription: project plan\n---\nP")
	s, ws := Load(g, proj, nil)
	if len(ws) != 0 {
		t.Fatalf("warnings: %v", ws)
	}
	if ty := mustGet(t, s, "explore"); ty.Source != SourceGlobal || ty.Description != "global explore" {
		t.Errorf("explore: %+v", ty)
	}
	if ty := mustGet(t, s, "plan"); ty.Source != SourceProject || ty.Body != "P" {
		t.Errorf("plan: %+v", ty)
	}
	if ty := mustGet(t, s, "general"); ty.Source != SourceBuiltin {
		t.Errorf("general: %+v", ty)
	}
}

func TestLoad_BadFilesSkippedWithWarning(t *testing.T) {
	cases := []struct {
		name, text, warn string
	}{
		{"nodesc", "---\ntools: grep\n---\nbody", "no description"},
		{"nofront", "just text", "no description"},
		{"steps0", "---\ndescription: d\nmax_steps: 0\n---\n", "max_steps"},
		{"steps201", "---\ndescription: d\nmax_steps: 201\n---\n", "max_steps"},
		{"stepsabc", "---\ndescription: d\nmax_steps: abc\n---\n", "max_steps"},
		{"badprof", "---\ndescription: d\nprofile: huge\n---\n", "profile"},
		{"Bad_Name", "---\ndescription: d\n---\n", "file name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := t.TempDir()
			writeFile(t, g, c.name+".md", c.text)
			s, ws := Load(g, t.TempDir(), nil)
			if !hasWarning(ws, c.warn) || !hasWarning(ws, "agents: ") {
				t.Errorf("warnings: %v", ws)
			}
			if _, ok := s.Get(c.name); ok {
				t.Errorf("%s was loaded", c.name)
			}
		})
	}
}

func TestLoad_ProfileInheritAndEmptyAccepted(t *testing.T) {
	g := t.TempDir()
	writeFile(t, g, "a.md", "---\ndescription: d\nprofile: inherit\n---\n")
	writeFile(t, g, "b.md", "---\ndescription: d\n---\n")
	s, ws := Load(g, t.TempDir(), nil)
	if len(ws) != 0 {
		t.Fatalf("warnings: %v", ws)
	}
	if mustGet(t, s, "a").Profile != "inherit" || mustGet(t, s, "b").Profile != "" {
		t.Error("profile values changed")
	}
}

func TestLoad_IgnoresNonMarkdownAndSubfolders(t *testing.T) {
	g := t.TempDir()
	writeFile(t, g, "note.txt", "---\ndescription: d\n---\n")
	writeFile(t, filepath.Join(g, "sub"), "x.md", "---\ndescription: d\n---\n")
	if err := os.Mkdir(filepath.Join(g, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, ws := Load(g, t.TempDir(), nil)
	if len(ws) != 0 || len(s.List()) != 4 {
		t.Errorf("warnings %v, list %v", ws, s.List())
	}
}

func TestLoad_DisabledStartOffAndUnknownWarns(t *testing.T) {
	s, ws := Load(t.TempDir(), t.TempDir(), []string{"plan", "nosuch"})
	if mustGet(t, s, "plan").Enabled {
		t.Error("plan should start disabled")
	}
	if !hasWarning(ws, `agents: unknown agent type "nosuch"`) || len(ws) != 1 {
		t.Errorf("warnings: %v", ws)
	}
}

// ---------------------------------------------------------------------------
// Set
// ---------------------------------------------------------------------------

func TestSetEnabled_UnknownNameListsKnown(t *testing.T) {
	s, _ := Load(t.TempDir(), t.TempDir(), nil)
	err := s.SetEnabled("nosuch", true)
	if err == nil || !strings.Contains(err.Error(), "explore, general, plan, review") {
		t.Errorf("error: %v", err)
	}
}

func TestEnabled_ExcludesDisabledAndReturnsCopies(t *testing.T) {
	s, _ := Load(t.TempDir(), t.TempDir(), nil)
	if err := s.SetEnabled("plan", false); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, ty := range s.Enabled() {
		names = append(names, ty.Name)
	}
	if !reflect.DeepEqual(names, []string{"explore", "general", "review"}) {
		t.Errorf("names: %v", names)
	}
	s.Enabled()[0].Tools[0] = "changed"
	if mustGet(t, s, "explore").Tools[0] != "readonly" {
		t.Error("Enabled leaked the internal Tools slice")
	}
	if ty := mustGet(t, s, "plan"); ty.Enabled {
		t.Error("Get should still return the disabled type, disabled")
	}
}

func TestBlock_ListsEnabledTypesInOrder(t *testing.T) {
	s, _ := Load(t.TempDir(), t.TempDir(), nil)
	if err := s.SetEnabled("review", false); err != nil {
		t.Fatal(err)
	}
	b := s.Block()
	if !strings.HasPrefix(b, "Sub-agents: hand a self-contained job") {
		t.Errorf("head: %q", b)
	}
	if !strings.Contains(b, "It returns one short report.\nTypes:\n- explore: ") ||
		!strings.Contains(b, "\n- general: Does a self-contained job") ||
		!strings.Contains(b, "\n- plan: ") ||
		!strings.Contains(b, "\nDelegate when: ") ||
		!strings.HasSuffix(b, "before you rely on it.") {
		t.Errorf("block: %q", b)
	}
	if strings.Contains(b, "- review:") {
		t.Error("disabled type is mentioned")
	}
	if strings.Index(b, "- explore:") > strings.Index(b, "- general:") || strings.Index(b, "- general:") > strings.Index(b, "- plan:") {
		t.Error("types are not in name order")
	}
	if strings.Contains(b, "\n\n") {
		t.Errorf("empty line in block: %q", b)
	}
}

func TestBlock_EmptyWhenAllDisabled(t *testing.T) {
	s, _ := Load(t.TempDir(), t.TempDir(), nil)
	for _, i := range s.List() {
		_ = s.SetEnabled(i.Name, false)
	}
	if b := s.Block(); b != "" {
		t.Errorf("block: %q", b)
	}
}

func TestBlock_ClipsLongDescriptionAndCollapsesNewlines(t *testing.T) {
	g := t.TempDir()
	long := strings.Repeat("x", 300)
	writeFile(t, g, "long.md", "---\ndescription: "+long+"\n---\n")
	s, _ := Load(g, t.TempDir(), nil)
	ty, _ := s.Get("long")
	ty.Description = "a\nb  c"
	s.mu.Lock()
	s.find("long").Description = ty.Description
	s.mu.Unlock()
	if !strings.Contains(s.Block(), "- long: a b c\n") {
		t.Errorf("newlines not collapsed: %q", s.Block())
	}
	s.mu.Lock()
	s.find("long").Description = long
	s.mu.Unlock()
	if !strings.Contains(s.Block(), "- long: "+strings.Repeat("x", 160)+"...\n") {
		t.Errorf("not clipped: %q", s.Block())
	}
}

func TestSet_ConcurrentListAndSetEnabled(t *testing.T) {
	s, _ := Load(t.TempDir(), t.TempDir(), nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = s.SetEnabled("plan", (i+j)%2 == 0)
				s.List()
				s.Enabled()
				s.Block()
				s.Get("plan")
			}
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// NewFile
// ---------------------------------------------------------------------------

func TestNewFile_CreatesDirAndLoadsBack(t *testing.T) {
	root := t.TempDir()
	dir := projectAgents(root)
	p, err := NewFile(dir, "my-agent")
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(dir, "my-agent.md") {
		t.Errorf("path: %s", p)
	}
	s, ws := Load(t.TempDir(), root, nil)
	if len(ws) != 0 {
		t.Fatalf("warnings: %v", ws)
	}
	ty := mustGet(t, s, "my-agent")
	if ty.Source != SourceProject || ty.Path != p || !strings.HasPrefix(ty.Description, "TODO") ||
		ty.MaxSteps != 15 || !reflect.DeepEqual(ty.Tools, []string{"readonly"}) || ty.Body == "" {
		t.Errorf("loaded: %+v", ty)
	}
}

func TestNewFile_RefusesExistingAndInvalidNames(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewFile(dir, "dup"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "dup.md"))
	if _, err := NewFile(dir, "dup"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("existing: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "dup.md"))
	if string(before) != string(after) {
		t.Error("existing file was changed")
	}
	for _, bad := range []string{"../x", "A", "", "a b", strings.Repeat("a", 33)} {
		if _, err := NewFile(dir, bad); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("extra files: %v", entries)
	}
}

func TestValidName_Shapes(t *testing.T) {
	for name, want := range map[string]bool{"a": true, "a_b-1": true, "Ab": false, "": false, "a.b": false, strings.Repeat("a", 32): true, strings.Repeat("a", 33): false} {
		if ValidName(name) != want {
			t.Errorf("ValidName(%q) != %s", name, strconv.FormatBool(want))
		}
	}
}

// ---------------------------------------------------------------------------
// Global agents (R-128, R-129)
// ---------------------------------------------------------------------------

func TestLoad_PrecedenceProjectOverGlobalOverBuiltin(t *testing.T) {
	g, root := t.TempDir(), t.TempDir()
	writeFile(t, g, "explore.md", "---\ndescription: global explore\n---\nG")
	writeFile(t, g, "plan.md", "---\ndescription: global plan\n---\nG")
	writeFile(t, projectAgents(root), "plan.md", "---\ndescription: project plan\n---\nP")
	s, _ := Load(g, root, nil)
	for name, want := range map[string]string{"explore": SourceGlobal, "plan": SourceProject, "review": SourceBuiltin} {
		if got := mustGet(t, s, name).Source; got != want {
			t.Errorf("%s source = %s, want %s", name, got, want)
		}
	}
}

func TestExportBuiltin_CopiesFullDefinitionAndLoadsBack(t *testing.T) {
	dir := t.TempDir()
	path, created, err := ExportBuiltin(dir, "review")
	if err != nil || !created || path != filepath.Join(dir, "review.md") {
		t.Fatalf("got %q %v %v", path, created, err)
	}
	s, ws := Load(dir, t.TempDir(), nil)
	if len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	got, want := mustGet(t, s, "review"), builtinByName(t, "review")
	if got.Source != SourceGlobal || got.Description != want.Description || got.Body != want.Body ||
		!reflect.DeepEqual(got.Tools, want.Tools) {
		t.Fatalf("copy differs: %+v vs %+v", got, want)
	}
}

func builtinByName(t *testing.T, name string) Type {
	t.Helper()
	for _, b := range builtins() {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("no built-in %q", name)
	return Type{}
}

func TestExportBuiltin_NeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "plan.md", "mine")
	path, created, err := ExportBuiltin(dir, "plan")
	if err != nil || created || path != p {
		t.Fatalf("got %q %v %v", path, created, err)
	}
	if b, _ := os.ReadFile(p); string(b) != "mine" {
		t.Fatalf("file overwritten: %q", b)
	}
}

func TestExportBuiltin_RefusesNonBuiltin(t *testing.T) {
	if _, _, err := ExportBuiltin(t.TempDir(), "mine"); err == nil {
		t.Fatal("a non-built-in name must be refused")
	}
}

func TestReload_PicksUpEditsAndKeepsOffState(t *testing.T) {
	g := t.TempDir()
	s, _ := Load(g, t.TempDir(), []string{"review"})
	if changed, _ := s.Reload(); changed {
		t.Fatal("nothing changed, yet Reload says so")
	}
	writeFile(t, g, "plan.md", "---\ndescription: edited plan\n---\nmy body")
	writeFile(t, g, "extra.md", "---\ndescription: extra\n---\nx")
	if changed, _ := s.Reload(); !changed {
		t.Fatal("new files not noticed")
	}
	if p := mustGet(t, s, "plan"); p.Description != "edited plan" || p.Source != SourceGlobal {
		t.Fatalf("plan = %+v", p)
	}
	if mustGet(t, s, "review").Enabled || !mustGet(t, s, "extra").Enabled {
		t.Error("off state lost or new type not on")
	}
	if err := os.Remove(filepath.Join(g, "plan.md")); err != nil {
		t.Fatal(err)
	}
	s.Reload()
	if mustGet(t, s, "plan").Source != SourceBuiltin {
		t.Error("removing the global file must bring the built-in back")
	}
}
