package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"swarg/internal/tools"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// write puts a SKILL.md (and extra files) of Skill name under dir.
func write(t *testing.T, dir, name, front, body string, extra map[string]string) {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	text := body
	if front != "" {
		text = "---\n" + front + "\n---\n" + body
	}
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	for n, b := range extra {
		p := filepath.Join(d, filepath.FromSlash(n))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// load builds a global dir and a project dir from the given Skills.
func load(t *testing.T, global, project map[string][2]string, disabled ...string) (*Set, []string) {
	t.Helper()
	g, p := t.TempDir(), t.TempDir()
	for n, fb := range global {
		write(t, g, n, fb[0], fb[1], nil)
	}
	for n, fb := range project {
		write(t, filepath.Join(p, ".swarg", "skills"), n, fb[0], fb[1], nil)
	}
	return Load(g, p, disabled)
}

func run(t *testing.T, set *Set, args string) (tools.Result, error) {
	t.Helper()
	return set.Tool().Run(context.Background(), tools.Env{}, json.RawMessage(args))
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

func TestLoad_ProjectWinsOverGlobal(t *testing.T) {
	set, warnings := load(t,
		map[string][2]string{"deploy": {"description: global one", "G"}, "lint": {"description: lint it", "L"}},
		map[string][2]string{"deploy": {"description: project one", "P"}})
	if len(warnings) != 0 {
		t.Fatalf("warnings %q", warnings)
	}
	list := set.List()
	if len(list) != 2 || list[0].Name != "deploy" || list[0].Source != SourceProject || list[0].Description != "project one" || list[1].Name != "lint" {
		t.Fatalf("list %+v", list)
	}
	res, err := run(t, set, `{"name":"deploy"}`)
	if err != nil || res.Output != "P" {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestLoad_SkipsWithoutDescription(t *testing.T) {
	set, warnings := load(t, map[string][2]string{"bad": {"name: bad", "x"}, "none": {"", "no front matter"}, "ok": {"description: fine", "y"}}, nil)
	if len(warnings) != 2 || !strings.Contains(warnings[0], "description") {
		t.Fatalf("warnings %q", warnings)
	}
	if l := set.List(); len(l) != 1 || l[0].Name != "ok" {
		t.Fatalf("list %+v", l)
	}
}

func TestLoad_FolderNameWinsAndBadFolderIsSkipped(t *testing.T) {
	set, warnings := load(t, map[string][2]string{"deploy": {"name: shipit\ndescription: \"ship it\"", "x"}, "Bad Name": {"description: d", "x"}}, nil)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "folder name") {
		t.Fatalf("warnings %q", warnings)
	}
	if l := set.List(); len(l) != 1 || l[0].Name != "deploy" || l[0].Description != "ship it" {
		t.Fatalf("list %+v", l)
	}
}

func TestLoad_MissingDirsGiveEmptySet(t *testing.T) {
	set, warnings := Load(filepath.Join(t.TempDir(), "none"), t.TempDir(), nil)
	if set.Len() != 0 || len(warnings) != 0 || set.Block() != "" {
		t.Fatalf("set %d warnings %q block %q", set.Len(), warnings, set.Block())
	}
}

func TestLoad_DisabledAndUnknownDisabled(t *testing.T) {
	set, warnings := load(t, map[string][2]string{"a": {"description: A", "x"}, "b": {"description: B", "y"}}, nil, "b", "zzz")
	if len(warnings) != 1 || !strings.Contains(warnings[0], `unknown skill "zzz"`) {
		t.Fatalf("warnings %q", warnings)
	}
	if l := set.List(); !l[0].Enabled || l[1].Enabled {
		t.Fatalf("list %+v", l)
	}
	if _, err := run(t, set, `{"name":"b"}`); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("err %v", err)
	}
}

func TestLoad_WarnsAboveTwenty(t *testing.T) {
	g := map[string][2]string{}
	for i := 0; i < 21; i++ {
		g[strings.Repeat("s", i+1)] = [2]string{"description: d", "x"}
	}
	_, warnings := load(t, g, nil)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "21 skills listed") {
		t.Fatalf("warnings %q", warnings)
	}
}

// ---------------------------------------------------------------------------
// Block
// ---------------------------------------------------------------------------

func TestBlock_ListsEnabledOnlyAndCutsDescriptions(t *testing.T) {
	long := strings.Repeat("d", 200)
	set, _ := load(t, map[string][2]string{"a": {"description: " + long, "x"}, "b": {"description: B", "y"}}, nil, "b")
	block := set.Block()
	if !strings.HasPrefix(block, "Skills (apply one with the skill tool") || strings.Contains(block, "- b:") {
		t.Fatalf("block %q", block)
	}
	if !strings.Contains(block, "- a: "+strings.Repeat("d", 160)+"...") || strings.Contains(block, strings.Repeat("d", 161)) {
		t.Fatalf("description not cut: %q", block)
	}
	if err := set.SetEnabled("b", true); err != nil || !strings.Contains(set.Block(), "- b: B") {
		t.Fatalf("re-enable: %v %q", err, set.Block())
	}
	if err := set.SetEnabled("nope", true); err == nil || !strings.Contains(err.Error(), "known skills: a, b") {
		t.Fatalf("unknown: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tool
// ---------------------------------------------------------------------------

func TestTool_BodyWithoutFrontMatterAndFileList(t *testing.T) {
	g := t.TempDir()
	write(t, g, "deploy", "name: deploy\ndescription: ship", "Step 1\r\nStep 2\r\n", map[string]string{"checklist.md": "- a\n- b", "ref/notes.txt": "n"})
	set, _ := Load(g, t.TempDir(), nil)
	res, err := run(t, set, `{"name":"deploy"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "Step 1\nStep 2\n\nfiles: checklist.md, ref/notes.txt" || res.Brief != "4 lines" {
		t.Fatalf("res %+v", res)
	}
	res, err = run(t, set, `{"name":"deploy","file":"checklist.md"}`)
	if err != nil || res.Output != "- a\n- b" {
		t.Fatalf("file: %+v %v", res, err)
	}
	if stub := set.Tool().(tools.Stubber).Stub(json.RawMessage(`{"name":"deploy"}`), res); stub != "[skill deploy: applied earlier; call it again if needed]" {
		t.Fatalf("stub %q", stub)
	}
}

func TestTool_FileInsideFolderOnly(t *testing.T) {
	g := t.TempDir()
	write(t, g, "deploy", "description: ship", "body", nil)
	os.WriteFile(filepath.Join(g, "secret.txt"), []byte("s"), 0o644)
	set, _ := Load(g, t.TempDir(), nil)
	for _, f := range []string{"../secret.txt", "/etc/passwd", "missing.md"} {
		_, err := run(t, set, `{"name":"deploy","file":"`+f+`"}`)
		if err == nil || !(strings.Contains(err.Error(), "outside the skill folder") || strings.Contains(err.Error(), "no file")) {
			t.Fatalf("%s: err %v", f, err)
		}
	}
}

func TestTool_UnknownNamesTheEnabled(t *testing.T) {
	set, _ := load(t, map[string][2]string{"a": {"description: A", "x"}, "b": {"description: B", "y"}}, nil, "b")
	_, err := run(t, set, `{"name":"zzz"}`)
	if err == nil || err.Error() != `unknown skill "zzz"; enabled skills: a` {
		t.Fatalf("err %v", err)
	}
	if _, err := run(t, set, `{}`); err == nil || !strings.Contains(err.Error(), `missing argument "name"`) {
		t.Fatalf("missing: %v", err)
	}
}

func TestTool_Flags(t *testing.T) {
	var tl Tool
	if !tl.ReadOnly() || tl.Confirm() || tl.Spec().Name != "skill" || tl.Summary(json.RawMessage(`{"name":"a","file":"f"}`)) != "a/f" {
		t.Fatal("skill must be read-only, free, named skill")
	}
}
