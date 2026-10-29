// Package config loads and validates the swarg configuration file.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Provider describes one OpenAI-compatible model server.
type Provider struct {
	Type          string         `json:"type"` // only "openai" in T2
	BaseURL       string         `json:"base_url"`
	Model         string         `json:"model"`
	ContextLimit  int            `json:"context_limit"`
	ContextLimits map[string]int `json:"context_limits,omitempty"` // model id -> context limit, see LimitFor
	MaxTokens     int            `json:"max_tokens"`
	APIKeyEnv     string         `json:"api_key_env,omitempty"`
	ExtraBody     map[string]any `json:"extra_body,omitempty"`
	// Thinking is the start state of the thinking mode (R-60); false = off, as before. /think
	// changes it in the app. ThinkingMaxTokens is the output limit while it is on (0 = 8192).
	Thinking          bool `json:"thinking"`
	ThinkingMaxTokens int  `json:"thinking_max_tokens,omitempty"`
	// IdleTimeoutSec ends a stream with no bytes for this long (R-63); 0 = 600. mlx_lm.server
	// sends a tool call in one chunk at the end of the generation, so a long call at a few tok/s
	// is minutes of silence.
	IdleTimeoutSec int `json:"idle_timeout_s,omitempty"`
}

// IdleTimeout is the stream idle limit: IdleTimeoutSec, or 10 minutes when unset.
func (p Provider) IdleTimeout() time.Duration {
	if p.IdleTimeoutSec <= 0 {
		return 600 * time.Second
	}
	return time.Duration(p.IdleTimeoutSec) * time.Second
}

// Prompt holds the system prompt settings.
type Prompt struct {
	WarnShare float64 `json:"warn_share"` // default 0.25
}

// Tools holds the tool settings.
type Tools struct {
	Disabled        []string `json:"disabled"`          // tool names switched off
	MaxSteps        int      `json:"max_steps"`         // model steps per turn, 1..200
	ShellTimeoutSec int      `json:"shell_timeout_sec"` // shell tool timeout, 1..3600
	AutoApprove     bool     `json:"auto_approve"`      // start with confirmations off
	// a background:true command that ends within this many seconds returns a normal result
	BackgroundGraceSec int `json:"background_grace_sec"` // 0..600, 0 = straight to the background
	OutputWaitSec      int `json:"output_wait_sec"`      // shell_output waits this long for a job end, 0..600
}

// Web holds the settings of the web_search and web_fetch tools.
type Web struct {
	Confirm      bool   `json:"confirm"`       // ask for approval before a web call (like shell)
	SearxURL     string `json:"searx_url"`     // SearXNG base URL; empty = DuckDuckGo
	AllowPrivate bool   `json:"allow_private"` // let web_fetch reach loopback and private addresses
	TimeoutSec   int    `json:"timeout_sec"`   // one request, 1..120
}

// Session holds the SessionFile and Trace settings.
type Session struct {
	Save  bool `json:"save"`  // write every Session to a SessionFile
	Keep  int  `json:"keep"`  // SessionFiles kept per project, >= 1
	Trace bool `json:"trace"` // write the debug Trace next to the SessionFile
}

// Context holds the context economy settings.
type Context struct {
	// AutoCompact is the context fill in percent at which a Compaction starts by itself;
	// 0 switches it off, otherwise 50..95.
	AutoCompact int `json:"auto_compact"`
}

// Tasks holds the Task mode limits (nested sub-tasks of the agent).
type Tasks struct {
	MaxDepth   int `json:"max_depth"`    // nesting depth, 0..8, 0 = Tasks off
	MaxSteps   int `json:"max_steps"`    // model steps of one Task, 1..200
	MaxPerTurn int `json:"max_per_turn"` // Tasks started in one Turn, 1..50
}

// Config is the whole configuration file.
type Config struct {
	Provider  string              `json:"provider"`
	Providers map[string]Provider `json:"providers"`
	Prompt    Prompt              `json:"prompt"`
	Tools     Tools               `json:"tools"`
	Web       Web                 `json:"web"`
	Session   Session             `json:"session"`
	Context   Context             `json:"context"`
	Tasks     Tasks               `json:"tasks"`
}

// ---------------------------------------------------------------------------
// Defaults and location
// ---------------------------------------------------------------------------

// Dir returns the swarg configuration directory.
func Dir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return filepath.Join(dir, "swarg"), nil
}

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{
		Provider: "local",
		Providers: map[string]Provider{
			"local": {
				Type:         "openai",
				BaseURL:      "http://127.0.0.1:8080/v1",
				Model:        "mlx-community/Qwen3.5-4B-4bit",
				ContextLimit: 16384,
				MaxTokens:    2048,
				ExtraBody: map[string]any{
					"chat_template_kwargs": map[string]any{"enable_thinking": false},
					// Qwen3.5 non-thinking, general tasks (PLAN 5.7); the server default is greedy
					"temperature":        0.7,
					"top_p":              0.8,
					"top_k":              20,
					"min_p":              0.0,
					"presence_penalty":   1.5,
					"repetition_penalty": 1.0,
				},
			},
		},
		Prompt: Prompt{WarnShare: 0.25},
		Tools: Tools{Disabled: []string{}, MaxSteps: 25, ShellTimeoutSec: 120, AutoApprove: false,
			BackgroundGraceSec: 10, OutputWaitSec: 10},
		Web:     Web{Confirm: true, TimeoutSec: 15},
		Session: Session{Save: true, Keep: 50, Trace: true},
		Context: Context{AutoCompact: 70},
		Tasks:   Tasks{MaxDepth: 8, MaxSteps: 15, MaxPerTurn: 8},
	}
}

// defaultThinkingMaxTokens is the output limit with thinking on: the reasoning shares it.
const defaultThinkingMaxTokens = 8192

// ThinkingBody is the part of the request body that changes with thinking on (R-60): it is laid
// over ExtraBody. Qwen3.5 thinking preset for precise coding (AI_Notes 05).
func (p Provider) ThinkingBody() map[string]any {
	return map[string]any{
		"chat_template_kwargs": map[string]any{"enable_thinking": true},
		"temperature":          0.6,
		"top_p":                0.95,
		"top_k":                20,
		"min_p":                0.0,
		"presence_penalty":     0.0,
	}
}

// ThinkingLimit is the output limit (max_tokens) with thinking on.
func (p Provider) ThinkingLimit() int {
	if p.ThinkingMaxTokens > 0 {
		return p.ThinkingMaxTokens
	}
	return defaultThinkingMaxTokens
}

// defaultLimits is the built-in context limit per model id. It stays out of Defaults:
// a file that defines providers.local replaces the whole entry, so the table would be lost.
var defaultLimits = map[string]int{
	"mlx-community/Qwen3.5-0.8B-4bit": 32768,
	"mlx-community/Qwen3.5-2B-4bit":   32768,
	"mlx-community/Qwen3.5-4B-4bit":   32768,
	"mlx-community/Qwen3.5-9B-4bit":   32768,
	// Qwen3.5 fine-tunes with the same 262k config (model comparison, T4)
	"mlx-community/Huihui-Qwen3.5-4B-Claude-4.6-Opus-abliterated-4bit": 32768,
	"mlx-community/MiMo-V2.6-Distill-Qwen-9B-OptiQ-4bit":               32768,
}

// ---------------------------------------------------------------------------
// Loading and validation
// ---------------------------------------------------------------------------

// Load reads the config file at path on top of Defaults. A missing file is
// fine unless explicit is true. Unknown fields and trailing data are errors.
// A provider entry in the file replaces the default entry of the same name.
func Load(path string, explicit bool) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && !explicit {
		return Defaults(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}

	cfg := Defaults()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("config: parse %s: unexpected data after the JSON value", path)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Validate checks the active provider, the prompt, tool, session, context and task settings.
func (c Config) Validate() error {
	p, ok := c.Providers[c.Provider]
	if !ok {
		return fmt.Errorf("config: active provider %q is not defined", c.Provider)
	}
	if p.Type != "openai" {
		return fmt.Errorf("config: provider %q: type must be \"openai\", got %q", c.Provider, p.Type)
	}
	if u, err := url.Parse(p.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("config: provider %q: base_url must be an http(s) URL with a host", c.Provider)
	}
	if p.Model == "" {
		return fmt.Errorf("config: provider %q: model is empty", c.Provider)
	}
	if p.ContextLimit <= 0 {
		return fmt.Errorf("config: provider %q: context_limit must be > 0", c.Provider)
	}
	ids := make([]string, 0, len(p.ContextLimits))
	for id := range p.ContextLimits {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if p.ContextLimits[id] <= 0 {
			return fmt.Errorf("config: provider %q: context_limits[%q] must be > 0", c.Provider, id)
		}
	}
	if p.MaxTokens <= 0 {
		return fmt.Errorf("config: provider %q: max_tokens must be > 0", c.Provider)
	}
	if p.ThinkingMaxTokens < 0 {
		return fmt.Errorf("config: provider %q: thinking_max_tokens must be >= 0", c.Provider)
	}
	if c.Prompt.WarnShare <= 0 || c.Prompt.WarnShare >= 1 {
		return fmt.Errorf("config: prompt.warn_share must be between 0 and 1 exclusive, got %v", c.Prompt.WarnShare)
	}
	if c.Tools.MaxSteps < 1 || c.Tools.MaxSteps > 200 {
		return fmt.Errorf("config: tools.max_steps must be between 1 and 200, got %d", c.Tools.MaxSteps)
	}
	if c.Tools.BackgroundGraceSec < 0 || c.Tools.BackgroundGraceSec > 600 {
		return fmt.Errorf("config: tools.background_grace_sec must be between 0 and 600, got %d", c.Tools.BackgroundGraceSec)
	}
	if c.Tools.OutputWaitSec < 0 || c.Tools.OutputWaitSec > 600 {
		return fmt.Errorf("config: tools.output_wait_sec must be between 0 and 600, got %d", c.Tools.OutputWaitSec)
	}
	if c.Tools.ShellTimeoutSec < 1 || c.Tools.ShellTimeoutSec > 3600 {
		return fmt.Errorf("config: tools.shell_timeout_sec must be between 1 and 3600, got %d", c.Tools.ShellTimeoutSec)
	}
	if c.Web.TimeoutSec < 1 || c.Web.TimeoutSec > 120 {
		return fmt.Errorf("config: web.timeout_sec must be between 1 and 120, got %d", c.Web.TimeoutSec)
	}
	if u := c.Web.SearxURL; u != "" {
		if pu, err := url.Parse(u); err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
			return fmt.Errorf("config: web.searx_url must be an http(s) URL with a host, got %q", u)
		}
	}
	if c.Session.Keep < 1 {
		return fmt.Errorf("config: session.keep must be >= 1, got %d", c.Session.Keep)
	}
	if a := c.Context.AutoCompact; a != 0 && (a < 50 || a > 95) {
		return fmt.Errorf("config: context.auto_compact must be 0 (off) or between 50 and 95, got %d", a)
	}
	if d := c.Tasks.MaxDepth; d < 0 || d > 8 {
		return fmt.Errorf("config: tasks.max_depth must be between 0 and 8, got %d", d)
	}
	if c.Tasks.MaxSteps < 1 || c.Tasks.MaxSteps > 200 {
		return fmt.Errorf("config: tasks.max_steps must be between 1 and 200, got %d", c.Tasks.MaxSteps)
	}
	if c.Tasks.MaxPerTurn < 1 || c.Tasks.MaxPerTurn > 50 {
		return fmt.Errorf("config: tasks.max_per_turn must be between 1 and 50, got %d", c.Tasks.MaxPerTurn)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Accessors and overrides
// ---------------------------------------------------------------------------

// Active returns the active provider (zero Provider if it is not defined).
func (c Config) Active() Provider {
	return c.Providers[c.Provider]
}

// Override sets model and baseURL on the active provider when they are non-empty.
// It does nothing if the active provider is not defined; Validate reports that.
func (c *Config) Override(model, baseURL string) {
	p, ok := c.Providers[c.Provider]
	if !ok {
		return
	}
	if model != "" {
		p.Model = model
	}
	if baseURL != "" {
		p.BaseURL = baseURL
	}
	c.Providers[c.Provider] = p
}

// APIKey returns the API key from the environment variable named by APIKeyEnv,
// or "" when APIKeyEnv is empty or unset.
func (p Provider) APIKey() string {
	if p.APIKeyEnv == "" {
		return ""
	}
	return os.Getenv(p.APIKeyEnv)
}

// LimitFor returns the context limit of model: the file's context_limits entry,
// else the built-in table, else ContextLimit. The table therefore beats an explicit
// context_limit for its model ids.
func (p Provider) LimitFor(model string) int {
	if n, ok := p.ContextLimits[model]; ok {
		return n
	}
	if n, ok := defaultLimits[model]; ok {
		return n
	}
	return p.ContextLimit
}
