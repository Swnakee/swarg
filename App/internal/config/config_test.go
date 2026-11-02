package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeConfig writes body to a temp config file and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

func TestDefaults_MatchSpec(t *testing.T) {
	c := Defaults()
	p := c.Active()
	if c.Provider != "local" {
		t.Fatalf("provider = %q, want local", c.Provider)
	}
	if p.Type != "openai" || p.BaseURL != "http://127.0.0.1:8080/v1" ||
		p.Model != "mlx-community/Qwen3.5-4B-4bit" || p.ContextLimit != 16384 || p.MaxTokens != 8192 {
		t.Fatalf("unexpected provider defaults: %+v", p)
	}
	ctargs, ok := p.ExtraBody["chat_template_kwargs"].(map[string]any)
	if !ok || ctargs["enable_thinking"] != false {
		t.Fatalf("extra_body = %v, want chat_template_kwargs.enable_thinking=false", p.ExtraBody)
	}
	if c.Prompt.WarnShare != 0.25 {
		t.Fatalf("warn_share = %v, want 0.25", c.Prompt.WarnShare)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
}

func TestDefaults_ExtraBodySampling(t *testing.T) {
	eb := Defaults().Active().ExtraBody
	want := map[string]any{"temperature": 0.7, "top_p": 0.8, "top_k": 20, "min_p": 0.0, "presence_penalty": 1.5, "repetition_penalty": 1.0}
	for k, v := range want {
		if eb[k] != v {
			t.Errorf("extra_body[%s] = %v, want %v", k, eb[k], v)
		}
	}
	if _, ok := eb["repetition_context_size"]; ok {
		t.Errorf("extra_body = %v, want no repetition_context_size", eb)
	}
}

func TestLoad_OldProviderKeepsItsOwnValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	old := `{"provider":"local","providers":{"local":{"type":"openai","base_url":"http://127.0.0.1:8080/v1","model":"m","context_limit":8000,"max_tokens":8192,"extra_body":{"repetition_penalty":1.1,"repetition_context_size":256}}}}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	p := c.Active()
	if p.MaxTokens != 8192 || p.ExtraBody["repetition_penalty"] != 1.1 || p.ExtraBody["repetition_context_size"] != 256.0 {
		t.Errorf("provider = %+v, want the file's own values", p)
	}
	if _, ok := p.ExtraBody["temperature"]; ok {
		t.Errorf("extra_body = %v, want no default sampling keys merged in", p.ExtraBody)
	}
}

func TestDefaults_ToolsSection(t *testing.T) {
	tl := Defaults().Tools
	if tl.Disabled == nil || len(tl.Disabled) != 0 || tl.MaxSteps != 50 || tl.ShellTimeoutSec != 120 || tl.AutoApprove {
		t.Fatalf("tools defaults = %+v, want disabled [], max_steps 50, shell_timeout_sec 120, auto_approve false", tl)
	}
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

func TestLoad_MissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.json")

	got, err := Load(missing, false)
	if err != nil {
		t.Fatalf("not explicit: unexpected error: %v", err)
	}
	if got.Active().Model != Defaults().Active().Model {
		t.Fatalf("not explicit: got %+v, want defaults", got)
	}

	_, err = Load(missing, true)
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("explicit: error = %v, want one naming %s", err, missing)
	}
}

func TestLoad_PartialOverride(t *testing.T) {
	path := writeConfig(t, `{
		"providers": {
			"local": {"type": "openai", "base_url": "http://10.0.0.2:9000/v1", "model": "m2",
				"context_limit": 4096, "max_tokens": 512}
		},
		"prompt": {}
	}`)

	got, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	p := got.Active()
	if p.BaseURL != "http://10.0.0.2:9000/v1" || p.Model != "m2" || p.ContextLimit != 4096 {
		t.Fatalf("file values not applied: %+v", p)
	}
	if p.ExtraBody != nil {
		t.Fatalf("provider entry must replace the default entry whole; extra_body = %v", p.ExtraBody)
	}
	if got.Prompt.WarnShare != 0.25 {
		t.Fatalf("unspecified warn_share = %v, want default 0.25", got.Prompt.WarnShare)
	}
	if got.Provider != "local" {
		t.Fatalf("unspecified provider = %q, want local", got.Provider)
	}
}

func TestLoad_RejectsUnknownField(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"top level", `{"provider": "local", "bogus": 1}`},
		{"nested", `{"providers": {"local": {"type": "openai", "base_url": "http://x:1/v1",
			"model": "m", "context_limit": 1, "max_tokens": 1, "bogus": true}}}`},
		{"tools", `{"tools": {"max_steps": 5, "bogus": true}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, tt.body), true); err == nil {
				t.Fatal("expected error for unknown field")
			}
		})
	}
}

func TestLoad_OldFileKeepsToolsDefaults(t *testing.T) {
	old, err := Load(writeConfig(t, `{"provider": "local", "prompt": {"warn_share": 0.5}}`), true)
	if err != nil {
		t.Fatalf("old file without tools: %v", err)
	}
	if old.Tools.MaxSteps != 50 || old.Tools.ShellTimeoutSec != 120 || old.Tools.AutoApprove || len(old.Tools.Disabled) != 0 {
		t.Fatalf("tools = %+v, want defaults", old.Tools)
	}

	partial, err := Load(writeConfig(t, `{"tools": {"max_steps": 7}}`), true)
	if err != nil {
		t.Fatalf("partial tools: %v", err)
	}
	if partial.Tools.MaxSteps != 7 || partial.Tools.ShellTimeoutSec != 120 {
		t.Fatalf("tools = %+v, want max_steps 7 and the other defaults", partial.Tools)
	}
}

func TestLoad_RejectsInvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"truncated", `{"provider": "local"`},
		{"trailing garbage", `{"provider": "local"} xyz`},
		{"second value", `{"provider": "local"} {}`},
		{"empty file", ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, tt.body), true); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"defaults are valid", func(c *Config) {}, false},
		{"missing active provider", func(c *Config) { c.Provider = "nope" }, true},
		{"type not openai", func(c *Config) { p := c.Providers["local"]; p.Type = "anthropic"; c.Providers["local"] = p }, true},
		{"base_url ftp", func(c *Config) { setBaseURL(c, "ftp://x") }, true},
		{"base_url garbage", func(c *Config) { setBaseURL(c, "not a url") }, true},
		{"base_url empty", func(c *Config) { setBaseURL(c, "") }, true},
		{"empty model", func(c *Config) { p := c.Providers["local"]; p.Model = ""; c.Providers["local"] = p }, true},
		{"context_limit zero", func(c *Config) { setInt(c, func(p *Provider) *int { return &p.ContextLimit }, 0) }, true},
		{"context_limit negative", func(c *Config) { setInt(c, func(p *Provider) *int { return &p.ContextLimit }, -1) }, true},
		{"max_tokens negative", func(c *Config) { setInt(c, func(p *Provider) *int { return &p.MaxTokens }, -1) }, true},
		{"warn_share zero", func(c *Config) { c.Prompt.WarnShare = 0 }, true},
		{"warn_share one", func(c *Config) { c.Prompt.WarnShare = 1 }, true},
		{"warn_share 0.01", func(c *Config) { c.Prompt.WarnShare = 0.01 }, false},
		{"warn_share 0.99", func(c *Config) { c.Prompt.WarnShare = 0.99 }, false},
		{"max_steps 0", func(c *Config) { c.Tools.MaxSteps = 0 }, true},
		{"max_steps 1", func(c *Config) { c.Tools.MaxSteps = 1 }, false},
		{"max_steps 200", func(c *Config) { c.Tools.MaxSteps = 200 }, false},
		{"max_steps 201", func(c *Config) { c.Tools.MaxSteps = 201 }, true},
		{"shell_timeout_sec 0", func(c *Config) { c.Tools.ShellTimeoutSec = 0 }, true},
		{"shell_timeout_sec 1", func(c *Config) { c.Tools.ShellTimeoutSec = 1 }, false},
		{"shell_timeout_sec 3600", func(c *Config) { c.Tools.ShellTimeoutSec = 3600 }, false},
		{"shell_timeout_sec 3601", func(c *Config) { c.Tools.ShellTimeoutSec = 3601 }, true},
		{"keep 0", func(c *Config) { c.Session.Keep = 0 }, true},
		{"keep 1", func(c *Config) { c.Session.Keep = 1 }, false},
		{"auto_compact off", func(c *Config) { c.Context.AutoCompact = 0 }, false},
		{"auto_compact 49", func(c *Config) { c.Context.AutoCompact = 49 }, true},
		{"auto_compact 50", func(c *Config) { c.Context.AutoCompact = 50 }, false},
		{"auto_compact 95", func(c *Config) { c.Context.AutoCompact = 95 }, false},
		{"auto_compact 96", func(c *Config) { c.Context.AutoCompact = 96 }, true},
		{"auto_compact negative", func(c *Config) { c.Context.AutoCompact = -1 }, true},
		{"warn_usd off", func(c *Config) { c.Cost.WarnUSD = 0 }, false},
		{"warn_usd 2.5", func(c *Config) { c.Cost.WarnUSD = 2.5 }, false},
		{"warn_usd negative", func(c *Config) { c.Cost.WarnUSD = -1 }, true},
		{"warn_usd absurd", func(c *Config) { c.Cost.WarnUSD = 1e9 }, true},
		{"https base_url", func(c *Config) { setBaseURL(c, "https://example.com/v1") }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Defaults()
			tt.mutate(&c)
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.HasPrefix(err.Error(), "config: ") {
				t.Fatalf("error %q must start with %q", err, "config: ")
			}
		})
	}
}

// setBaseURL sets base_url on the active provider of c.
func setBaseURL(c *Config, u string) {
	p := c.Providers[c.Provider]
	p.BaseURL = u
	c.Providers[c.Provider] = p
}

// setInt sets an int field of the active provider of c.
func setInt(c *Config, field func(*Provider) *int, v int) {
	p := c.Providers[c.Provider]
	*field(&p) = v
	c.Providers[c.Provider] = p
}

// ---------------------------------------------------------------------------
// Provider, Override, Dir
// ---------------------------------------------------------------------------

func TestProvider_APIKeyFromEnv(t *testing.T) {
	tests := []struct {
		name   string
		envVar string // APIKeyEnv of the provider
		value  string // value set in the environment ("" = unset)
		set    bool
		want   string
	}{
		{"set", "SWARG_TEST_KEY", "secret", true, "secret"},
		{"unset", "SWARG_TEST_KEY", "", false, ""},
		{"empty value", "SWARG_TEST_KEY", "", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(tt.envVar, tt.value)
			} else {
				t.Setenv(tt.envVar, "")
				os.Unsetenv(tt.envVar)
			}
			p := Provider{APIKeyEnv: tt.envVar}
			if got := p.APIKey(); got != tt.want {
				t.Fatalf("APIKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOverride_ChangesActiveProvider(t *testing.T) {
	c := Defaults()
	c.Override("other-model", "")
	if got := c.Active().Model; got != "other-model" {
		t.Fatalf("model = %q, want other-model", got)
	}
	if got := c.Active().BaseURL; got != Defaults().Active().BaseURL {
		t.Fatalf("empty baseURL must keep the default, got %q", got)
	}

	c.Override("", "http://10.0.0.1:1/v1")
	if got := c.Active().BaseURL; got != "http://10.0.0.1:1/v1" {
		t.Fatalf("base_url = %q", got)
	}
	if got := c.Active().Model; got != "other-model" {
		t.Fatalf("empty model must keep the previous value, got %q", got)
	}
}

func TestDir_UsesUserConfigDir(t *testing.T) {
	base, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("no user config dir: %v", err)
	}
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "swarg"); dir != want {
		t.Fatalf("Dir() = %q, want %q", dir, want)
	}
}

// The background grace and the shell_output wait are configurable (tests set 0 for speed).
func TestTools_BackgroundGraceAndOutputWait(t *testing.T) {
	d := Defaults().Tools
	if d.BackgroundGraceSec != 10 || d.OutputWaitSec != 10 {
		t.Fatalf("defaults %d / %d, want 10 / 10", d.BackgroundGraceSec, d.OutputWaitSec)
	}
	for _, tc := range []struct {
		name    string
		mut     func(*Config)
		wantErr bool
	}{
		{"grace 0", func(c *Config) { c.Tools.BackgroundGraceSec = 0 }, false},
		{"grace -1", func(c *Config) { c.Tools.BackgroundGraceSec = -1 }, true},
		{"grace 601", func(c *Config) { c.Tools.BackgroundGraceSec = 601 }, true},
		{"wait 0", func(c *Config) { c.Tools.OutputWaitSec = 0 }, false},
		{"wait 601", func(c *Config) { c.Tools.OutputWaitSec = 601 }, true},
	} {
		c := Defaults()
		tc.mut(&c)
		if err := c.Validate(); (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Session and context sections
// ---------------------------------------------------------------------------

func TestDefaults_SessionAndContext(t *testing.T) {
	c := Defaults()
	if !c.Session.Save || c.Session.Keep != 50 || !c.Session.Trace {
		t.Errorf("session defaults = %+v, want save, keep 50, trace", c.Session)
	}
	if c.Context.AutoCompact != 70 {
		t.Errorf("auto_compact = %d, want 70", c.Context.AutoCompact)
	}
}

func TestLoad_OldFileKeepsNewDefaults(t *testing.T) {
	c, err := Load(writeConfig(t, `{"tools":{"max_steps":7},"prompt":{"warn_share":0.5}}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Tools.MaxSteps != 7 || c.Prompt.WarnShare != 0.5 {
		t.Errorf("old keys changed: %+v %+v", c.Tools, c.Prompt)
	}
	if want := Defaults(); c.Session != want.Session || c.Context != want.Context {
		t.Errorf("new sections = %+v %+v, want defaults", c.Session, c.Context)
	}
}

func TestLoad_PartialSessionObject(t *testing.T) {
	c, err := Load(writeConfig(t, `{"session":{"keep":10},"context":{"auto_compact":0}}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Session.Save || !c.Session.Trace || c.Session.Keep != 10 {
		t.Errorf("session = %+v, want save and trace kept, keep 10", c.Session)
	}
	if c.Context.AutoCompact != 0 {
		t.Errorf("auto_compact = %d, want 0 (off)", c.Context.AutoCompact)
	}
	c, err = Load(writeConfig(t, `{"session":{"save":false,"trace":false}}`), true)
	if err != nil || c.Session.Save || c.Session.Trace || c.Session.Keep != 50 {
		t.Errorf("session = %+v, err %v", c.Session, err)
	}
}

func TestLoad_RejectsUnknownFieldInNewSections(t *testing.T) {
	for _, body := range []string{`{"context":{"autocompact":70}}`, `{"session":{"saves":true}}`} {
		_, err := Load(writeConfig(t, body), true)
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Errorf("%s: err = %v, want unknown field", body, err)
		}
	}
}

func TestLoad_RejectsOutOfRangeNewValues(t *testing.T) {
	for _, body := range []string{`{"context":{"auto_compact":30}}`, `{"session":{"keep":0}}`} {
		if _, err := Load(writeConfig(t, body), true); err == nil {
			t.Errorf("%s: want a validation error", body)
		}
	}
}

// ---------------------------------------------------------------------------
// Web section (T4)
// ---------------------------------------------------------------------------

func TestDefaults_WebSection(t *testing.T) {
	w := Defaults().Web
	if !w.Confirm || w.SearxURL != "" || w.AllowPrivate || w.TimeoutSec != 15 {
		t.Fatalf("web defaults = %+v", w)
	}
}

func TestLoad_OldFileKeepsWebDefaults(t *testing.T) {
	c, err := Load(writeConfig(t, `{"tools":{"max_steps":10}}`), true)
	if err != nil || c.Web != Defaults().Web {
		t.Fatalf("web = %+v err %v", c.Web, err)
	}
	c, err = Load(writeConfig(t, `{"web":{"confirm":false,"searx_url":"http://127.0.0.1:8888"}}`), true)
	if err != nil || c.Web.Confirm || c.Web.SearxURL != "http://127.0.0.1:8888" || c.Web.TimeoutSec != 15 {
		t.Fatalf("partial web = %+v err %v", c.Web, err)
	}
}

func TestLoad_RejectsBadWeb(t *testing.T) {
	for _, body := range []string{
		`{"web":{"timeout_sec":0}}`, `{"web":{"timeout_sec":121}}`, `{"web":{"searx_url":"not a url"}}`,
		`{"web":{"searx_url":"ftp://x.org"}}`, `{"web":{"bogus":1}}`,
	} {
		if _, err := Load(writeConfig(t, body), true); err == nil {
			t.Errorf("%s: err = %v", body, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Per-model context limit (context_limits, LimitFor)
// ---------------------------------------------------------------------------

func TestContextLimits_LimitForOrder(t *testing.T) {
	const table = "mlx-community/Qwen3.5-4B-4bit"
	const other = "some/other-model"
	tests := []struct {
		name   string
		limits map[string]int
		model  string
		want   int
	}{
		{"file entry beats default table", map[string]int{table: 8000}, table, 8000},
		{"default table beats context_limit", nil, table, 32768},
		{"Qwen3.5 fine-tunes are in the table", nil, "mlx-community/Huihui-Qwen3.5-4B-Claude-4.6-Opus-abliterated-4bit", 32768},
		{"MiMo distill is in the table", nil, "mlx-community/MiMo-V2.6-Distill-Qwen-9B-OptiQ-4bit", 32768},
		{"unknown model falls back to context_limit", nil, other, 16384},
		{"file entry for another model keeps the table", map[string]int{other: 4000}, table, 32768},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Provider{ContextLimit: 16384, ContextLimits: tt.limits}
			if got := p.LimitFor(tt.model); got != tt.want {
				t.Fatalf("LimitFor(%q) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}

func TestContextLimits_FileProviderKeepsDefaultTable(t *testing.T) {
	if Defaults().Active().ContextLimits != nil {
		t.Fatal("Defaults must not pre-fill context_limits")
	}
	c, err := Load(writeConfig(t, `{"providers":{"local":{"type":"openai","base_url":"http://127.0.0.1:8080/v1",
		"model":"mlx-community/Qwen3.5-4B-4bit","context_limit":16384,"max_tokens":2048}}}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Active().LimitFor("mlx-community/Qwen3.5-4B-4bit"); got != 32768 {
		t.Fatalf("LimitFor = %d, want 32768 from the default table", got)
	}

	c, err = Load(writeConfig(t, `{"providers":{"local":{"type":"openai","base_url":"http://127.0.0.1:8080/v1",
		"model":"mlx-community/Qwen3.5-4B-4bit","context_limit":16384,"max_tokens":2048,
		"context_limits":{"mlx-community/Qwen3.5-4B-4bit":8000}}}}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Active().LimitFor("mlx-community/Qwen3.5-4B-4bit"); got != 8000 {
		t.Fatalf("LimitFor = %d, want 8000 from the file", got)
	}
}

func TestContextLimits_Validate(t *testing.T) {
	tests := []struct {
		name    string
		limits  map[string]int
		wantErr bool
	}{
		{"no limits", nil, false},
		{"positive", map[string]int{"m": 1}, false},
		{"zero", map[string]int{"m": 0}, true},
		{"negative", map[string]int{"m": -5}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Defaults()
			p := c.Providers[c.Provider]
			p.ContextLimits = tt.limits
			c.Providers[c.Provider] = p
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.HasPrefix(err.Error(), "config: ") {
				t.Fatalf("error %q must start with %q", err, "config: ")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tasks section
// ---------------------------------------------------------------------------

func TestTasks_Defaults(t *testing.T) {
	want := Tasks{MaxDepth: 8, MaxSteps: 15, MaxPerTurn: 8}
	if got := Defaults().Tasks; got != want {
		t.Fatalf("tasks defaults = %+v, want %+v", got, want)
	}
}

func TestTasks_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"max_depth -1", func(c *Config) { c.Tasks.MaxDepth = -1 }, true},
		{"max_depth 0 (off)", func(c *Config) { c.Tasks.MaxDepth = 0 }, false},
		{"max_depth 8", func(c *Config) { c.Tasks.MaxDepth = 8 }, false},
		{"max_depth 9", func(c *Config) { c.Tasks.MaxDepth = 9 }, true},
		{"max_steps 0", func(c *Config) { c.Tasks.MaxSteps = 0 }, true},
		{"max_steps 1", func(c *Config) { c.Tasks.MaxSteps = 1 }, false},
		{"max_steps 200", func(c *Config) { c.Tasks.MaxSteps = 200 }, false},
		{"max_steps 201", func(c *Config) { c.Tasks.MaxSteps = 201 }, true},
		{"max_per_turn 0", func(c *Config) { c.Tasks.MaxPerTurn = 0 }, true},
		{"max_per_turn 1", func(c *Config) { c.Tasks.MaxPerTurn = 1 }, false},
		{"max_per_turn 50", func(c *Config) { c.Tasks.MaxPerTurn = 50 }, false},
		{"max_per_turn 51", func(c *Config) { c.Tasks.MaxPerTurn = 51 }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Defaults()
			tt.mutate(&c)
			if err := c.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTasks_PartialObjectKeepsDefaults(t *testing.T) {
	c, err := Load(writeConfig(t, `{"tasks": {"max_depth": 2}}`), true)
	if err != nil {
		t.Fatal(err)
	}
	want := Tasks{MaxDepth: 2, MaxSteps: 15, MaxPerTurn: 8}
	if c.Tasks != want {
		t.Fatalf("tasks = %+v, want %+v", c.Tasks, want)
	}
}

func TestThinking_DefaultOffAndPreset(t *testing.T) {
	p := Defaults().Active()
	if p.Thinking {
		t.Error("thinking must be off by default")
	}
	if p.ThinkingLimit() != 16384 {
		t.Errorf("limit %d, want 16384", p.ThinkingLimit())
	}
	b := p.ThinkingBody()
	args, _ := b["chat_template_kwargs"].(map[string]any)
	if args["enable_thinking"] != true || b["temperature"] != 0.6 || b["top_p"] != 0.95 || b["presence_penalty"] != 0.0 {
		t.Errorf("preset %v", b)
	}
}

func TestLoad_ThinkingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := `{"provider":"local","providers":{"local":{"type":"openai","base_url":"http://127.0.0.1:8080/v1","model":"m","context_limit":8000,"max_tokens":512,"thinking":true,"thinking_max_tokens":4000}}}`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if p := c.Active(); !p.Thinking || p.ThinkingLimit() != 4000 {
		t.Errorf("provider %+v", p)
	}
	bad := `{"provider":"local","providers":{"local":{"type":"openai","base_url":"http://127.0.0.1:8080/v1","model":"m","context_limit":8000,"max_tokens":512,"thinking_max_tokens":-1}}}`
	os.WriteFile(path, []byte(bad), 0o600)
	if _, err := Load(path, true); err == nil {
		t.Error("a negative thinking_max_tokens was accepted")
	}
}

// ---------------------------------------------------------------------------
// Idle timeout of a provider (R-63)
// ---------------------------------------------------------------------------

func TestProvider_IdleTimeoutDefault(t *testing.T) {
	if got := Defaults().Active().IdleTimeout(); got != 600*time.Second {
		t.Errorf("default idle timeout = %v, want 10m (a long silent tool call at ~6 tok/s)", got)
	}
}

func TestProvider_IdleTimeoutFromFile(t *testing.T) {
	path := writeConfig(t, `{"providers": {"local": {"type": "openai", "base_url": "http://10.1.2.3:9000/v1",
		"model": "m", "context_limit": 4096, "max_tokens": 512, "idle_timeout_s": 30}}}`)
	got, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if d := got.Active().IdleTimeout(); d != 30*time.Second {
		t.Errorf("idle timeout from the file = %v, want 30s", d)
	}
}

// ---------------------------------------------------------------------------
// Ollama provider type (R-64)
// ---------------------------------------------------------------------------

func TestDefaults_OllamaPreset(t *testing.T) {
	c := Defaults()
	p, ok := c.Providers["ollama"]
	if !ok {
		t.Fatal("no ollama preset in Defaults")
	}
	if p.Type != "ollama" || p.BaseURL != "http://127.0.0.1:11434/v1" || p.Model == "" || p.ContextLimit <= 0 {
		t.Fatalf("preset %+v", p)
	}
	if p.MaxTokens != 8192 { // a 250-line file is ~2800 tokens: 2048 cut it on every model (T7 code-07)
		t.Errorf("max_tokens = %d, want 8192", p.MaxTokens)
	}
	if p.ExtraBody["reasoning_effort"] != "none" {
		t.Errorf("extra_body = %v, want reasoning_effort none", p.ExtraBody)
	}
	if _, has := p.ExtraBody["chat_template_kwargs"]; has {
		t.Error("the ollama preset must not carry chat_template_kwargs (Ollama ignores it)")
	}
	c.Provider = "ollama"
	if err := c.Validate(); err != nil {
		t.Fatalf("ollama preset must validate: %v", err)
	}
}

func TestValidate_ProviderTypes(t *testing.T) {
	for _, tc := range []struct {
		typ string
		ok  bool
	}{{"openai", true}, {"ollama", true}, {"openrouter", true}, {"foo", false}} {
		c := Defaults()
		p := c.Active()
		p.Type = tc.typ
		c.Providers[c.Provider] = p
		err := c.Validate()
		if (err == nil) != tc.ok {
			t.Errorf("type %q: err = %v, want ok=%v", tc.typ, err, tc.ok)
		}
		if err != nil && !strings.Contains(err.Error(), "type must be") {
			t.Errorf("type %q: error %q does not name the type rule", tc.typ, err)
		}
	}
}

func TestThinkingBody_ByType(t *testing.T) {
	o := Provider{Type: "ollama"}.ThinkingBody()
	if o["reasoning_effort"] != "high" || o["temperature"] != 0.6 || o["top_p"] != 0.95 || o["presence_penalty"] != 0.0 {
		t.Errorf("ollama thinking body %v", o)
	}
	if _, has := o["chat_template_kwargs"]; has {
		t.Error("ollama thinking body must not carry chat_template_kwargs")
	}
	q := Provider{Type: "openai"}.ThinkingBody()
	args, _ := q["chat_template_kwargs"].(map[string]any)
	if args["enable_thinking"] != true {
		t.Errorf("openai thinking body %v", q)
	}
	if _, has := q["reasoning_effort"]; has {
		t.Error("openai thinking body must not carry reasoning_effort")
	}
}

// ---------------------------------------------------------------------------
// Skills and MCP (T8 S/M)
// ---------------------------------------------------------------------------

// A file without skills and mcp objects loads as before: no servers, MCP confirm on.
func TestSkillsAndMCP_Defaults(t *testing.T) {
	c := Defaults()
	if len(c.Skills.Disabled) != 0 || len(c.MCP.Servers) != 0 || !c.MCP.Confirm {
		t.Fatalf("defaults %+v %+v", c.Skills, c.MCP)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"mcp":{"servers":{"demo":{"command":"python3","args":["demo.py"],"tools":["a"],"trust":true},
		"docs":{"url":"https://mcp.example.com/mcp","headers":{"Authorization":"Bearer ${TOKEN}"}}}}}`), 0o644)
	c, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	s := c.MCP.Servers["demo"]
	if !c.MCP.Confirm || s.Disabled || !s.Trust || s.Timeout() != 60*time.Second || len(s.Tools) != 1 {
		t.Fatalf("server %+v confirm %v", s, c.MCP.Confirm)
	}
	if d := c.MCP.Servers["docs"]; d.Headers["Authorization"] != "Bearer ${TOKEN}" {
		t.Fatalf("headers %+v", d.Headers)
	}
}

func TestValidate_MCPServers(t *testing.T) {
	ok := MCPServer{Command: "python3", Args: []string{"x.py"}}
	tests := []struct {
		name    string
		servers map[string]MCPServer
		wantErr string
	}{
		{"command only", map[string]MCPServer{"demo": ok}, ""},
		{"url only", map[string]MCPServer{"docs": {URL: "http://127.0.0.1:3001/mcp", TimeoutSec: 600}}, ""},
		{"url with headers", map[string]MCPServer{"docs": {URL: "https://mcp.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ${TOKEN}"}}}, ""},
		{"headers with command", map[string]MCPServer{"demo": {Command: "x", Headers: map[string]string{"Authorization": "Bearer x"}}}, "headers"},
		{"empty header name", map[string]MCPServer{"docs": {URL: "https://h/", Headers: map[string]string{" ": "x"}}}, "headers"},
		{"bad name", map[string]MCPServer{"Demo Server": ok}, "name"},
		{"neither", map[string]MCPServer{"demo": {}}, "exactly one"},
		{"both", map[string]MCPServer{"demo": {Command: "x", URL: "http://h/"}}, "exactly one"},
		{"bad url", map[string]MCPServer{"demo": {URL: "ftp://h/"}}, "url"},
		{"timeout 601", map[string]MCPServer{"demo": {Command: "x", TimeoutSec: 601}}, "timeout_sec"},
		{"empty tool name", map[string]MCPServer{"demo": {Command: "x", Tools: []string{"a", " "}}}, "tools"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Defaults()
			c.MCP.Servers = tt.servers
			err := c.Validate()
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.HasPrefix(err.Error(), "config: ")) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
