package agent

import (
	"context"
	"errors"
	"strings"
)

// ---------------------------------------------------------------------------
// Source: the Provider in use and its request presets (R-100, I-36)
// ---------------------------------------------------------------------------

// Source is everything a request takes from the model server in use: the Provider (client), the
// model, the output and context limits, the request presets (Extra, the thinking overlay) and the
// server check. New builds the first one from Deps; SetSource replaces it at run time.
type Source struct {
	Name         string   // the provider name of the config (the SessionFile records it)
	Provider     Provider // required
	Model        string   // the model the Source starts with (its last used, else configured)
	MaxTokens    int
	ContextLimit int
	Extra        map[string]any
	ThinkExtra   map[string]any
	ThinkTokens  int
	LimitFor     func(model string) int          // nil = ContextLimit for every model (R-41)
	Health       func(ctx context.Context) error // nil = no server check after an Abort
}

// ErrBadSource is returned by SetSource for a Source without a name or a Provider.
var ErrBadSource = errors.New("agent: a source needs a name and a provider")

// Source returns the Source in use (a copy).
func (a *Agent) Source() Source {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.src
}

// SetSource switches the model server for every request built from now on: the next Step of a
// running Turn, the next Turn, Compaction summaries and the server check; a request in flight is
// never touched (I-36). The model becomes s.Model; both are recorded (Store.Provider, Store.Model,
// trace `provider`) and announced (EvProvider, EvModel, EvMeter). The ContextLimit is re-read for
// the new Source; when the history no longer fits it, a Compaction is queued ahead of everything
// else, so the first request to the new server fits (R-100). Safe from any goroutine.
func (a *Agent) SetSource(s Source) error {
	s.Model = strings.TrimSpace(s.Model)
	if s.Name == "" || s.Provider == nil {
		return ErrBadSource
	}
	if s.Model == "" {
		return ErrBlankModel
	}
	a.mu.Lock()
	a.src = s
	a.model = s.Model
	a.storeLocked(func(st Store) error { return st.Provider(s.Name) })
	a.storeLocked(func(st Store) error { return st.Model(s.Model) })
	a.traceLocked("provider", map[string]any{"provider": s.Name, "model": s.Model})
	a.setLimitLocked(s.Model)
	meter := a.meterSnapshot(&a.conversation)
	if meter.Used > a.meter.limit && !a.stopped && len(findSpans(a.history)) > 0 &&
		!(len(a.queue) > 0 && a.queue[0].control()) {
		a.queue = append([]qitem{{compact: &compactItem{}}}, a.queue...)
	}
	a.mu.Unlock()
	a.sync()
	a.emit(Event{Kind: EvProvider, Text: s.Name})
	a.emit(Event{Kind: EvModel, Text: s.Model})
	a.emit(meter)
	signal(a.wake)
	return nil
}

// provider is the Provider in use (locks).
func (a *Agent) provider() Provider {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.src.Provider
}

// health is the server check of the Source in use, nil when it has none (locks).
func (a *Agent) health() func(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.src.Health
}
