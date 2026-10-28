package agent

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// WorkSpans: what a Compaction may replace (PLAN 2.6, 5.3; Domain R-16)
//
// A WorkSpan is a maximal run of assistant Messages with ToolCalls and their ToolResults.
// User Messages (TurnStart and Injected), final answers (any assistant Message without
// ToolCalls) and earlier WorkSummaries are never inside a span: they end it, so they stay
// verbatim and an Injected Message splits the work of its Turn into two spans.
// ---------------------------------------------------------------------------

const (
	smallSpanTokens = 600 // below this a span gets the generated list, no model call
	minGainPercent  = 30  // a span is replaced only if the text saves at least this much
	summaryPrefix   = "[work summary] "

	taskClip   = 1000 // bytes of the Turn's user Message in the summary request
	resultClip = 500  // bytes of head and of tail of one ToolResult in the summary request
	noteClip   = 300  // bytes of the model's text between ToolCalls in the summary request
	listBrief  = 80   // bytes of a result line in the generated list when the tool gave no brief
)

// span is one WorkSpan: a copy of its Messages and the Seq range that addresses it.
type span struct {
	msgs           []chat.Message
	fromSeq, toSeq int
	task           string // the user Message that started the Turn the span belongs to
	steps          []string
	tokens         int    // estimated tokens the span costs in a request (Stubs count as sent)
	list           string // the generated list
}

// findSpans returns the compactable WorkSpans of h, oldest first. Runs whose calls and results
// do not pair up exactly in order (a repaired history) and Messages without a Seq are left alone.
func findSpans(h []chat.Message) (out []span) {
	for i := 0; i < len(h); {
		if !inSpan(h[i]) {
			i++
			continue
		}
		j := i
		for j < len(h) && inSpan(h[j]) {
			j++
		}
		if run := h[i:j]; closedRun(run) && run[0].Seq > 0 && run[len(run)-1].Seq > 0 {
			s := span{msgs: slices.Clone(run), fromSeq: run[0].Seq, toSeq: run[len(run)-1].Seq,
				task: taskOf(h, i)}
			s.steps = stepsOf(s.msgs)
			s.tokens = estimateRequest(chat.Request{Messages: s.msgs}).Total()
			s.list = generatedList(s.msgs)
			out = append(out, s)
		}
		i = j
	}
	return out
}

// inSpan reports whether a Message can belong to a WorkSpan.
func inSpan(m chat.Message) bool {
	return m.Role == chat.RoleTool || (m.Role == chat.RoleAssistant && len(m.ToolCalls) > 0)
}

// closedRun reports whether every assistant Message of the run is followed by exactly the
// results of its calls, in order, and no result is left over.
func closedRun(run []chat.Message) bool {
	for k := 0; k < len(run); {
		m := run[k]
		if m.Role != chat.RoleAssistant {
			return false
		}
		for n, c := range m.ToolCalls {
			if k+1+n >= len(run) || run[k+1+n].Role != chat.RoleTool || run[k+1+n].ToolCallID != c.ID {
				return false
			}
		}
		k += 1 + len(m.ToolCalls)
	}
	return true
}

// taskOf is the text of the TurnStart Message before h[i], or "".
func taskOf(h []chat.Message, i int) string {
	for k := i - 1; k >= 0; k-- {
		if h[k].Role == chat.RoleUser && h[k].TurnStart {
			return h[k].Content
		}
	}
	return ""
}

// indexOfSeq is the position of the Message with the given Seq, or -1.
func indexOfSeq(h []chat.Message, seq int) int {
	for i, m := range h {
		if m.Seq == seq {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// stepsOf renders the span for the summary request: the model's text between calls as
// "- note: ..." and every call as "- <tool> <label>: <brief>" followed by the head and tail
// of its result.
func stepsOf(msgs []chat.Message) (out []string) {
	for k := 0; k < len(msgs); k++ {
		m := msgs[k]
		if t := strings.TrimSpace(m.Content); t != "" {
			out = append(out, "- note: "+clipHead(t, noteClip))
		}
		for n := range m.ToolCalls {
			out = append(out, renderStep(msgs[k+1+n]))
		}
		k += len(m.ToolCalls)
	}
	return out
}

// callLine is "<tool> <label>: <brief>" of a ToolResult (label and brief may be missing).
func callLine(res chat.Message) string {
	line := res.Name
	if res.Label != "" {
		line += " " + res.Label
	}
	if b := briefOf(res); b != "" {
		line += ": " + b
	}
	return line
}

// briefOf is the one-line result of a ToolResult: its Brief, else the first line of the output.
func briefOf(res chat.Message) string {
	b := res.Brief
	if b == "" {
		b, _, _ = strings.Cut(strings.TrimSpace(res.Content), "\n")
	}
	if res.Failed && !strings.HasPrefix(b, "error") {
		b = "error: " + b
	}
	return clipHead(b, listBrief)
}

func renderStep(res chat.Message) string {
	line := "- " + callLine(res)
	if body := headTail(res.Content, resultClip); body != "" {
		line += "\n" + body
	}
	return line
}

// generatedList is the fallback WorkSummary text: one "<tool> <label>: <brief>" per ToolResult.
func generatedList(msgs []chat.Message) string {
	var parts []string
	for _, m := range msgs {
		if m.Role == chat.RoleTool {
			parts = append(parts, callLine(m))
		}
	}
	return strings.Join(parts, "; ")
}

// headTail returns s, or its first and last n bytes with a gap marker when it is longer than 2n.
func headTail(s string, n int) string {
	if len(s) <= 2*n {
		return s
	}
	return clipHead(s, n) + fmt.Sprintf("\n[... %d bytes cut ...]\n", len(s)-2*n) + clipTail(s, n)
}

// clipHead returns at most n bytes of s from the start, cut at a rune boundary.
func clipHead(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// clipTail returns at most n bytes of s from the end, cut at a rune boundary.
func clipTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}

// ---------------------------------------------------------------------------
// Gain
// ---------------------------------------------------------------------------

// gainOK reports whether a WorkSummary text saves at least minGainPercent of the span.
func gainOK(text string, spanTokens int) bool {
	return spanTokens > 0 && chat.EstimateTokens(text)*100 <= spanTokens*(100-minGainPercent)
}

// gainPercent is the share of the span's tokens the text saves, 0 when it saves nothing.
func gainPercent(text string, spanTokens int) int {
	if spanTokens <= 0 {
		return 0
	}
	return max(0, 100-chat.EstimateTokens(text)*100/spanTokens)
}
