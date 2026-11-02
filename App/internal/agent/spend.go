package agent

import (
	"fmt"
	"maps"
	"strings"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Spend (R-111, R-114, I-39)
// ---------------------------------------------------------------------------

// Spend is what the Session has cost so far in USD: the sum of the costs the servers reported,
// in total and per model id. A Source that reports no cost (a local server) adds nothing.
type Spend struct {
	Total   float64
	ByModel map[string]float64
}

// Tokens is what the Session's requests have used so far (R-131): the prompt and completion
// tokens the servers reported, summed over every request; Cached is the part of Prompt the
// server served from its cache.
type Tokens struct {
	Prompt, Cached, Completion int
}

// Total is the Session usage figure shown in the panel: prompt plus completion.
func (t Tokens) Total() int { return t.Prompt + t.Completion }

// spendState is the Session's spend and the state of its warning; guarded by Agent.mu.
type spendState struct {
	by     map[string]float64
	tok    Tokens
	warned bool // the warning for Deps.WarnUSD was given (or the loaded spend was already over it)
}

func (s *spendState) total() (t float64) {
	for _, v := range s.by {
		t += v
	}
	return t
}

// Spend returns the Session's spend so far (a copy).
func (a *Agent) Spend() Spend {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.spendLocked()
}

// Tokens returns the tokens the Session has used so far (R-131).
func (a *Agent) Tokens() Tokens {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.spend.tok
}

func (a *Agent) spendLocked() Spend {
	return Spend{Total: a.spend.total(), ByModel: maps.Clone(a.spend.by)}
}

// WithSpend makes Load also set the Session's spend per model (a resumed Session continues with
// the cost it already had; nil = none). The SessionFile already holds it, so nothing is recorded.
func WithSpend(byModel map[string]float64) LoadOption {
	return func(l *loadItem) { l.spend, l.setSpend = byModel, true }
}

// WithTokens makes Load also set the Session's token usage (a resumed Session continues with
// the tokens it already used). The SessionFile already holds it, so nothing is recorded.
func WithTokens(t Tokens) LoadOption {
	return func(l *loadItem) { l.tok, l.setTok = t, true }
}

// addUsage adds one response of model to the Session usage: its cost (R-111) and its tokens
// (R-131). Each is recorded (Store, trace `spend`), the new totals are announced with EvSpend
// (Spend = the cost total, Tokens = prompt + completion total) and, when the cost total first
// reaches Deps.WarnUSD, followed by one EvWarning. A cost that is not positive adds no cost and
// a blank model adds none either; tokens count whatever the model. Nothing here can stop or
// delay a Turn (I-39). Call it without mu.
func (a *Agent) addUsage(model string, u chat.Usage) {
	usd := u.Cost
	if !(usd > 0) || model == "" {
		usd = 0
	}
	tok := Tokens{Prompt: max(u.PromptTokens, 0), Cached: max(u.CachedTokens, 0), Completion: max(u.CompletionTokens, 0)}
	if usd == 0 && tok == (Tokens{}) {
		return
	}
	a.mu.Lock()
	if usd > 0 {
		if a.spend.by == nil {
			a.spend.by = map[string]float64{}
		}
		a.spend.by[model] += usd
		a.storeLocked(func(s Store) error { return s.Spend(model, usd) })
	}
	if tok != (Tokens{}) {
		a.spend.tok.Prompt += tok.Prompt
		a.spend.tok.Cached += tok.Cached
		a.spend.tok.Completion += tok.Completion
		a.storeLocked(func(s Store) error { return s.Usage(tok.Prompt, tok.Cached, tok.Completion) })
	}
	total := a.spend.total()
	all := a.spend.tok.Total()
	a.traceLocked("spend", map[string]any{"model": model, "usd": usd, "total": total, "tokens": all})
	warn := a.deps.WarnUSD > 0 && !a.spend.warned && total >= a.deps.WarnUSD
	if warn {
		a.spend.warned = true
	}
	a.mu.Unlock()
	a.sync()
	a.emit(Event{Kind: EvSpend, Spend: total, Tokens: all})
	if warn {
		a.emit(Event{Kind: EvWarning, Text: fmt.Sprintf("spend %s reached the warning level %s (cost.warn_usd); nothing is stopped", FormatUSD(total), FormatUSD(a.deps.WarnUSD))})
	}
}

// setSpendLocked installs the spend of a loaded Session; the warning is already given when the
// loaded total is over the level. Caller holds mu.
func (a *Agent) setSpendLocked(by map[string]float64) {
	a.spend = spendState{by: maps.Clone(by), tok: a.spend.tok}
	a.spend.warned = a.deps.WarnUSD > 0 && a.spend.total() >= a.deps.WarnUSD
}

// FormatUSD writes an amount for the panel and /cost: `$0.0012`, 4 decimals under a cent-tenth,
// 2 decimals from a dollar up ($12.50).
func FormatUSD(v float64) string {
	switch {
	case v >= 1:
		return fmt.Sprintf("$%.2f", v)
	case v >= 0.01:
		return fmt.Sprintf("$%.3f", v)
	}
	return fmt.Sprintf("$%.4f", v)
}

// FormatTokens writes a token count for the panel and /cost: plain digits below 1000, then `12.4k`,
// from a million up `1.2M`.
func FormatTokens(n int) string {
	if n >= 1_000_000 {
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	}
	return formatK(n)
}
