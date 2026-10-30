package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// project writes the project (and optional local) config under a temp dir and returns the dir.
func project(t *testing.T, body, local string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".swarg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if body != "" {
		os.WriteFile(filepath.Join(dir, ".swarg", "config.json"), []byte(body), 0o644)
	}
	if local != "" {
		os.WriteFile(filepath.Join(dir, ".swarg", "config.local.json"), []byte(local), 0o644)
	}
	return dir
}

// ---------------------------------------------------------------------------
// LoadLayered (R-95, R-96)
// ---------------------------------------------------------------------------

func TestLoadLayered_ProjectWinsPerKeyAndMapsMerge(t *testing.T) {
	global := writeConfig(t, `{"tools":{"max_steps":30,"disabled":["shell"]},
		"providers":{"ollama":{"type":"ollama","base_url":"http://127.0.0.1:11434/v1","model":"g","context_limit":1,"max_tokens":1}},
		"mcp":{"servers":{"g1":{"command":"a"},"both":{"command":"global"}}}}`)
	dir := project(t, `{"tools":{"max_steps":7,"disabled":["grep","read_file"]},
		"providers":{"ollama":{"model":"p"}},
		"mcp":{"servers":{"p1":{"url":"https://h/mcp"},"both":{"disabled":true}}}}`, "")
	c, l, err := LoadLayered(global, false, dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Tools.MaxSteps != 7 || strings.Join(c.Tools.Disabled, ",") != "grep,read_file" {
		t.Fatalf("tools %+v", c.Tools)
	}
	// a project entry changes only the keys it names: the provider keeps its url, the server its command
	if c.Tools.ShellTimeoutSec != 120 || c.Providers["ollama"].Model != "p" || c.Providers["ollama"].BaseURL != "http://127.0.0.1:11434/v1" || c.Providers["local"].Model == "" {
		t.Fatalf("untouched keys changed: %+v", c.Providers)
	}
	if b := c.MCP.Servers["both"]; len(c.MCP.Servers) != 3 || b.Command != "global" || !b.Disabled || c.MCP.Servers["g1"].Command != "a" {
		t.Fatalf("servers %+v", c.MCP.Servers)
	}
	if !l.Project || !l.ProjectServers["p1"] || !l.ProjectServers["both"] || l.ProjectServers["g1"] || len(l.Warnings) != 0 {
		t.Fatalf("layers %+v", l)
	}
	if !l.FromProject("tools.max_steps") || !l.FromProject("mcp.servers.both.disabled") || l.FromProject("tools.shell_timeout_sec") {
		t.Fatalf("FromProject %+v", l.ProjectRaw)
	}
}

func TestLoadLayered_LocalOverProject(t *testing.T) {
	dir := project(t, `{"tools":{"max_steps":7}}`, `{"tools":{"max_steps":9},"mcp":{"servers":{"mine":{"command":"x"}}}}`)
	c, l, err := LoadLayered(filepath.Join(t.TempDir(), "none.json"), false, dir)
	if err != nil || c.Tools.MaxSteps != 9 || !l.ProjectServers["mine"] {
		t.Fatalf("%+v %+v %v", c.Tools, l, err)
	}
}

func TestLoadLayered_NoProjectFilesIsTheOldLoad(t *testing.T) {
	global := writeConfig(t, `{"tools":{"max_steps":30}}`)
	want, _ := Load(global, false)
	for _, dir := range []string{t.TempDir(), project(t, "", "")} {
		c, l, err := LoadLayered(global, false, dir)
		if err != nil || c.Tools.MaxSteps != want.Tools.MaxSteps || l.Project || len(l.ProjectServers) != 0 {
			t.Fatalf("dir %s: %+v %+v %v", dir, c.Tools, l, err)
		}
	}
	if _, _, err := LoadLayered(filepath.Join(t.TempDir(), "x.json"), true, t.TempDir()); err == nil {
		t.Fatal("an explicit missing global file must fail")
	}
}

func TestLoadLayered_UnknownFieldInProjectFileIsAnError(t *testing.T) {
	dir := project(t, `{"tools":{"max_stepz":7}}`, "")
	_, _, err := LoadLayered(filepath.Join(t.TempDir(), "none.json"), false, dir)
	if err == nil || !strings.Contains(err.Error(), "max_stepz") || !strings.Contains(err.Error(), ".swarg") {
		t.Fatalf("err %v", err)
	}
}

func TestLoadLayered_GuardResetsUnsafeProjectValues(t *testing.T) {
	global := writeConfig(t, `{"mcp":{"servers":{"g":{"command":"a","trust":true}}}}`)
	dir := project(t, `{"tools":{"auto_approve":true,"max_steps":5},"web":{"confirm":false},"mcp":{"confirm":false,
		"servers":{"p":{"command":"b","trust":true},"g":{"command":"a","trust":true}}}}`, "")
	c, l, err := LoadLayered(global, false, dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Tools.AutoApprove || !c.Web.Confirm || !c.MCP.Confirm || c.MCP.Servers["p"].Trust || c.Tools.MaxSteps != 5 {
		t.Fatalf("guard: %+v %+v %+v", c.Tools, c.Web, c.MCP)
	}
	if !c.MCP.Servers["g"].Trust { // the global value of the same server is kept
		t.Fatal("global trust lost")
	}
	if len(l.Warnings) != 4 || !strings.Contains(l.Warnings[0], "tools.auto_approve") || !strings.Contains(l.Warnings[3], "mcp.servers.p.trust") {
		t.Fatalf("warnings %q", l.Warnings)
	}
	// a safe direction is fine: the project may switch auto-approve off
	global = writeConfig(t, `{"tools":{"auto_approve":true}}`)
	dir = project(t, `{"tools":{"auto_approve":false}}`, "")
	if c, l, _ := LoadLayered(global, false, dir); c.Tools.AutoApprove || len(l.Warnings) != 0 {
		t.Fatalf("safe direction refused: %+v %q", c.Tools, l.Warnings)
	}
}

// ---------------------------------------------------------------------------
// Set (R-98)
// ---------------------------------------------------------------------------

func TestSet_WritesOneKeyAndKeepsTheRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json") // the folder is created
	if err := Set(path, "tools.max_steps", 12, false); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "skills.disabled", []string{"a", "b"}, false); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "mcp.servers.demo.disabled", true, false); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "tools.max_steps", 15, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "\n  \"tools\": {\n    \"max_steps\": 15\n  }") || !strings.Contains(s, `"disabled": [`) || !strings.Contains(s, `"demo": {`) {
		t.Fatalf("file:\n%s", s)
	}
	c, err := Load(path, true) // what was written loads (max_steps counted; demo needs a command to validate: not here)
	if err == nil || !strings.Contains(err.Error(), "exactly one of command and url") {
		t.Fatalf("a server with only disabled is incomplete, got %+v %v", c.MCP.Servers, err)
	}
	if err := Set(path, "mcp.servers.demo.command", "python3", false); err != nil {
		t.Fatal(err)
	}
	if c, err = Load(path, true); err != nil || c.Tools.MaxSteps != 15 || !c.MCP.Servers["demo"].Disabled || len(c.Skills.Disabled) != 2 {
		t.Fatalf("%+v %v", c, err)
	}
	os.WriteFile(path, []byte("{bad"), 0o644)
	if err := Set(path, "tools.max_steps", 1, false); err == nil {
		t.Fatal("a broken file must not be overwritten")
	}
}

func TestSet_RefusesUnsafeValueInProjectFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for key, v := range map[string]any{"tools.auto_approve": true, "web.confirm": false, "mcp.confirm": false, "mcp.servers.x.trust": true} {
		if err := Set(path, key, v, true); err == nil || !strings.Contains(err.Error(), "not allowed in a project file") {
			t.Fatalf("%s: %v", key, err)
		}
		if err := Set(path, key, v, false); err != nil { // the global file takes it
			t.Fatal(err)
		}
	}
	if err := Set(path, "tools.auto_approve", false, true); err != nil { // the safe direction is fine
		t.Fatal(err)
	}
}
