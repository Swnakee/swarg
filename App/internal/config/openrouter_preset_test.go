package config

import (
	"os"
	"reflect"
	"testing"
)

// Owner (2026-11-01): OpenRouter routes away from providers that drop gpt-oss tool calls
// (Darkbloom, DeepInfra, DekaLLM) unless the user's config says otherwise.
func TestDefaults_OpenRouterIgnoresBrokenProviders(t *testing.T) {
	p, ok := Defaults().Providers["openrouter"].ExtraBody["provider"].(map[string]any)
	if !ok {
		t.Fatalf("openrouter extra_body has no provider object: %v", Defaults().Providers["openrouter"].ExtraBody)
	}
	want := []string{"Darkbloom", "DeepInfra", "DekaLLM"}
	if got, _ := p["ignore"].([]string); !reflect.DeepEqual(got, want) {
		t.Errorf("provider.ignore = %v, want %v", p["ignore"], want)
	}
}

// The thinking overlay of openrouter keeps the provider routing (it is laid over extra_body).
func TestThinkingBody_OpenRouterKeepsProviderRouting(t *testing.T) {
	p := Defaults().Providers["openrouter"]
	if _, clash := p.ThinkingBody()["provider"]; clash {
		t.Errorf("thinking body overrides provider: %v", p.ThinkingBody())
	}
}

// A config file that only chooses openrouter keeps the routing of the preset.
func TestLoad_OpenRouterChosenKeepsProviderRouting(t *testing.T) {
	path := t.TempDir() + "/config.json"
	if err := os.WriteFile(path, []byte(`{"provider":"openrouter"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := c.Providers["openrouter"].ExtraBody["provider"].(map[string]any)
	if p == nil || p["ignore"] == nil {
		t.Errorf("extra_body = %v, want the provider.ignore of the preset", c.Providers["openrouter"].ExtraBody)
	}
}
