package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"swarg/internal/keys"
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
		p.Model == "" || p.ContextLimit <= 0 || p.MaxTokens < 0 {
		t.Fatalf("preset %+v", p)
	}
	for k := range p.ExtraBody { // no local Qwen sampling; only the provider routing
		if k != "provider" {
			t.Errorf("extra_body has %q (no local Qwen sampling), got %v", k, p.ExtraBody)
		}
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

// ---------------------------------------------------------------------------
// API key lookup through the user keys file (R-105)
// ---------------------------------------------------------------------------

// isolateUserKeys points keys.User (and Dir) at a temp dir through the OS variables.
func isolateUserKeys(t *testing.T) keys.Store {
	t.Helper()
	d := t.TempDir()
	t.Setenv("HOME", d)
	t.Setenv("XDG_CONFIG_HOME", d)
	t.Setenv("AppData", d)
	ks := keys.User()
	if !strings.HasPrefix(ks.File, d) {
		t.Fatalf("keys file %q is outside the temp dir", ks.File)
	}
	return ks
}

// writeFile writes body with exactly mode (the umask may have cut the bits).
func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// R-105
func TestAPIKey_FromEnvThenKeysFile(t *testing.T) {
	ks := isolateUserKeys(t)
	t.Setenv("T12_KEY_I", "")
	p := Provider{APIKeyEnv: "T12_KEY_I"}
	if p.APIKey() != "" {
		t.Fatal("no key yet")
	}
	writeFile(t, ks.File, "T12_KEY_I=test-key-value-1\n", 0o600)
	if got := p.APIKey(); got != "test-key-value-1" { // read on every call: no restart
		t.Errorf("APIKey = %q", got)
	}
	writeFile(t, ks.File, "T12_KEY_I=test-key-value-2\n", 0o600)
	if got := p.APIKey(); got != "test-key-value-2" {
		t.Errorf("after an edit APIKey = %q", got)
	}
	t.Setenv("T12_KEY_I", "test-env-value")
	if got := p.APIKey(); got != "test-env-value" {
		t.Errorf("env must win, got %q", got)
	}
}

func TestAPIKey_StillReadsTheOldKeysDir(t *testing.T) {
	ks := isolateUserKeys(t)
	t.Setenv("T12_KEY_J", "")
	writeFile(t, filepath.Join(ks.OldDir, "T12_KEY_J"), "test-old-value\n", 0o600)
	if got := (Provider{APIKeyEnv: "T12_KEY_J"}).APIKey(); got != "test-old-value" {
		t.Errorf("APIKey = %q", got)
	}
}

func TestNoKey_NamesTheVariableAndTheKeysFile(t *testing.T) {
	ks := isolateUserKeys(t)
	h := (Provider{APIKeyEnv: "T12_KEY_K"}).NoKey()
	if !strings.Contains(h, "set T12_KEY_K") || !strings.Contains(h, ks.File) || !strings.Contains(h, "chmod 600") {
		t.Errorf("NoKey = %q", h)
	}
	if h := (Provider{APIKeyEnv: "../x"}).NoKey(); strings.Contains(h, "keys.env") {
		t.Errorf("a bad name gets no file hint: %q", h)
	}
}
