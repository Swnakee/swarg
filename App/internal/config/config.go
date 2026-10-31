// Package config loads and validates the swarg configuration file.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Provider describes one OpenAI-compatible model server.
type Provider struct {
	Type          string         `json:"type"` // "openai" (mlx_lm.server and alike), "ollama" (R-64) or "openrouter" (R-102)
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
	// Profile is this Source's default prompt profile ("" = prompt.profile, R-116).
	Profile string `json:"profile,omitempty"`
	// MaxParallel is how many sub-agent requests may run at once on this Source (R-122);
	// 0 = the default of the type: 1 for openai and ollama (a local single lane), 4 for openrouter.
	MaxParallel int `json:"max_parallel,omitempty"`
}

// maxParallelLimit is the largest max_parallel a Source or agents.max_parallel may name.
const maxParallelLimit = 4

// Parallel is the number of sub-agent requests this Source takes at once (R-122).
func (p Provider) Parallel() int {
	switch {
	case p.MaxParallel > 0:
		return p.MaxParallel
	case p.Type == "openrouter":
		return maxParallelLimit
	}
	return 1
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
	// Profile is the start prompt profile (compact, standard, full; "" = compact, R-116);
	// Profiles maps a model pattern to a profile and wins over a provider's profile.
	Profile  string            `json:"profile,omitempty"`
	Profiles map[string]string `json:"profiles,omitempty"`
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

// Cost holds the spend settings (R-114). There is no hard limit: swarg shows the spend and warns.
type Cost struct {
	WarnUSD float64 `json:"warn_usd"` // one warning when the Session spend reaches this many USD; 0 = off
}

// Tasks holds the Task mode limits (nested sub-tasks of the agent).
type Tasks struct {
	MaxDepth   int `json:"max_depth"`    // nesting depth, 0..8, 0 = Tasks off
	MaxSteps   int `json:"max_steps"`    // model steps of one Task, 1..200
	MaxPerTurn int `json:"max_per_turn"` // Tasks started in one Turn, 1..50
}

// Skills holds the Skill settings (R-84).
type Skills struct {
	Disabled []string `json:"disabled"` // Skill names switched off
}

// Agents holds the sub-agent settings (R-120..R-122).
type Agents struct {
	Disabled    []string `json:"disabled"`     // agent type names switched off
	MaxParallel int      `json:"max_parallel"` // agents running at once over all Sources, 1..4
}

// MCPServer is one MCP Server (R-85): a stdio program (command) or a Streamable HTTP endpoint (url).
type MCPServer struct {
	Command    string            `json:"command,omitempty"`
	Args       []string          `json:"args,omitempty"`
	Env        map[string]string `json:"env,omitempty"` // values pass through os.ExpandEnv: name a secret, never write it
	URL        string            `json:"url,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"` // HTTP headers for url servers; values pass through os.ExpandEnv (R-91)
	Disabled   bool              `json:"disabled"`          // not started, its Tools not offered
	Trust      bool              `json:"trust"`             // believe readOnlyHint: such tools run without approval (R-87)
	Tools      []string          `json:"tools,omitempty"`   // allowlist of tool names offered to the model; empty = all (R-88)
	TimeoutSec int               `json:"timeout_sec"`       // one tools/call, 1..600; 0 = 60
}

// Timeout is the limit of one tools/call: TimeoutSec, or 60 s when unset.
func (s MCPServer) Timeout() time.Duration {
	if s.TimeoutSec <= 0 {
		return 60 * time.Second
	}
	return time.Duration(s.TimeoutSec) * time.Second
}

// MCP holds the MCP settings (R-85..R-88).
type MCP struct {
	Confirm bool                 `json:"confirm"` // ask for approval before an MCP tool runs (unless trusted and read-only)
	Servers map[string]MCPServer `json:"servers"`
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
	Cost      Cost                `json:"cost"`
	Tasks     Tasks               `json:"tasks"`
	Skills    Skills              `json:"skills"`
	Agents    Agents              `json:"agents"`
	MCP       MCP                 `json:"mcp"`
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
			// Ollama (R-64): thinking is switched with reasoning_effort; it reads only temperature,
			// top_p, presence_penalty, frequency_penalty, seed, stop and max_tokens from /v1. The
			// context window is a server setting (OLLAMA_CONTEXT_LENGTH); context_limit should match it.
			"ollama": {
				Type:         "ollama",
				BaseURL:      "http://127.0.0.1:11434/v1",
				Model:        "qwen3.5:4b",
				ContextLimit: 16384,
				MaxTokens:    4096, // a 250-line file is ~2800 tokens; 2048 cut it on every model (T7)
				ExtraBody: map[string]any{
					"reasoning_effort": "none",
					"temperature":      0.7,
					"top_p":            0.8,
					"presence_penalty": 1.5,
				},
			},
			// OpenRouter (R-102): the key comes from the environment only (I-35); the Catalog's
			// context_length beats context_limit per model; thinking goes through `reasoning`.
			"openrouter": {
				Type:         "openrouter",
				BaseURL:      "https://openrouter.ai/api/v1",
				Model:        "nvidia/nemotron-3.5-lightning:free",
				ContextLimit: 32768,
				MaxTokens:    4096,
				APIKeyEnv:    "OPENROUTER_API_KEY",
				Profile:      "standard", // eval B4: compact 15/16, standard 16/16, full 14/16 (R-129)
			},
		},
		Prompt: Prompt{WarnShare: 0.25},
		Tools: Tools{Disabled: []string{}, MaxSteps: 50, ShellTimeoutSec: 120, AutoApprove: false,
			BackgroundGraceSec: 10, OutputWaitSec: 10},
		Web:     Web{Confirm: true, TimeoutSec: 15},
		Session: Session{Save: true, Keep: 50, Trace: true},
		Context: Context{AutoCompact: 70},
		Tasks:   Tasks{MaxDepth: 8, MaxSteps: 15, MaxPerTurn: 8},
		Agents:  Agents{MaxParallel: maxParallelLimit},
		MCP:     MCP{Confirm: true},
	}
}

// defaultThinkingMaxTokens is the output limit with thinking on: the reasoning shares it.
const defaultThinkingMaxTokens = 8192

// ThinkingBody is the part of the request body that changes with thinking on (R-60): it is laid
// over ExtraBody. Qwen3.5 thinking preset for precise coding. An ollama provider
// switches thinking with reasoning_effort instead of chat_template_kwargs (R-64); an openrouter
// provider with the `reasoning` object (R-102).
func (p Provider) ThinkingBody() map[string]any {
	if p.Type == "openrouter" {
		return map[string]any{"reasoning": map[string]any{"effort": "high"}}
	}
	if p.Type == "ollama" {
		return map[string]any{
			"reasoning_effort": "high",
			"temperature":      0.6,
			"top_p":            0.95,
			"presence_penalty": 0.0,
		}
	}
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
// Other provider entries are not checked here (an unused broken entry must not stop the start);
// Provider.Validate checks one before a run-time switch (R-100).
func (c Config) Validate() error {
	p, ok := c.Providers[c.Provider]
	if !ok {
		return fmt.Errorf("config: active provider %q is not defined", c.Provider)
	}
	if err := p.Validate(c.Provider); err != nil {
		return err
	}
	if c.Prompt.WarnShare <= 0 || c.Prompt.WarnShare >= 1 {
		return fmt.Errorf("config: prompt.warn_share must be between 0 and 1 exclusive, got %v", c.Prompt.WarnShare)
	}
	if err := c.Prompt.validateProfiles(); err != nil {
		return err
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
	if err := c.MCP.validate(); err != nil {
		return err
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
	if w := c.Cost.WarnUSD; !(w >= 0) || w > 1e6 { // also refuses NaN
		return fmt.Errorf("config: cost.warn_usd must be 0 (off) or a positive amount in USD, got %v", w)
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
	if n := c.Agents.MaxParallel; n < 1 || n > maxParallelLimit {
		return fmt.Errorf("config: agents.max_parallel must be between 1 and %d, got %d", maxParallelLimit, n)
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

// APIKey returns the API key (R-105): the environment variable named by APIKeyEnv, else the
// keys file <config dir>/keys.env, else the old keys directory (R-106). A file that others can
// read (any group or other permission bit, outside Windows) is ignored. "" when there is no key.
func (p Provider) APIKey() string {
	if p.APIKeyEnv == "" {
		return ""
	}
	v, _ := UserKeys().Lookup(p.APIKeyEnv)
	return v
}

// NoKey is the hint for a provider whose key is missing: where to put it.
func (p Provider) NoKey() string {
	if f := UserKeys().File; f != "" && ValidKeyName(p.APIKeyEnv) {
		return fmt.Sprintf("no key: set %s or add the line %s=... to %s (chmod 600; /keys init creates it)", p.APIKeyEnv, p.APIKeyEnv, f)
	}
	return "no key: set " + p.APIKeyEnv
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

// ---------------------------------------------------------------------------
// Provider checks
// ---------------------------------------------------------------------------

// Validate checks one provider entry; name is used in the error texts.
func (p Provider) Validate(name string) error {
	if p.Type != "openai" && p.Type != "ollama" && p.Type != "openrouter" {
		return fmt.Errorf("config: provider %q: type must be \"openai\", \"ollama\" or \"openrouter\", got %q", name, p.Type)
	}
	if u, err := url.Parse(p.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("config: provider %q: base_url must be an http(s) URL with a host", name)
	}
	if p.Model == "" {
		return fmt.Errorf("config: provider %q: model is empty", name)
	}
	if p.ContextLimit <= 0 {
		return fmt.Errorf("config: provider %q: context_limit must be > 0", name)
	}
	if p.Profile != "" {
		if err := checkProfile("providers."+name+".profile", p.Profile); err != nil {
			return err
		}
	}
	ids := make([]string, 0, len(p.ContextLimits))
	for id := range p.ContextLimits {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if p.ContextLimits[id] <= 0 {
			return fmt.Errorf("config: provider %q: context_limits[%q] must be > 0", name, id)
		}
	}
	if p.MaxParallel < 0 || p.MaxParallel > maxParallelLimit {
		return fmt.Errorf("config: provider %q: max_parallel must be between 0 and %d, got %d", name, maxParallelLimit, p.MaxParallel)
	}
	if p.MaxTokens <= 0 {
		return fmt.Errorf("config: provider %q: max_tokens must be > 0", name)
	}
	if p.ThinkingMaxTokens < 0 {
		return fmt.Errorf("config: provider %q: thinking_max_tokens must be >= 0", name)
	}
	return nil
}

// Ready returns the provider name if it can be switched to (R-100): defined, valid, and its key
// variable set when it names one (R-104); else the reason.
func (c Config) Ready(name string) (Provider, error) {
	p, ok := c.Providers[name]
	if !ok {
		return p, fmt.Errorf("provider %q is not defined; /server lists them", name)
	}
	if err := p.Validate(name); err != nil {
		return p, err
	}
	if p.APIKeyEnv != "" && p.APIKey() == "" {
		return p, fmt.Errorf("provider %q: %s", name, p.NoKey())
	}
	return p, nil
}

// Local reports whether the server is on this machine or the local network (R-104): plain http to
// localhost, a loopback, private or link-local address, or a plain host name (a LAN name). https or
// a public address is a cloud service that sees the conversation.
func (p Provider) Local() bool {
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// ---------------------------------------------------------------------------
// MCP validation
// ---------------------------------------------------------------------------

// mcpName is the allowed shape of an MCP Server name (it becomes part of the tool names).
var mcpName = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// validate checks every server entry: the name, command xor url, the timeout and the allowlist.
func (m MCP) validate() error {
	names := make([]string, 0, len(m.Servers))
	for n := range m.Servers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := m.Servers[n]
		switch {
		case !mcpName.MatchString(n):
			return fmt.Errorf("config: mcp.servers: name %q must match %s", n, mcpName)
		case (s.Command == "") == (s.URL == ""):
			return fmt.Errorf("config: mcp.servers.%s: exactly one of command and url is required", n)
		case s.TimeoutSec < 0 || s.TimeoutSec > 600:
			return fmt.Errorf("config: mcp.servers.%s: timeout_sec must be between 0 and 600, got %d", n, s.TimeoutSec)
		}
		if u := s.URL; u != "" {
			if pu, err := url.Parse(u); err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
				return fmt.Errorf("config: mcp.servers.%s: url must be an http(s) URL with a host, got %q", n, u)
			}
		} else if len(s.Headers) > 0 {
			return fmt.Errorf("config: mcp.servers.%s: headers need url", n)
		}
		for k := range s.Headers {
			if strings.TrimSpace(k) == "" {
				return fmt.Errorf("config: mcp.servers.%s: headers must not contain an empty name", n)
			}
		}
		for _, t := range s.Tools {
			if strings.TrimSpace(t) == "" {
				return fmt.Errorf("config: mcp.servers.%s: tools must not contain an empty name", n)
			}
		}
	}
	return nil
}
