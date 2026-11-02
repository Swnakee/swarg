package ui

import (
	"fmt"
	"slices"
	"strings"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Tool call lines (shared with plain mode)
// ---------------------------------------------------------------------------

const (
	maxCalls     = 2000 // call records kept for Ctrl+O and /tool N (the list keeps every block)
	errorPrefix  = "error: "
	noCallsHint  = "no tool calls yet"
	callNotFound = "no tool call #%d"
)

// Glyphs: the one place to retune the look of the UI.
const (
	glyphUser     = "▸"
	glyphCall     = "●"
	glyphResult   = "└"
	glyphDone     = "✔"
	glyphError    = "✘"
	glyphDenied   = "○"
	glyphSkipped  = "»"
	glyphCanceled = "◼"
	glyphAuto     = "◉"
)

// glyphFrames are the spinner frames (rotation).
var glyphFrames = []string{"◐", "◓", "◑", "◒"}

// statusGlyph picks the status mark for a result text the agent produced, "" for a normal result.
func statusGlyph(brief string) string {
	switch {
	case strings.HasPrefix(brief, "denied by the user"):
		return glyphDenied
	case strings.HasPrefix(brief, "skipped"):
		return glyphSkipped
	case strings.HasPrefix(brief, "cancelled"):
		return glyphCanceled
	}
	return ""
}

// ToolLine returns the two collapsed lines of a finished call: "⏺ name(label)  #N" and
// "  ⎿ brief" (or "  ⎿ error: <first line>" when err is set). Text is sanitized.
func ToolLine(n int, name, label, brief string, err error) (head, tail string) {
	head = fmt.Sprintf("%s %s(%s)  #%d", glyphCall, Clean(name), firstLine(Clean(label)), n)
	mark := ""
	if err != nil {
		brief, mark = errorPrefix+err.Error(), glyphError+" "
	} else if g := statusGlyph(brief); g != "" {
		mark = g + " "
	}
	return head, fmt.Sprintf("  %s %s%s", glyphResult, mark, firstLine(Clean(brief)))
}

// firstLine returns s up to its first newline.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// ---------------------------------------------------------------------------
// Call records
// ---------------------------------------------------------------------------

// callRec is one tool call of the session as the UI saw it. item is its index in the list.
type callRec struct {
	n, step      int
	call         chat.ToolCall
	label, brief string
	output       string
	err          error
	done         bool
	item         int
}

// ---------------------------------------------------------------------------
// Model side: recording calls, Ctrl+O, /tool N
// ---------------------------------------------------------------------------

// running returns the calls that have started and not ended.
func (m *model) running() []callRec {
	out := make([]callRec, 0, len(m.runN))
	for _, n := range m.runN {
		if r := m.rec(n); r != nil {
			out = append(out, *r)
		}
	}
	return out
}

// appendCall records r and keeps the newest maxCalls records.
func (m *model) appendCall(r callRec) {
	m.calls = append(m.calls, r)
	if len(m.calls) > maxCalls {
		m.calls = append(m.calls[:0], m.calls[len(m.calls)-maxCalls:]...)
	}
}

// toolStart records a call and adds its block to the list. The first start after all calls
// ended opens a new step; the model text before it is finished first.
func (m *model) toolStart(ev agent.Event) {
	m.endLive(false)
	if len(m.runN) == 0 {
		m.step++
	}
	m.nextN++
	r := callRec{n: m.nextN, step: m.step, call: ev.Call, label: ev.Label}
	r.item = m.place(m.cv.addTool(r))
	m.appendCall(r)
	m.runN = append(m.runN, r.n)
}

// toolEnd completes the matching call.
func (m *model) toolEnd(ev agent.Event) {
	r := m.runningByID(ev.Call.ID)
	if r == nil { // end without a start: keep the record anyway
		m.toolStart(ev)
		r = m.rec(m.nextN)
	}
	r.done, r.brief, r.output, r.err = true, ev.Brief, capTail(ev.Output, maxLiveOutput), ev.Err
	m.finishCall(r)
}

// runningByID returns the running call with this tool-call id.
func (m *model) runningByID(id string) *callRec {
	for _, n := range m.runN {
		if r := m.rec(n); r != nil && r.call.ID == id {
			return r
		}
	}
	return nil
}

// finishCall publishes a call that became done to the list and forgets its live state.
func (m *model) finishCall(r *callRec) {
	m.cv.setTool(r.item, *r)
	delete(m.outBuf, r.n)
	for i, n := range m.runN {
		if n == r.n {
			m.runN = append(m.runN[:i], m.runN[i+1:]...)
			break
		}
	}
}

// abandonCalls ends the calls still running when the Turn ends.
func (m *model) abandonCalls() {
	m.syncOutputs()
	for _, n := range append([]int(nil), m.runN...) {
		if r := m.rec(n); r != nil {
			r.done, r.brief = true, cancelledOnEnd
			m.finishCall(r)
		}
	}
	m.runN, m.tasks = nil, nil
}

// toggleLast is Ctrl+O: it flips the last block and scrolls to it when its head is off screen.
func (m *model) toggleLast() {
	if len(m.calls) == 0 {
		m.say(noCallsHint)
		return
	}
	r := m.calls[len(m.calls)-1]
	m.cv.openAncestors(r.item)
	m.cv.toggle(r.item)
	if y := m.cv.top(r.item); !m.rowVisible(y) {
		m.scrollTo(y)
	}
}

// showCall opens call #n and scrolls its head into view (/tool N).
func (m *model) showCall(n int) {
	r := m.rec(n)
	if r == nil {
		m.say(fmt.Sprintf(callNotFound, n))
		return
	}
	m.cv.openAncestors(r.item)
	m.cv.setOpen(r.item, true)
	m.scrollTo(m.cv.top(r.item))
}

// spinnerWork is the spinner text while tools run, "" when none do. The running Tasks come
// first, then the calls inside the innermost one: "Running task(fix tests) › shell(go test)…".
func (m *model) spinnerWork() string {
	var path []string
	var work []*callRec
	for _, n := range m.runN {
		r := m.rec(n)
		switch {
		case r == nil:
		case slices.Contains(m.tasks, r.item):
			path = append(path, callName(r))
		default:
			work = append(work, r)
		}
	}
	switch {
	case len(work) == 1:
		path = append(path, callName(work[0]))
	case len(work) > 1:
		path = append(path, fmt.Sprintf("%d tools", len(work)))
	case len(path) == 0 && len(m.runN) > 0: // records dropped by the cap
		path = append(path, fmt.Sprintf("%d tools", len(m.runN)))
	}
	if len(path) == 0 {
		return ""
	}
	return "Running " + strings.Join(path, " › ") + "…"
}

// callName is "name(label)" of a call, sanitized.
func callName(r *callRec) string {
	return fmt.Sprintf("%s(%s)", Clean(r.call.Name), firstLine(Clean(r.label)))
}
