package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// OpenRouter preset and the per-entry check (T11, R-102)
// ---------------------------------------------------------------------------

func TestDefaults_OpenRouterPreset(t *testing.T) {
	p, ok := Defaults().Providers["openrouter"]
	if !ok {
		t.Fatal("no openrouter preset")
	}
	if p.Type != "openrouter" || p.BaseURL != "https://openrouter.ai/api/v1" || p.APIKeyEnv != "OPENROUTER_API_KEY" ||
		p.Model == "" || p.ContextLimit <= 0 || p.MaxTokens <= 0 {
		t.Fatalf("preset %+v", p)
	}
	if len(p.ExtraBody) != 0 {
		t.Errorf("extra_body must be empty (no local Qwen sampling), got %v", p.ExtraBody)
	}
	tb := p.ThinkingBody()
	r, _ := tb["reasoning"].(map[string]any)
	if r["effort"] != "high" || len(tb) != 1 {
		t.Errorf("thinking body = %v, want reasoning.effort high only", tb)
	}
}

func TestValidate_OpenRouterTypeAccepted(t *testing.T) {
	c := Defaults()
	c.Provider = "openrouter"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

// An unused broken entry does not stop the start; Provider.Validate names it for /server.
func TestValidate_UnusedBrokenProviderDoesNotStopTheStart(t *testing.T) {
	c := Defaults()
	c.Providers["bad"] = Provider{Type: "openai", BaseURL: "nope", Model: "m", ContextLimit: 1, MaxTokens: 1}
	if err := c.Validate(); err != nil {
		t.Fatalf("start refused by an unused entry: %v", err)
	}
	err := c.Providers["bad"].Validate("bad")
	if err == nil || !strings.Contains(err.Error(), `provider "bad"`) || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("Validate = %v, want the entry named with the base_url rule", err)
	}
}

func TestConfig_Ready(t *testing.T) {
	isolateUserKeys(t)
	c := Defaults()
	c.Providers["bad"] = Provider{Type: "openai", BaseURL: "nope", Model: "m", ContextLimit: 1, MaxTokens: 1}
	os.Unsetenv("OPENROUTER_API_KEY")
	for _, tc := range []struct{ name, want string }{
		{"local", ""}, {"nothere", "not defined"}, {"bad", "base_url"}, {"openrouter", "no key: set OPENROUTER_API_KEY"},
	} {
		_, err := c.Ready(tc.name)
		if (err == nil) != (tc.want == "") || (err != nil && !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("Ready(%s) = %v, want %q", tc.name, err, tc.want)
		}
	}
	t.Setenv("OPENROUTER_API_KEY", "sk-test")
	if _, err := c.Ready("openrouter"); err != nil {
		t.Errorf("with the key: %v", err)
	}
}

func TestProvider_Local(t *testing.T) {
	for _, tc := range []struct {
		url   string
		local bool
	}{
		{"http://127.0.0.1:8080/v1", true}, {"http://localhost:8080/v1", true}, {"http://172.16.0.5:11434/v1", true},
		{"http://10.1.2.3:1/v1", true}, {"http://[fe80::1]:8080/v1", true}, {"http://model:8080/v1", true},
		{"https://openrouter.ai/api/v1", false}, {"http://203.0.113.5:8080/v1", false},
	} {
		if got := (Provider{BaseURL: tc.url}).Local(); got != tc.local {
			t.Errorf("Local(%s) = %v, want %v", tc.url, got, tc.local)
		}
	}
}

// ---------------------------------------------------------------------------
// Recent models per provider (R-101)
// ---------------------------------------------------------------------------

func TestRecent_UseMovesToFrontCapsAndSaves(t *testing.T) {
	dir := t.TempDir()
	r, err := LoadRecent(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Models("openrouter"); len(got) != 0 {
		t.Fatalf("empty store lists %v", got)
	}
	for i := 0; i < recentCap+2; i++ {
		if err := r.Use("openrouter", "m"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Use("openrouter", "mc"); err != nil { // used again: to the front, no duplicate
		t.Fatal(err)
	}
	if err := r.Use("local", "x"); err != nil {
		t.Fatal(err)
	}
	again, err := LoadRecent(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Models("openrouter")
	if len(got) != recentCap || got[0] != "mc" || got[1] != "ml" || strings.Count(strings.Join(got, " "), "mc") != 1 {
		t.Fatalf("recent = %v", got)
	}
	if l := again.Models("local"); len(l) != 1 || l[0] != "x" {
		t.Fatalf("local recent = %v", l)
	}
	if again.First("local", "cfg") != "x" || again.First("none", "cfg") != "cfg" {
		t.Errorf("First = %q / %q", again.First("local", "cfg"), again.First("none", "cfg"))
	}
	if _, err := os.Stat(filepath.Join(dir, recentFile)); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// One-time notices next to the approvals (R-104)
// ---------------------------------------------------------------------------

func TestApprovals_NoticeOncePerProject(t *testing.T) {
	dir := t.TempDir()
	a, err := LoadApprovals(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.Noticed("/p", "cloud") {
		t.Fatal("noticed before any notice")
	}
	if err := a.Notice("/p", "cloud"); err != nil {
		t.Fatal(err)
	}
	again, _ := LoadApprovals(dir)
	if !again.Noticed("/p", "cloud") || again.Noticed("/q", "cloud") {
		t.Fatal("notice not kept per project")
	}
	if again.Approved("/p", "cloud", MCPServer{}) {
		t.Fatal("a notice must not count as an MCP approval")
	}
}
