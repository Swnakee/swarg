package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/agent"
)

// ---------------------------------------------------------------------------
// Spend in the app (/cost, the Bottom panel item; R-112, R-113)
// ---------------------------------------------------------------------------

// KeyUsage is what a server reports about the API key in use, in USD (OpenRouter GET /key):
// nil Limit and Remaining mean a key without a limit. It never carries the key itself (I-39).
type KeyUsage struct {
	Label                  string
	Limit, Remaining       *float64
	Usage                  float64
	Daily, Weekly, Monthly float64
}

// ErrNoKeyInfo is returned by CostAPI.KeyUsage when the active Source has no key to ask about (a
// local server, a Source without a key endpoint): /cost then shows the Session spend only.
var ErrNoKeyInfo = errors.New("no key info")

// CostAPI is what /cost needs besides the agent's own spend; main adapts the active Source's
// client. Deps.Cost may be nil: /cost then shows the Session spend only.
type CostAPI interface {
	KeyUsage(ctx context.Context) (KeyUsage, error)
}

// spender is the part of the agent /cost reads (the test fakes lack it).
type spender interface {
	Spend() agent.Spend
}

// costMsg carries the key usage (or the error) back into Update.
type costMsg struct {
	ku  KeyUsage
	err error
}

// costCmd is /cost: the Session spend at once, then the key's usage when the Source can tell.
func (m *model) costCmd() tea.Cmd {
	var sp agent.Spend
	if s, ok := m.d.Agent.(spender); ok {
		sp = s.Spend()
	}
	m.say(spendText(sp))
	if m.d.Cost == nil {
		return nil
	}
	api, ctx := m.d.Cost, m.ctx
	return func() tea.Msg {
		ku, err := api.KeyUsage(ctx)
		return costMsg{ku: ku, err: err}
	}
}

// onCost shows the key usage; a Source without key info says nothing more.
func (m *model) onCost(msg costMsg) {
	switch {
	case errors.Is(msg.err, ErrNoKeyInfo):
	case msg.err != nil:
		m.say("key usage: " + firstLine(msg.err.Error()))
	default:
		m.say(keyText(msg.ku))
	}
}

// spendText is the Session part of /cost: the total and the models, the dearest first.
func spendText(sp agent.Spend) string {
	if sp.Total <= 0 {
		return "Session spend: " + agent.FormatUSD(0) + " (no paid response yet)"
	}
	models := make([]string, 0, len(sp.ByModel))
	for k := range sp.ByModel {
		models = append(models, k)
	}
	slices.SortFunc(models, func(a, b string) int {
		if sp.ByModel[a] != sp.ByModel[b] {
			if sp.ByModel[a] > sp.ByModel[b] {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})
	var b strings.Builder
	fmt.Fprintf(&b, "Session spend: %s\n", agent.FormatUSD(sp.Total))
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, k := range models {
		fmt.Fprintf(w, "  %s\t%s\n", k, agent.FormatUSD(sp.ByModel[k]))
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

// keyText is the key part of /cost: usage today, this week and month, and the limit.
func keyText(k KeyUsage) string {
	head := "Key"
	if k.Label != "" {
		head += " " + k.Label
	}
	limit := "no limit"
	if k.Limit != nil {
		limit = "limit " + agent.FormatUSD(*k.Limit)
		if k.Remaining != nil {
			limit += ", remaining " + agent.FormatUSD(*k.Remaining)
		}
	}
	return fmt.Sprintf("%s: today %s, week %s, month %s, all time %s; %s", head,
		agent.FormatUSD(k.Daily), agent.FormatUSD(k.Weekly), agent.FormatUSD(k.Monthly), agent.FormatUSD(k.Usage), limit)
}
