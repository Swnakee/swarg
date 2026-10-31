package agent

import (
	"fmt"
	"maps"
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

// spendState is the Session's spend and the state of its warning; guarded by Agent.mu.
type spendState struct {
	by     map[string]float64
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

func (a *Agent) spendLocked() Spend {
	return Spend{Total: a.spend.total(), ByModel: maps.Clone(a.spend.by)}
}

// WithSpend makes Load also set the Session's spend per model (a resumed Session continues with
// the cost it already had; nil = none). The SessionFile already holds it, so nothing is recorded.
func WithSpend(byModel map[string]float64) LoadOption {
	return func(l *loadItem) { l.spend, l.setSpend = byModel, true }
}

// addSpend adds the cost of one response of model: it is recorded (Store, trace `spend`),
// announced with EvSpend (Spend = the new total) and, when the total first reaches
// Deps.WarnUSD, followed by one EvWarning. A cost that is not positive is ignored. Nothing here
// can stop or delay a Turn (I-39). Call it without mu.
func (a *Agent) addSpend(model string, usd float64) {
	if !(usd > 0) || model == "" {
		return
	}
	a.mu.Lock()
	if a.spend.by == nil {
		a.spend.by = map[string]float64{}
	}
	a.spend.by[model] += usd
	total := a.spend.total()
	a.storeLocked(func(s Store) error { return s.Spend(model, usd) })
	a.traceLocked("spend", map[string]any{"model": model, "usd": usd, "total": total})
	warn := a.deps.WarnUSD > 0 && !a.spend.warned && total >= a.deps.WarnUSD
	if warn {
		a.spend.warned = true
	}
	a.mu.Unlock()
	a.sync()
	a.emit(Event{Kind: EvSpend, Spend: total})
	if warn {
		a.emit(Event{Kind: EvWarning, Text: fmt.Sprintf("spend %s reached the warning level %s (cost.warn_usd); nothing is stopped", FormatUSD(total), FormatUSD(a.deps.WarnUSD))})
	}
}

// setSpendLocked installs the spend of a loaded Session; the warning is already given when the
// loaded total is over the level. Caller holds mu.
func (a *Agent) setSpendLocked(by map[string]float64) {
	a.spend = spendState{by: maps.Clone(by)}
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
