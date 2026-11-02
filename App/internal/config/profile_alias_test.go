package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Old profile names in a config file (R-134)
// ---------------------------------------------------------------------------

func TestLoad_OldProfileNamesLoadAsTheNewOnes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"prompt":{"warn_share":0.25,"profile":"standard","profiles":{"*9b*":"full","*2b*":"compact"}},"providers":{"local":{"type":"openai","base_url":"http://127.0.0.1:8080/v1","model":"m","context_limit":1000,"max_tokens":10,"profile":"full"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Prompt.Profile != "medium" || c.Providers["local"].Profile != "high" {
		t.Errorf("stored names = %q, %q, want medium and high", c.Prompt.Profile, c.Providers["local"].Profile)
	}
	for model, want := range map[string]string{"qwen-9b": "high", "qwen-2b": "low", "other": "high"} {
		if got := c.ProfileFor("local", model); got != want {
			t.Errorf("ProfileFor(%q) = %q, want %q", model, got, want)
		}
	}
	if got := c.ProfileFor("nope", "other"); got != "medium" {
		t.Errorf("global old name gives %q, want medium", got)
	}
}

func TestLoadLayered_OldProfileNameInProjectFile(t *testing.T) {
	global := filepath.Join(t.TempDir(), "config.json")
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, ".swarg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ProjectConfig), []byte(`{"prompt":{"profile":"standard"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadLayered(global, false, proj)
	if err != nil {
		t.Fatal(err)
	}
	if c.Prompt.Profile != "medium" {
		t.Errorf("prompt.profile = %q, want medium", c.Prompt.Profile)
	}
}

func TestCanonicalProfile_MapsOldNamesOnly(t *testing.T) {
	for in, want := range map[string]string{"compact": "low", "standard": "medium", "full": "high", "low": "low", "": "", "huge": "huge"} {
		if got := CanonicalProfile(in); got != want {
			t.Errorf("CanonicalProfile(%q) = %q, want %q", in, got, want)
		}
	}
}
