package agent

import "maps"

// ---------------------------------------------------------------------------
// Thinking mode (R-60, I-30)
// ---------------------------------------------------------------------------

// Thinking reports whether the next request asks the model to think.
func (a *Agent) Thinking() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.thinking
}

// SetThinking switches the thinking mode for every request built from now on (the next Step of
// a running Turn, the next Turn). The choice is recorded (Store, trace `thinking`) and announced
// with EvThinking; setting the mode already in use changes and records nothing. Safe to call at
// any time from any goroutine.
func (a *Agent) SetThinking(on bool) {
	a.mu.Lock()
	if on == a.thinking {
		a.mu.Unlock()
		return
	}
	a.thinking = on
	a.storeLocked(func(s Store) error { return s.Thinking(on) })
	a.traceLocked("thinking", map[string]any{"on": on})
	a.mu.Unlock()
	a.sync()
	a.emit(thinkEvent(on))
}

func thinkEvent(on bool) Event {
	if on {
		return Event{Kind: EvThinking, Text: "on"}
	}
	return Event{Kind: EvThinking, Text: "off"}
}

// mergeExtra returns base with over laid on top, a new map. Nested maps are merged one level
// (chat_template_kwargs keeps its other keys); base and over are not changed.
func mergeExtra(base, over map[string]any) map[string]any {
	out := maps.Clone(base)
	if out == nil {
		out = map[string]any{}
	}
	for k, v := range over {
		ov, ok1 := v.(map[string]any)
		bv, ok2 := out[k].(map[string]any)
		if ok1 && ok2 {
			m := maps.Clone(bv)
			maps.Copy(m, ov)
			v = m
		}
		out[k] = v
	}
	return out
}
