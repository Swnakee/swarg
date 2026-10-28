package agent

import (
	"errors"
	"strings"
)

// ---------------------------------------------------------------------------
// Model choice (R-30, I-9)
// ---------------------------------------------------------------------------

// ErrBlankModel is returned by SetModel for an empty or blank model id.
var ErrBlankModel = errors.New("agent: blank model id")

// Model returns the model id the next request will name.
func (a *Agent) Model() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.model
}

// SetModel chooses the model for every request built from now on: the next Step of a running
// Turn, the next Turn, Compaction summaries and the server check. The choice is recorded
// (Store.Model, trace `model`) and announced with EvModel. Choosing the model in use changes
// and records nothing. Safe to call at any time from any goroutine.
func (a *Agent) SetModel(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrBlankModel
	}
	a.mu.Lock()
	if name == a.model {
		a.mu.Unlock()
		return nil
	}
	a.model = name
	a.storeLocked(func(s Store) error { return s.Model(name) })
	a.traceLocked("model", map[string]any{"model": name})
	var meter Event
	changed := a.setLimitLocked(name) // R-41: the ContextLimit follows the model
	if changed {
		meter = a.meterSnapshot(&a.conversation)
	}
	a.mu.Unlock()
	a.sync()
	a.emit(Event{Kind: EvModel, Text: name})
	if changed {
		a.emit(meter)
	}
	return nil
}

// WithModel makes Load also set the Model choice (a resumed Session continues with its last
// model). The SessionFile already holds that choice, so nothing is recorded; EvModel follows the
// Load when the model changed. A blank name changes nothing.
func WithModel(name string) LoadOption {
	return func(l *loadItem) { l.model = strings.TrimSpace(name) }
}
