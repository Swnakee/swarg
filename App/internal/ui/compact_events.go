package ui

import (
	"fmt"

	"swarg/internal/agent"
)

// ---------------------------------------------------------------------------
// Compaction events (the only file that names agent.EvCompactStarted / EvCompacted)
// ---------------------------------------------------------------------------

// compactLine is the one-line text of a compaction event, ok=false for any other event.
// Shared by the full-screen UI (as a notice) and plain mode.
func compactLine(ev agent.Event) (line string, ok bool) {
	switch ev.Kind {
	case agent.EvCompactStarted:
		return "[compacting…]", true
	case agent.EvCompacted:
		return fmt.Sprintf("[compacted %d spans: ~%d → ~%d tokens]", ev.Spans, ev.Before, ev.After), true
	}
	return "", false
}

// compactEvent handles a compaction event of the full-screen UI. It reports whether a
// pending quit completes now. Other event kinds are ignored.
func (m *model) compactEvent(ev agent.Event) (quit bool) {
	switch ev.Kind {
	case agent.EvCompactStarted:
		m.compacting, m.stopping, m.escCount = true, false, 0
		m.started, m.now = m.clock(), m.clock()
		m.ensureTick()
	case agent.EvCompacted:
		m.compacting, m.stopping, m.escCount = false, false, 0
		m.say(fmt.Sprintf("compacted %d spans: ~%d → ~%d tokens", ev.Spans, ev.Before, ev.After))
		return m.quitting && !m.busy
	}
	return false
}
