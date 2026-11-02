package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Prompt profile selection (R-116)
// ---------------------------------------------------------------------------

func TestProfileFor_Precedence(t *testing.T) {
	c := Defaults()
	c.Prompt.Profile = "medium"
	c.Prompt.Profiles = map[string]string{"*qwen*": "low", "*Qwen3.5-9B*": "high"}
	p := c.Providers["openrouter"]
	p.Profile = "high"
	c.Providers["openrouter"] = p

	tests := []struct {
		name, provider, model, want string
	}{
		{"glob beats provider and global", "openrouter", "mlx-community/Qwen3.5-4B-4bit", "low"},
		{"the longest matching pattern wins", "local", "mlx-community/Qwen3.5-9B-4bit", "high"},
		{"provider beats global", "openrouter", "nvidia/nemotron", "high"},
		{"global when no pattern or provider profile", "local", "other-model", "medium"},
		{"pattern case is ignored", "local", "QWEN-x", "low"},
		{"unknown provider falls to global", "nope", "other-model", "medium"},
	}
	for _, tt := range tests {
		if got := c.ProfileFor(tt.provider, tt.model); got != tt.want {
			t.Errorf("%s: ProfileFor(%q, %q) = %q, want %q", tt.name, tt.provider, tt.model, got, tt.want)
		}
	}
}

func TestProfileFor_NothingSetIsCompact(t *testing.T) {
	if got := Defaults().ProfileFor("local", "any"); got != "low" {
		t.Errorf("default profile = %q, want low", got)
	}
}

func TestGlobMatch(t *testing.T) {
	tests := []struct {
		pattern, s string
		want       bool
	}{
		{"*", "", true}, {"a*c", "abbc", true}, {"a*c", "ab", false}, {"a?c", "abc", true},
		{"a?c", "ac", false}, {"*/x*", "org/xy", true}, {"**a", "ba", true}, {"abc", "abc", true}, {"abc", "abcd", false},
	}
	for _, tt := range tests {
		if got := globMatch(tt.pattern, tt.s); got != tt.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", tt.pattern, tt.s, got, tt.want)
		}
	}
}

func TestValidate_ProfileNames(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"prompt.profile", func(c *Config) { c.Prompt.Profile = "huge" }, "prompt.profile"},
		{"prompt.profiles value", func(c *Config) { c.Prompt.Profiles = map[string]string{"x*": "tiny"} }, "prompt.profiles.x*"},
		{"empty pattern", func(c *Config) { c.Prompt.Profiles = map[string]string{" ": "high"} }, "empty model pattern"},
		{"provider profile", func(c *Config) { p := c.Providers["local"]; p.Profile = "big"; c.Providers["local"] = p }, "providers.local.profile"},
	}
	for _, tt := range tests {
		c := Defaults()
		tt.edit(&c)
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), tt.want) || (tt.name != "empty pattern" && !strings.Contains(err.Error(), "low, medium, high")) {
			t.Errorf("%s: error = %v, want it to name %q and list the profiles", tt.name, err, tt.want)
		}
	}
}

func TestLoad_ProfileKeysFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"prompt":{"warn_share":0.25,"profile":"medium","profiles":{"*9b*":"high"}},"providers":{"local":{"type":"openai","base_url":"http://127.0.0.1:8080/v1","model":"m","context_limit":1000,"max_tokens":10,"profile":"high"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ProfileFor("local", "qwen-9B"); got != "high" {
		t.Errorf("ProfileFor = %q, want high", got)
	}
	if got := c.ProfileFor("local", "m"); got != "high" {
		t.Errorf("provider profile ignored: %q", got)
	}
}

func TestLoad_OldConfigHasNoProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"prompt":{"warn_share":0.3}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ProfileFor("local", "m"); got != "low" {
		t.Errorf("an old config gives %q, want low", got)
	}
}

func TestDefaults_OpenRouterPresetIsMediumLocalsStayLow(t *testing.T) {
	c := Defaults()
	if got := c.ProfileFor("openrouter", "nvidia/nemotron-3.5-lightning:free"); got != "medium" {
		t.Errorf("openrouter default profile = %q, want medium", got)
	}
	for name, p := range c.Providers {
		if p.Type != "openrouter" && c.ProfileFor(name, "any") != "low" {
			t.Errorf("local source %q default profile = %q, want low", name, c.ProfileFor(name, "any"))
		}
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
