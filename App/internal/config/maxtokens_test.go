package config

import "testing"

// Owner (2026-10-31): output length must not cut answers. Local servers get a roomy explicit limit
// (mlx_lm.server defaults to 512 when max_tokens is absent); OpenRouter sends none, so each model
// answers up to its own maximum.
func TestDefaults_OutputLimits(t *testing.T) {
	d := Defaults()
	for name, want := range map[string]int{"local": 8192, "ollama": 8192, "openrouter": 0} {
		if got := d.Providers[name].MaxTokens; got != want {
			t.Errorf("%s max_tokens = %d, want %d", name, got, want)
		}
	}
	if got := d.Providers["openrouter"].ThinkingLimit(); got != 0 {
		t.Errorf("openrouter thinking limit = %d, want 0 (the model's own maximum)", got)
	}
	if got := d.Providers["local"].ThinkingLimit(); got != 16384 {
		t.Errorf("local thinking limit = %d, want 16384", got)
	}
}

func TestValidate_ZeroMaxTokensMeansModelMaximum(t *testing.T) {
	c := Defaults()
	p := c.Providers["local"]
	p.MaxTokens = 0
	c.Providers["local"] = p
	if err := c.Validate(); err != nil {
		t.Errorf("max_tokens 0 refused: %v", err)
	}
	p.MaxTokens = -1
	c.Providers["local"] = p
	if err := c.Validate(); err == nil {
		t.Error("negative max_tokens accepted")
	}
}
