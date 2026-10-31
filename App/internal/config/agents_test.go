package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Sub-agent settings (T13 C, R-122)
// ---------------------------------------------------------------------------

func TestProvider_ParallelDefaultsByType(t *testing.T) {
	for _, tc := range []struct {
		p    Provider
		want int
	}{
		{Provider{Type: "openai"}, 1},
		{Provider{Type: "ollama"}, 1},
		{Provider{Type: "openrouter"}, 4},
		{Provider{Type: "openai", MaxParallel: 3}, 3},
		{Provider{Type: "openrouter", MaxParallel: 2}, 2},
	} {
		if got := tc.p.Parallel(); got != tc.want {
			t.Errorf("%+v: Parallel = %d, want %d", tc.p, got, tc.want)
		}
	}
}

func TestValidate_MaxParallelRanges(t *testing.T) {
	c := Defaults()
	if c.Agents.MaxParallel != 4 {
		t.Fatalf("default agents.max_parallel = %d, want 4", c.Agents.MaxParallel)
	}
	for _, n := range []int{0, 5, -1} {
		c := Defaults()
		c.Agents.MaxParallel = n
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "agents.max_parallel") {
			t.Errorf("agents.max_parallel %d: err = %v", n, err)
		}
	}
	for _, n := range []int{-1, 5} {
		p := Defaults().Providers["local"]
		p.MaxParallel = n
		if err := p.Validate("local"); err == nil || !strings.Contains(err.Error(), "max_parallel") {
			t.Errorf("providers.local.max_parallel %d: err = %v", n, err)
		}
	}
}

func TestLoad_AgentsKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := `{"agents":{"disabled":["review"],"max_parallel":2},"providers":{"local":{"type":"openai","base_url":"http://127.0.0.1:8080/v1","model":"m","context_limit":8192,"max_tokens":512,"max_parallel":2}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Agents.Disabled) != 1 || c.Agents.Disabled[0] != "review" || c.Agents.MaxParallel != 2 || c.Providers["local"].Parallel() != 2 {
		t.Fatalf("loaded %+v / %+v", c.Agents, c.Providers["local"])
	}
}
