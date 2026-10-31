package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"
)

// ---------------------------------------------------------------------------
// Catalog (GET /models)
// ---------------------------------------------------------------------------

const modelsTimeout = 10 * time.Second

// modelsBodyLimit caps the model list body (OpenRouter lists ~500 models with their details).
const modelsBodyLimit = 16 << 20

// ModelInfo is one Catalog entry as the server reports it (R-102). A plain OpenAI-compatible
// server fills only ID; OpenRouter adds the name, the context length, the prices in USD per token
// and the supported parameters, from which Tools and Reasoning are read.
type ModelInfo struct {
	ID, Name         string
	Context          int
	Prompt           float64
	Completion       float64
	Tools, Reasoning bool
	HasParameters    bool // supported_parameters was present (Tools is meaningful)
}

// Free reports whether both prices are zero.
func (m ModelInfo) Free() bool { return m.Prompt == 0 && m.Completion == 0 }

// Catalog returns the server's model list at GET {base}/models, sorted by id, without blanks or
// duplicates. mlx_lm.server lists every model in the local cache, not only the loaded one.
func (c *Client) Catalog(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	resp, err := c.do(ctx, http.MethodGet, "/models", nil, "application/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var list struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
			Pricing       struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
			Parameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, modelsBodyLimit)).Decode(&list); err != nil {
		return nil, fmt.Errorf("model list from %s: %w", c.baseURL, err)
	}
	out := make([]ModelInfo, 0, len(list.Data))
	for _, d := range list.Data {
		if d.ID == "" {
			continue
		}
		m := ModelInfo{ID: d.ID, Name: d.Name, Context: d.ContextLength, HasParameters: d.Parameters != nil}
		m.Prompt, _ = strconv.ParseFloat(d.Pricing.Prompt, 64)
		m.Completion, _ = strconv.ParseFloat(d.Pricing.Completion, 64)
		m.Tools, m.Reasoning = slices.Contains(d.Parameters, "tools"), slices.Contains(d.Parameters, "reasoning")
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b ModelInfo) int { return cmpString(a.ID, b.ID) })
	return slices.CompactFunc(out, func(a, b ModelInfo) bool { return a.ID == b.ID }), nil
}

// Models returns the Catalog's ids.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	cat, err := c.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(cat))
	for i, m := range cat {
		ids[i] = m.ID
	}
	return ids, nil
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Key info (GET /key, OpenRouter, R-113)
// ---------------------------------------------------------------------------

// KeyInfo is what OpenRouter reports about the API key in use, in USD. Limit and Remaining are
// nil for a key without a limit. The key itself is never part of it (I-39).
type KeyInfo struct {
	Label                  string
	Limit, Remaining       *float64
	Usage                  float64
	Daily, Weekly, Monthly float64
}

// KeyInfo asks the server about the key in use: GET {base}/key. A server without the endpoint
// (anything but OpenRouter) answers with an error.
func (c *Client) KeyInfo(ctx context.Context) (KeyInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	resp, err := c.do(ctx, http.MethodGet, "/key", nil, "application/json")
	if err != nil {
		return KeyInfo{}, err
	}
	defer resp.Body.Close()
	var w struct {
		Data struct {
			Label     string   `json:"label"`
			Limit     *float64 `json:"limit"`
			Remaining *float64 `json:"limit_remaining"`
			Usage     float64  `json:"usage"`
			Daily     float64  `json:"usage_daily"`
			Weekly    float64  `json:"usage_weekly"`
			Monthly   float64  `json:"usage_monthly"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&w); err != nil {
		return KeyInfo{}, fmt.Errorf("key info from %s: %w", c.baseURL, err)
	}
	d := w.Data
	return KeyInfo{Label: d.Label, Limit: d.Limit, Remaining: d.Remaining, Usage: d.Usage,
		Daily: d.Daily, Weekly: d.Weekly, Monthly: d.Monthly}, nil
}
