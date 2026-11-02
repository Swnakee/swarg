package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"swarg/internal/agent"
	"swarg/internal/config"
	"swarg/internal/llm/openai"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// Sources (R-100..R-104): the configured providers as the app switches between them
// ---------------------------------------------------------------------------

const (
	probeTimeout = 5 * time.Second // one /models probe of /server
	cloudNotice  = "cloud"         // the one-time notice key (R-104)
)

// sources adapts the config's providers to ui.ServersAPI (the /server list and switch) and
// ui.ModelsAPI (the active Source's Catalog): one openai.Client per provider (its key is read per request), the Catalog of each
// probed or listed provider, the Recent models and the one-time cloud notice.
type sources struct {
	cfg     *config.Config
	cwd     string
	recent  *config.Recent
	notices *config.Approvals
	a       *agent.Agent // set by main once the agent exists

	mu      sync.Mutex
	active  string
	clients map[string]*openai.Client
	catalog map[string][]openai.ModelInfo
	tried   map[string]bool // limitFor fetched (or tried to fetch) this Catalog
}

func newSources(cfg *config.Config, cwd string, recent *config.Recent, notices *config.Approvals) *sources {
	return &sources{cfg: cfg, cwd: cwd, recent: recent, notices: notices, active: cfg.Provider,
		clients: map[string]*openai.Client{}, catalog: map[string][]openai.ModelInfo{}, tried: map[string]bool{}}
}

var _ ui.ServersAPI = (*sources)(nil)
var _ ui.ModelsAPI = (*sources)(nil)
var _ ui.CostAPI = (*sources)(nil)

// KeyUsage is what the active Source reports about its API key (OpenRouter GET /key, R-113); a
// Source of another type has no such endpoint and gives ui.ErrNoKeyInfo. The key is sent by the
// client and is in no text returned here (I-39).
func (s *sources) KeyUsage(ctx context.Context) (ui.KeyUsage, error) {
	name := s.Current()
	if s.cfg.Providers[name].Type != "openrouter" {
		return ui.KeyUsage{}, ui.ErrNoKeyInfo
	}
	k, err := s.client(name).KeyInfo(ctx)
	if err != nil {
		return ui.KeyUsage{}, err
	}
	return ui.KeyUsage{Label: k.Label, Limit: k.Limit, Remaining: k.Remaining, Usage: k.Usage,
		Daily: k.Daily, Weekly: k.Weekly, Monthly: k.Monthly}, nil
}

// Current is the name of the Source in use.
func (s *sources) Current() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

func (s *sources) setActive(name string) {
	s.mu.Lock()
	s.active = name
	s.mu.Unlock()
}

// client is the provider's client, made on first use.
func (s *sources) client(name string) *openai.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[name]; ok {
		return c
	}
	p := s.cfg.Providers[name]
	c := openai.New(p.BaseURL, "", nil)
	c.Key = p.APIKey                // read on every request: a key set in /keys works at once (R-107)
	c.IdleTimeout = p.IdleTimeout() // R-63
	s.clients[name] = c
	return c
}

// limitFor is the ContextLimit of a model on provider name: the Catalog's context_length when
// known (R-102), else the entry's LimitFor. An openrouter Catalog not loaded yet (the start
// provider gets no Switch) is fetched once, best effort.
func (s *sources) limitFor(name string) func(model string) int {
	return func(model string) int {
		s.mu.Lock()
		cat, tried := s.catalog[name], s.tried[name]
		s.tried[name] = true
		s.mu.Unlock()
		if cat == nil && !tried && s.cfg.Providers[name].Type == "openrouter" {
			cat, _ = s.fetch(context.Background(), name)
		}
		for _, m := range cat {
			if m.ID == model && m.Context > 0 {
				return m.Context
			}
		}
		return s.cfg.Providers[name].LimitFor(model)
	}
}

// source builds the agent's Source for provider name with model.
func (s *sources) source(name, model string) agent.Source {
	p, c := s.cfg.Providers[name], s.client(name)
	return agent.Source{Name: name, Provider: c, Model: model, MaxTokens: p.MaxTokens, ContextLimit: p.ContextLimit,
		Extra: p.ExtraBody, ThinkExtra: p.ThinkingBody(), ThinkTokens: p.ThinkingLimit(), LimitFor: s.limitFor(name),
		Health: func(ctx context.Context) error { return c.Health(ctx, s.a.Model()) }}
}

// Switch makes name the agent's Source from the next request on (R-100). An openrouter Source
// gets its Catalog first (the limits of its models); the one-time cloud notice is returned the
// first time a project leaves the local machine (R-104).
func (s *sources) Switch(name string) (string, error) {
	p, err := s.cfg.Ready(name)
	if err != nil {
		return "", err
	}
	if s.a == nil {
		return "", errors.New("the agent is not running")
	}
	if p.Type == "openrouter" {
		s.fetch(context.Background(), name) // best effort: without it the limits fall back to the config
	}
	prev := s.Current()
	s.setActive(name) // before SetSource: its EvModel records the model under the new Source
	if err := s.a.SetSource(s.source(name, s.recent.First(name, p.Model))); err != nil {
		s.setActive(prev)
		return "", err
	}
	if p.Local() || s.notices.Noticed(s.cwd, cloudNotice) {
		return "", nil
	}
	_ = s.notices.Notice(s.cwd, cloudNotice)
	return fmt.Sprintf("notice: %s is a cloud service: your messages, the files the tools read and the command output "+
		"are sent to a third party (%s); shown once per project", name, hostOf(p.BaseURL)), nil
}

// fetch loads and keeps the Catalog of name (bounded by probeTimeout inside ctx); it returns the error of the request.
func (s *sources) fetch(ctx context.Context, name string) ([]openai.ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cat, err := s.client(name).Catalog(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.catalog[name] = cat
	s.mu.Unlock()
	return cat, nil
}

// Servers lists every configured provider with its state, probed in parallel (R-100).
func (s *sources) Servers(ctx context.Context) ([]ui.ServerRow, error) {
	names := slices.Sorted(maps.Keys(s.cfg.Providers))
	rows := make([]ui.ServerRow, len(names))
	var wg sync.WaitGroup
	for i, n := range names {
		p := s.cfg.Providers[n]
		rows[i] = ui.ServerRow{Name: n, Type: p.Type, Address: p.BaseURL, Active: n == s.Current()}
		if err := p.Validate(n); err != nil {
			rows[i].State = "bad config: " + strings.TrimPrefix(err.Error(), fmt.Sprintf("config: provider %q: ", n))
			continue
		}
		if p.APIKeyEnv != "" && p.APIKey() == "" {
			rows[i].State = p.NoKey()
			continue
		}
		wg.Add(1)
		go func(i int, n string) {
			defer wg.Done()
			if _, err := s.fetch(ctx, n); err != nil {
				rows[i].State = "down: " + netCause(err)
				return
			}
			rows[i].State = "ok"
		}(i, n)
	}
	wg.Wait()
	return rows, nil
}

// List is the active Source's Catalog as /model shows it: tool-capable models only when the
// server reports supported_parameters, the Recent ones marked, the price per row (R-101, R-102).
func (s *sources) List(ctx context.Context) ([]ui.ModelRow, error) {
	name := s.Current()
	cat, err := s.fetch(ctx, name)
	if err != nil {
		return nil, err
	}
	recent := s.recent.Models(name)
	var rows []ui.ModelRow
	for _, m := range cat {
		if m.HasParameters && !m.Tools {
			continue
		}
		r := ui.ModelRow{ID: m.ID, Name: m.Name}
		for _, id := range recent {
			r.Recent = r.Recent || id == m.ID
		}
		if m.HasParameters {
			r.Price = priceText(m)
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// Used records model as the last used one of the active Source (R-101).
func (s *sources) Used(model string) error { return s.recent.Use(s.Current(), model) }

// netCause is the network error behind a failed request (without the URL), else the whole text.
func netCause(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

// priceText is `free` or the prices in USD per million tokens, in then out.
func priceText(m openai.ModelInfo) string {
	if m.Free() {
		return "free"
	}
	return fmt.Sprintf("$%.2f/$%.2f per M", m.Prompt*1e6, m.Completion*1e6)
}

// hostOf is the host of a URL for the notice text.
func hostOf(raw string) string {
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	if i := strings.IndexAny(raw, "/:"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}
