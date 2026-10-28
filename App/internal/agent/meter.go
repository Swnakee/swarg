package agent

import (
	"fmt"
	"strings"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Context meter
// ---------------------------------------------------------------------------

// meter tracks context usage and the 80% / 100% warnings. It is not
// goroutine-safe; the agent guards it with its mutex.
type meter struct {
	limit     int
	exact     int
	hasExact  bool
	warned80  bool
	warned100 bool

	cachePct int  // share of the last request's prompt the server took from its cache
	hasCache bool // a usage report has been seen
}

// setUsage records the exact usage reported by the server.
func (m *meter) setUsage(u chat.Usage) {
	m.exact = u.PromptTokens + u.CompletionTokens
	m.hasExact = true
	m.cachePct, m.hasCache = cachePercent(u.CachedTokens, u.PromptTokens), true
}

// cachePercent is cached/prompt in percent, clamped to 0..100; 0 for an empty prompt.
func cachePercent(cached, prompt int) int {
	if prompt <= 0 || cached <= 0 {
		return 0
	}
	return min(100, cached*100/prompt)
}

// reset forgets the exact value and re-arms the warnings (after Clear).
func (m *meter) reset() {
	*m = meter{limit: m.limit}
}

// value returns the last exact usage, or the estimate when there is none.
func (m *meter) value(estimate int) (used int, est bool) {
	if m.hasExact {
		return m.exact, false
	}
	return estimate, true
}

// check returns the warnings for one upward crossing of 80% and of 100%.
// A threshold is re-armed once the value drops below it.
func (m *meter) check(used int, est bool) []string {
	if m.limit <= 0 {
		return nil
	}
	var out []string
	over80, over100 := used*100 >= 80*m.limit, used >= m.limit
	text := FormatMeter(used, m.limit, est)
	if over80 && !m.warned80 {
		out = append(out, fmt.Sprintf("context 80%% full (%s)", text))
	}
	if over100 && !m.warned100 {
		out = append(out, fmt.Sprintf("context full (%s): use /clear", text))
	}
	m.warned80, m.warned100 = over80, over100
	return out
}

// estimateHistory estimates the prompt size: system prompt plus all messages (no tool schemas).
func estimateHistory(system string, history []chat.Message) int {
	msgs := append([]chat.Message{{Role: chat.RoleSystem, Content: system}}, history...)
	return estimateRequest(chat.Request{Messages: msgs}).Total()
}

// ---------------------------------------------------------------------------
// Breakdown
// ---------------------------------------------------------------------------

// Breakdown is the estimated size of a request in tokens by category. The parts add up to
// exactly chat.EstimateTokens of the whole request text (rounding is carried from part to part).
type Breakdown struct {
	System      int // system prompt and tool schemas
	User        int // user messages
	Answers     int // assistant text and the reasoning sent back
	CallArgs    int // tool call names and arguments
	ResultsFull int // tool results sent in full
	Stubs       int // tool results sent as a Stub
}

// Total is the sum of all categories.
func (b Breakdown) Total() int {
	return b.System + b.User + b.Answers + b.CallArgs + b.ResultsFull + b.Stubs
}

// estimateRequest sizes what a request carries. A tool message with Stub set counts as a stub
// (its Stub text), otherwise as a full result; tool schemas count with the system prompt.
func estimateRequest(req chat.Request) Breakdown {
	var system, user, answers, args, full, stubs int
	for _, m := range req.Messages {
		switch m.Role {
		case chat.RoleSystem:
			system += len(m.Content)
		case chat.RoleUser:
			user += len(m.Content)
		case chat.RoleAssistant:
			answers += len(m.Content) + len(m.Reasoning)
			for _, c := range m.ToolCalls {
				args += len(c.Name) + len(c.Args)
			}
		case chat.RoleTool:
			if m.Stub != "" {
				stubs += len(m.Stub)
			} else {
				full += len(m.Content)
			}
		}
	}
	for _, t := range req.Tools {
		system += len(t.Name) + len(t.Description) + len(t.Schema)
	}
	var b Breakdown
	cum, prev := 0, 0
	for _, p := range []struct {
		bytes int
		to    *int
	}{{system, &b.System}, {user, &b.User}, {answers, &b.Answers}, {args, &b.CallArgs}, {full, &b.ResultsFull}, {stubs, &b.Stubs}} {
		cum += p.bytes
		now := chat.EstimateBytes(cum)
		*p.to, prev = now-prev, now
	}
	return b
}

// FormatMeter renders the context meter, e.g. "35% · 5.6k/16k"; an estimated
// value gets a "~" on the percent and on the used count.
func FormatMeter(used, limit int, est bool) string {
	pct, tilde := 0, ""
	if limit > 0 {
		pct = used * 100 / limit
	}
	if est {
		tilde = "~"
	}
	return fmt.Sprintf("%s%d%% · %s%s/%s", tilde, pct, tilde, formatK(used), formatK(limit))
}

// formatK prints n as plain digits below 1000, else as k with one decimal
// (".0" dropped).
func formatK(n int) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
}

// FormatCache renders the prompt cache share, e.g. "cache 80%"; "cache –" before any usage.
func FormatCache(pct int, known bool) string {
	if !known {
		return "cache –"
	}
	return fmt.Sprintf("cache %d%%", pct)
}
