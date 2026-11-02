package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"swarg/internal/agent"
	"swarg/internal/chat"
	"swarg/internal/config"
	"swarg/internal/mcp"
	"swarg/internal/mcp/mcptest"
	"swarg/internal/skills"
)

// skillDir writes n Skills with descriptions of the given length into a global dir.
func skillDir(t *testing.T, n, descLen int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		d := filepath.Join(dir, "s"+strings.Repeat("k", i+1))
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\ndescription: "+strings.Repeat("w ", descLen/2)+"\n---\nbody"), 0o644)
	}
	return dir
}

// R-89
func TestExtraPromptWarning_UnderAndOver(t *testing.T) {
	sk, _ := skills.Load(skillDir(t, 2, 40), t.TempDir(), nil)
	if w := extraPromptWarning(sk, nil); w != "" {
		t.Fatalf("two small skills warned: %q", w)
	}
	var big []mcptest.Tool
	for i := 0; i < 30; i++ {
		big = append(big, mcptest.Tool{Name: "tool" + strings.Repeat("x", i), Description: strings.Repeat("long description ", 12),
			Schema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string","description":"` + strings.Repeat("p", 80) + `"}}}`)})
	}
	fake := mcptest.New(big...)
	mc := mcp.New(map[string]config.MCPServer{"demo": {Command: "x"}}, mcp.Deps{Dial: fake.Dial})
	mc.Start(context.Background())
	w := extraPromptWarning(sk, mc)
	if !strings.HasPrefix(w, "prompt: skills and MCP add ") || !strings.Contains(w, "(limit 1500): mcp demo ") || !strings.Contains(w, "skills.disabled") {
		t.Fatalf("warning %q", w)
	}
	if extraPromptWarning(nil, nil) != "" {
		t.Fatal("nothing loaded must not warn")
	}
}

func TestToolRunner_RoutesMCPPrefix(t *testing.T) {
	fake := mcptest.New(mcptest.Tool{Name: "lookup", Description: "d", ReadOnly: true,
		Handler: func(json.RawMessage) (string, bool) { return "found", false }})
	mc := mcp.New(map[string]config.MCPServer{"demo": {Command: "x", Trust: true}}, mcp.Deps{Dial: fake.Dial, Confirm: true})
	mc.Start(context.Background())
	r := newTestRunner(t)
	r.mcp = mc
	names := map[string]bool{}
	for _, s := range r.Specs() {
		names[s.Name] = true
	}
	if !names["read_file"] || !names["mcp__demo__lookup"] {
		t.Fatalf("specs %v", names)
	}
	if known, ro, confirm := r.Kind("mcp__demo__lookup"); !known || !ro || confirm {
		t.Fatalf("kind %v %v %v", known, ro, confirm)
	}
	call := chat.ToolCall{ID: "c1", Name: "mcp__demo__lookup", Args: `{"q":"A"}`}
	if out, _, err := r.Run(context.Background(), call, agent.ToolHooks{}); err != nil || out != "found" {
		t.Fatalf("run %q %v", out, err)
	}
	if r.Label(call) != "A" || r.Stub(call, "found", "") != "[mcp__demo__lookup: result shown earlier]" || r.Touched(call) != "" || r.Detachable(call.Name) {
		t.Fatalf("label %q stub %q", r.Label(call), r.Stub(call, "found", ""))
	}
	if known, _, _ := r.Kind("shell"); !known { // built-ins still answer
		t.Fatal("shell unknown")
	}
}
