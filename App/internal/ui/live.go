package ui

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"swarg/internal/agent"
)

// ---------------------------------------------------------------------------
// Tool block data: kept output, line rules, rows
// ---------------------------------------------------------------------------

const (
	liveRunLines    = 5         // output lines of a running, closed block
	previewLines    = 3         // output lines under a closed, done shell block
	maxLiveOutput   = 256 << 10 // bytes of output kept per block
	cancelledOnEnd  = "cancelled: turn ended"
	outputPrefix    = "  │ "
	moreLinesFormat = "  … %d more lines (/tool %d)"
	previewMore     = "  … +%d lines (/tool %d)"
	taskToolName    = "task" // the built-in Tool that starts a Task
)

// rec returns the call record numbered n, nil when it was dropped by the history cap.
// Records are in call order, so the search is a binary one.
func (m *model) rec(n int) *callRec {
	lo, hi := 0, len(m.calls)
	for lo < hi {
		mid := (lo + hi) / 2
		if m.calls[mid].n < n {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(m.calls) && m.calls[lo].n == n {
		return &m.calls[lo]
	}
	return nil
}

// toolOutput buffers a streamed chunk of the running call it belongs to; chunks for an
// unknown or finished call are dropped. syncOutputs moves the buffers into the records.
func (m *model) toolOutput(ev agent.Event) {
	for _, n := range m.runN {
		if r := m.rec(n); r != nil && r.call.ID == ev.Call.ID {
			b := append(m.outBuf[n], ev.Text...)
			if len(b) > 2*maxLiveOutput { // trim rarely: amortised linear
				b = append(b[:0], b[len(b)-maxLiveOutput:]...)
			}
			m.outBuf[n] = b
			return
		}
	}
}

// syncOutputs copies the buffered chunks into the call records and the list (once per batch).
func (m *model) syncOutputs() {
	for n, b := range m.outBuf {
		if r := m.rec(n); r != nil && !r.done {
			r.output = capTail(string(b), maxLiveOutput)
			m.cv.setTool(r.item, *r)
		}
	}
}

// capTail keeps the last max bytes of s, cut at a rune start.
func capTail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[len(s)-max:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// outputLines splits raw tool output into display lines: CRLF endings are dropped, a
// carriage-return progress line keeps its last state, controls are stripped and tabs widened.
func outputLines(raw string) []string {
	raw = strings.TrimSuffix(raw, "\n")
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	for i, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		if j := strings.LastIndexByte(l, '\r'); j >= 0 {
			l = l[j+1:]
		}
		lines[i] = strings.ReplaceAll(Clean(l), "\t", "    ")
	}
	return lines
}

// hasPreview says whether a block shows output lines in its closed, done state: only a
// successful shell call, whose output is what the user ran (file content stays one line).
func hasPreview(r callRec, open bool) bool {
	return r.done && !open && r.err == nil && r.call.Name == "shell"
}

// resultLeads says whether the result line sits right under the head: a done preview block and
// a done task block, whose children (the Task's work) follow it in the list.
func resultLeads(r callRec, open bool) bool {
	return hasPreview(r, open) || r.done && r.call.Name == taskToolName
}

// blockLines are the output lines a block works with. A closed done shell block drops the
// trailing status line ("[exit 0]", "[killed: ...]") and blank lines: the result line says it.
func blockLines(r callRec, open bool) []string {
	lines := outputLines(r.output)
	if !hasPreview(r, open) {
		return lines
	}
	if n := len(lines); n > 0 && isStatusLine(lines[n-1]) {
		lines = lines[:n-1]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// isStatusLine matches the line tools/shell.go appends: "[exit N]" or "[killed: why]".
func isStatusLine(l string) bool {
	return strings.HasPrefix(l, "[exit ") && strings.HasSuffix(l, "]") ||
		strings.HasPrefix(l, "[killed: ") && strings.HasSuffix(l, "]")
}

// naturalVis is how many output lines a closed block shows (an open one shows them all).
func naturalVis(r callRec, open bool) int {
	switch {
	case !r.done:
		return liveRunLines
	case hasPreview(r, open):
		return previewLines
	}
	return 0
}

// shape says how many output lines a block shows out of n when it may show vis, and how
// many it hides. A running block shows the tail of its output, a done one the head, and
// an open done block and a done preview name the hidden rest.
func shape(r callRec, open bool, vis, n int) (show, hidden int) {
	show = min(vis, n)
	if r.done && (open || hasPreview(r, open)) {
		hidden = n - show
	}
	return show, hidden
}

// blockRowsAt returns the rows of one block: head, output lines, a note for hidden lines
// and, once done, the result line (a done preview block puts it before the output). Long
// lines are hard-wrapped into more rows, continuation rows of an output line keep the output
// prefix. headRows is how many leading rows belong to the head line.
func blockRowsAt(r callRec, open bool, vis int, lines []string, width, frame int) (rows []string, headRows int) {
	add := func(style lipgloss.Style, line string) {
		for _, p := range wrapRowS(line, width) {
			rows = append(rows, style.Render(p))
		}
	}
	head, tail := ToolLine(r.n, r.call.Name, r.label, r.brief, r.err)
	if !r.done {
		head += " " + glyphFrames[frame%len(glyphFrames)]
	}
	add(headStyle, head)
	headRows = len(rows)
	result := func() {
		style := noticeStyle
		if r.err != nil {
			style = errStyle
		}
		add(style, tail)
	}
	if resultLeads(r, open) { // the result line leads, the output hangs under it
		result()
	}
	show, hidden := shape(r, open, vis, len(lines))
	shown := lines[:show]
	if !r.done {
		shown = lines[len(lines)-show:]
	}
	for _, l := range shown {
		pw := ansi.StringWidth(outputPrefix) // cells, not bytes: "│" is 3 bytes
		if width <= pw {
			add(outStyle, outputPrefix+l)
			continue
		}
		for _, p := range wrapRowS(l, width-pw) {
			rows = append(rows, outStyle.Render(outputPrefix+p))
		}
	}
	if hidden > 0 {
		format := moreLinesFormat
		if !open {
			format = previewMore
		}
		add(noticeStyle, fmt.Sprintf(format, hidden, r.n))
	}
	if r.done && !resultLeads(r, open) {
		result()
	}
	return rows, headRows
}
