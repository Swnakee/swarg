package agent

import (
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Generation speed of a Step (R-29)
// ---------------------------------------------------------------------------

// stepClock keeps the times of one Step's stream and its completion tokens.
type stepClock struct {
	start    time.Time // the request was sent
	first    time.Time // the first generated delta, text or reasoning (zero: none)
	lastProg time.Time // the last prompt-reading progress event (zero: none)
	done     time.Time // the final event (zero: the stream did not end normally)
	tokens   int       // completion tokens of the usage report
	usage    bool      // a usage report was seen
}

// note records the time of one stream event.
func (c *stepClock) note(ev chat.StreamEvent, now time.Time) {
	switch ev.Kind {
	case chat.EventDelta, chat.EventReasoning: // reasoning tokens count in usage, so the clock starts with them (R-43)
		if c.first.IsZero() && ev.Text != "" {
			c.first = now
		}
	case chat.EventProgress:
		c.lastProg = now
	case chat.EventUsage:
		c.tokens, c.usage = ev.Usage.CompletionTokens, true
	case chat.EventDone:
		c.done = now
	}
}

// speed is completion tokens per second from the first generated token to the end of the
// stream. Generation starts at the first delta of any kind (text or reasoning); a Step without one
// (only tool calls: mlx_lm.server sends them in one chunk at the END of the generation, so their
// arrival is no start) counts from the end of prompt reading, else from the request. ok is false
// without usage, without tokens, without a normal end, or with no time passed.
func (c stepClock) speed() (tps float64, d time.Duration, ok bool) {
	if !c.usage || c.tokens <= 0 || c.done.IsZero() {
		return 0, 0, false
	}
	from := c.start
	switch {
	case !c.first.IsZero():
		from = c.first
	case !c.lastProg.IsZero():
		from = c.lastProg
	}
	if d = c.done.Sub(from); d <= 0 {
		return 0, 0, false
	}
	return float64(c.tokens) / d.Seconds(), d, true
}

// reportSpeed emits EvSpeed (and a trace record) for a Step that ended normally. A Step that
// failed or was aborted has no speed.
func (a *Agent) reportSpeed(c *conversation, r *turnResult) {
	if r.aborted || r.looped || r.finish == chat.FinishError {
		return
	}
	tps, d, ok := r.clock.speed()
	if !ok {
		return
	}
	a.traceC(c, "speed", map[string]any{"tokens": r.clock.tokens, "ms": d.Milliseconds(), "tps": tps})
	a.emit(c.tag(Event{Kind: EvSpeed, Speed: tps}))
}
