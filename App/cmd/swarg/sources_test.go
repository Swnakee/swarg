package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"swarg/internal/agent"
	"swarg/internal/config"
	"swarg/internal/llm/llmtest"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// sources (R-100..R-104)
// ---------------------------------------------------------------------------

// testSources returns a sources over cfg with an idle agent, fresh Recent and notices.
func testSources(t *testing.T, cfg *config.Config) (*sources, *agent.Agent) {
	t.Helper()
	dir := t.TempDir()
	recent, err := config.LoadRecent(dir)
	if err != nil {
		t.Fatal(err)
	}
	notices, err := config.LoadApprovals(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := newSources(cfg, filepath.Join(dir, "project"), recent, notices)
	s.a = agent.New(agent.Deps{Provider: s.client(cfg.Provider), SystemPrompt: func() string { return "" }, Model: cfg.Active().Model, ContextLimit: 100})
	return s, s.a
}

func orServer(t *testing.T) *llmtest.Server {
	srv := llmtest.New(t)
	srv.Models = []llmtest.Model{
		{ID: "a/alpha", Name: "Alpha", Context: 200000, Prompt: "0.000001", Completion: "0.000002", Parameters: []string{"tools", "reasoning"}},
		{ID: "b/beta:free", Name: "Beta", Context: 32768, Prompt: "0", Completion: "0", Parameters: []string{"tools"}},
		{ID: "c/chat", Name: "No tools", Context: 8192, Prompt: "0", Completion: "0", Parameters: []string{"temperature"}},
	}
	return srv
}

func TestSources_SwitchUsesRecentModelAndCatalogLimit(t *testing.T) {
	srv := orServer(t)
	cfg := config.Defaults()
	p := cfg.Providers["openrouter"]
	p.BaseURL, p.APIKeyEnv = srv.URL, "T11_TEST_KEY"
	cfg.Providers["openrouter"] = p
	t.Setenv("T11_TEST_KEY", "sk-test")
	s, a := testSources(t, &cfg)
	if err := s.recent.Use("openrouter", "a/alpha"); err != nil {
		t.Fatal(err)
	}
	notice, err := s.Switch("openrouter")
	if err != nil {
		t.Fatal(err)
	}
	if notice != "" { // the fake runs on 127.0.0.1: local, no notice
		t.Errorf("notice %q for a local address", notice)
	}
	src := a.Source()
	if src.Name != "openrouter" || a.Model() != "a/alpha" || s.Current() != "openrouter" {
		t.Fatalf("source %q model %q current %q", src.Name, a.Model(), s.Current())
	}
	if got := src.LimitFor("a/alpha"); got != 200000 {
		t.Errorf("LimitFor from the Catalog = %d, want 200000", got)
	}
	if got := src.LimitFor("unknown"); got != p.ContextLimit {
		t.Errorf("LimitFor of an unknown model = %d, want the entry's %d", got, p.ContextLimit)
	}
	if r, _ := src.ThinkExtra["reasoning"].(map[string]any); r["effort"] != "high" {
		t.Errorf("thinking body %v", src.ThinkExtra)
	}
}

func TestSources_SwitchRefusesWithoutKeyOrBadEntry(t *testing.T) {
	isolateConfigDir(t)
	cfg := config.Defaults()
	cfg.Providers["bad"] = config.Provider{Type: "openai", BaseURL: "nope", Model: "m", ContextLimit: 1, MaxTokens: 1}
	os.Unsetenv("OPENROUTER_API_KEY")
	s, a := testSources(t, &cfg)
	if _, err := s.Switch("openrouter"); err == nil || !strings.Contains(err.Error(), "no key: set OPENROUTER_API_KEY") {
		t.Errorf("no key: err = %v", err)
	}
	if _, err := s.Switch("bad"); err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Errorf("bad entry: err = %v", err)
	}
	if _, err := s.Switch("nothere"); err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Errorf("unknown: err = %v", err)
	}
	if a.Source().Name != "" || s.Current() != "local" {
		t.Errorf("a refused switch changed the source: %q / %q", a.Source().Name, s.Current())
	}
}

func TestSources_CloudNoticeOnce(t *testing.T) {
	cfg := config.Defaults()
	cfg.Providers["cloud"] = config.Provider{Type: "openai", BaseURL: "https://example.com/v1", Model: "m", ContextLimit: 1000, MaxTokens: 100}
	s, _ := testSources(t, &cfg)
	first, err := s.Switch("cloud")
	if err != nil || !strings.Contains(first, "example.com") || !strings.Contains(first, "third party") {
		t.Fatalf("first switch: %q, %v", first, err)
	}
	if again, _ := s.Switch("cloud"); again != "" {
		t.Errorf("second switch repeats the notice: %q", again)
	}
	s2 := newSources(&cfg, s.cwd, s.recent, s.notices)
	s2.a = s.a
	if later, _ := s2.Switch("cloud"); later != "" {
		t.Errorf("a new run of the same project repeats the notice: %q", later)
	}
	s3 := newSources(&cfg, s.cwd+"-other", s.recent, s.notices)
	s3.a = s.a
	if other, _ := s3.Switch("cloud"); other == "" {
		t.Error("another project must get its own notice")
	}
}

func TestSources_ServersStates(t *testing.T) {
	isolateConfigDir(t)
	srv := orServer(t)
	cfg := config.Defaults()
	cfg.Providers = map[string]config.Provider{
		"up":   {Type: "openai", BaseURL: srv.URL, Model: "m", ContextLimit: 1000, MaxTokens: 100},
		"down": {Type: "openai", BaseURL: "http://127.0.0.1:1/v1", Model: "m", ContextLimit: 1000, MaxTokens: 100},
		"key":  {Type: "openrouter", BaseURL: "https://example.com/v1", Model: "m", ContextLimit: 1000, MaxTokens: 100, APIKeyEnv: "T11_MISSING_KEY"},
		"bad":  {Type: "openai", BaseURL: "nope", Model: "m", ContextLimit: 1000, MaxTokens: 100},
	}
	cfg.Provider = "up"
	os.Unsetenv("T11_MISSING_KEY")
	s, _ := testSources(t, &cfg)
	rows, err := s.Servers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.Name] = r.State
		if r.Active != (r.Name == "up") {
			t.Errorf("%s active = %v", r.Name, r.Active)
		}
	}
	if rows[0].Name != "bad" || rows[1].Name != "down" || rows[2].Name != "key" || rows[3].Name != "up" {
		t.Errorf("rows not sorted by name: %+v", rows)
	}
	if got["up"] != "ok" || !strings.HasPrefix(got["down"], "down: ") || !strings.HasPrefix(got["key"], "no key: set T11_MISSING_KEY") ||
		!strings.HasPrefix(got["bad"], "bad config: ") || strings.Contains(got["bad"], `provider "bad"`) {
		t.Errorf("states %v", got)
	}
	if rows[3].Type != "openai" || rows[3].Address != srv.URL {
		t.Errorf("row %+v", rows[3])
	}
}

func TestSources_ListFiltersToolsRecentAndPrices(t *testing.T) {
	srv := orServer(t)
	cfg := config.Defaults()
	p := cfg.Providers["openrouter"]
	p.BaseURL, p.APIKeyEnv = srv.URL, ""
	cfg.Providers["openrouter"] = p
	cfg.Provider = "openrouter"
	s, _ := testSources(t, &cfg)
	if err := s.Used("b/beta:free"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "a/alpha" || rows[1].ID != "b/beta:free" {
		t.Fatalf("rows %+v, want the two tool-capable models", rows)
	}
	if rows[0].Price != "$1.00/$2.00 per M" || rows[0].Recent || rows[0].Name != "Alpha" {
		t.Errorf("paid row %+v", rows[0])
	}
	if rows[1].Price != "free" || !rows[1].Recent {
		t.Errorf("free recent row %+v", rows[1])
	}
	// a plain server (no supported_parameters) lists everything without prices
	plain := llmtest.New(t)
	plain.Models = []llmtest.Model{{ID: "x"}, {ID: "y"}}
	cfg.Providers["local"] = config.Provider{Type: "openai", BaseURL: plain.URL, Model: "x", ContextLimit: 1000, MaxTokens: 100}
	cfg.Provider = "local"
	s2, _ := testSources(t, &cfg)
	rows, err = s2.List(t.Context())
	if err != nil || len(rows) != 2 || rows[0].Price != "" || rows[0].Name != "" {
		t.Errorf("plain rows %+v, %v", rows, err)
	}
}

// Found by the live look: the EvModel of a switch arrives while the switch is still running, so
// the new Source's model was recorded under the OLD Source and a switch back started on it.
func TestSources_SwitchRecordsTheModelUnderTheNewSource(t *testing.T) {
	srv := llmtest.New(t)
	cfg := config.Defaults()
	cfg.Providers["second"] = config.Provider{Type: "openai", BaseURL: srv.URL, Model: "m2", ContextLimit: 1000, MaxTokens: 100}
	s, _ := testSources(t, &cfg)
	s.a = agent.New(agent.Deps{Provider: s.client("local"), SystemPrompt: func() string { return "" }, Model: "m1", ContextLimit: 100,
		OnEvent: func(ev agent.Event) {
			if ev.Kind == agent.EvModel {
				_ = s.Used(ev.Text)
			}
		}})
	if err := s.Used("m1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Switch("second"); err != nil {
		t.Fatal(err)
	}
	if r := s.recent.Models("local"); len(r) != 1 || r[0] != "m1" {
		t.Errorf("recent of local = %v, want [m1]", r)
	}
	if r := s.recent.Models("second"); len(r) != 1 || r[0] != "m2" {
		t.Errorf("recent of second = %v, want [m2]", r)
	}
	if _, err := s.Switch("local"); err != nil {
		t.Fatal(err)
	}
	if got := s.a.Model(); got != "m1" {
		t.Errorf("back on local the model is %q, want m1", got)
	}
}

// The down state names the network error, not the request URL.
func TestSources_DownStateIsShort(t *testing.T) {
	cfg := config.Defaults()
	cfg.Providers = map[string]config.Provider{"down": {Type: "openai", BaseURL: "http://127.0.0.1:1/v1", Model: "m", ContextLimit: 1000, MaxTokens: 100}}
	cfg.Provider = "down"
	s, _ := testSources(t, &cfg)
	rows, _ := s.Servers(t.Context())
	if st := rows[0].State; !strings.HasPrefix(st, "down: ") || strings.Contains(st, "/models") || strings.Contains(st, "Get ") {
		t.Errorf("state %q", st)
	}
}

// isolateConfigDir points config.Dir() (and so the key files) into a temp dir.
func isolateConfigDir(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("HOME", d)
	t.Setenv("XDG_CONFIG_HOME", d)
	t.Setenv("AppData", d)
}

// Bug (live, 2026-10-31): OpenRouter as the start provider (no Switch) showed the config's
// 32768 for every model; the Catalog was fetched only by Switch or /model's list.
func TestSources_LimitForFetchesTheCatalogOnce(t *testing.T) {
	isolateConfigDir(t)
	srv := orServer(t)
	cfg := config.Defaults()
	p := cfg.Providers["openrouter"]
	p.BaseURL, p.APIKeyEnv = srv.URL, "T11_TEST_KEY"
	cfg.Providers["openrouter"] = p
	cfg.Provider = "openrouter"
	t.Setenv("T11_TEST_KEY", "sk-test")
	s, _ := testSources(t, &cfg)
	lim := s.limitFor("openrouter")
	if got := lim("a/alpha"); got != 200000 {
		t.Errorf("LimitFor at start = %d, want the Catalog's 200000", got)
	}
	if got := lim("b/beta:free"); got != 32768 {
		t.Errorf("LimitFor of a second model = %d, want 32768", got)
	}
	if got := lim("unknown"); got != p.ContextLimit {
		t.Errorf("LimitFor of an unknown model = %d, want the entry's %d", got, p.ContextLimit)
	}
}

// ---------------------------------------------------------------------------
// KeyUsage (R-113)
// ---------------------------------------------------------------------------

func TestSources_KeyUsageAsksOpenRouterOnly(t *testing.T) {
	isolateConfigDir(t)
	srv := orServer(t)
	limit, left := 5.0, 4.0
	srv.Key = &llmtest.Key{Label: "k", Limit: &limit, Remaining: &left, Usage: 1, Monthly: 1}
	cfg := config.Defaults()
	p := cfg.Providers["openrouter"]
	p.BaseURL, p.APIKeyEnv = srv.URL, "T13_TEST_KEY"
	cfg.Providers["openrouter"] = p
	t.Setenv("T13_TEST_KEY", "sk-test")
	s, _ := testSources(t, &cfg)
	if _, err := s.KeyUsage(t.Context()); !errors.Is(err, ui.ErrNoKeyInfo) {
		t.Fatalf("local source: err = %v, want ErrNoKeyInfo", err)
	}
	s.setActive("openrouter")
	got, err := s.KeyUsage(t.Context())
	if err != nil || got.Label != "k" || got.Limit == nil || *got.Limit != 5 || *got.Remaining != 4 || got.Monthly != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	srv.Key = &llmtest.Key{Status: 401, Body: "no"}
	if _, err := s.KeyUsage(t.Context()); err == nil || strings.Contains(err.Error(), "sk-test") {
		t.Errorf("err = %v, want a refusal that does not name the key", err)
	}
}
