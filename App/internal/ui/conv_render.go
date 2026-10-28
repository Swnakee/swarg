package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------------------
// Rendering one item to wrapped rows
// ---------------------------------------------------------------------------

const interruptedText = "interrupted"

// taskGutter prefixes every row of an item once per Task level it sits in; gw is its width.
const taskGutter = "  │ "

var gw = ansi.StringWidth(taskGutter)

// itemRows returns the cache of item i, rendering the item when the key
// (width, open, hidden, version, frame) changed. Every row is at most c.width cells wide; an item
// inside d Tasks is rendered gw*d cells narrower and gets d gutters. An item inside a closed
// Task block has no rows (its live assistant state is kept, nothing is rendered).
func (c *conv) itemRows(i int) *itemCache {
	it := c.items[i]
	frame := 0
	if it.kind == itemTool && !it.rec.done {
		frame = c.frame
	}
	ch, hidden := &it.cache, it.depth > 0 && c.hidden(i)
	if ch.ok && ch.width == c.width && ch.open == it.open && ch.hidden == hidden && ch.version == it.version && ch.frame == frame {
		return ch
	}
	ch.rows, ch.cont, ch.lead, ch.headRows = nil, nil, nil, 0
	if !hidden {
		c.renders++
		w := max(c.width-it.depth*gw, 1)
		switch it.kind {
		case itemUser:
			ch.rows = c.userRows(it, w)
		case itemNotice:
			ch.rows = c.noticeRows(it, w)
		case itemAssistant:
			ch.rows = c.assistantRows(it, w)
		case itemTool:
			ch.rows, ch.headRows = c.toolRows(it, w)
		}
		ch.rows, ch.cont, ch.lead = splitMarks(ch.rows)
		if it.depth > 0 {
			gut := outStyle.Render(strings.Repeat(taskGutter, it.depth))
			for r := range ch.rows {
				ch.rows[r], ch.lead[r] = gut+ch.rows[r], ch.lead[r]+it.depth*gw
			}
		}
	}
	ch.ok, ch.width, ch.open, ch.hidden, ch.version, ch.frame = true, c.width, it.open, hidden, it.version, frame
	return ch
}

// splitMarks returns a copy of rows without the marks, the continuation kind and the lead
// (cells of renderer margin dropped from a copy) of every row. The copy matters: a live
// assistant item keeps its marked block rows between versions.
func splitMarks(rows []string) ([]string, []byte, []int) {
	out, cont, lead := make([]string, len(rows)), make([]byte, len(rows)), make([]int, len(rows))
	for i, r := range rows {
		r, lead[i] = popLead(r)
		out[i], cont[i] = popMark(r)
	}
	return out, cont, lead
}

// userRows: the message after the user glyph, every row on the user background and padded
// to the full width.
func (c *conv) userRows(it *convItem, w int) []string {
	var rows []string
	for _, r := range wrapTextS(glyphUser+" "+Clean(it.text), w) {
		rows = append(rows, userStyle.Width(w).Render(r))
	}
	return rows
}

// noticeRows: dim text.
func (c *conv) noticeRows(it *convItem, w int) []string {
	var rows []string
	for _, r := range wrapTextS(Clean(it.text), w) {
		rows = append(rows, noticeStyle.Render(r))
	}
	return rows
}

// toolRows: the block of a tool call. Output is wrapped, not cut; an open block shows all the
// kept output, a closed one the same few lines as the inline view.
func (c *conv) toolRows(it *convItem, w int) (rows []string, headRows int) {
	r := *it.rec
	lines := blockLines(r, it.open)
	vis := naturalVis(r, it.open)
	if it.open {
		vis = len(lines)
	}
	return blockRowsAt(r, it.open, vis, lines, w, c.frame)
}

// assistantRows: a live item shows its committed blocks (rendered once per width, kept
// between versions) and the open tail; an ended item is rendered once as a whole from the
// raw text. Rendered markdown can still be wider than the width (code, tables), so rows are wrapped.
func (c *conv) assistantRows(it *convItem, w int) []string {
	if !it.live {
		var rows []string
		if strings.TrimSpace(it.text) != "" {
			rows = splitRowsS(renderMarkdown(Clean(it.text), w), w)
		}
		if it.interrupted {
			rows = append(rows, noticeStyle.Render(interruptedText))
		}
		return rows
	}
	ch := &it.cache
	if ch.blkWidth != w || ch.blkN > len(it.blocks) {
		ch.blkWidth, ch.blkN, ch.blkRows = w, 0, nil
	}
	for ; ch.blkN < len(it.blocks); ch.blkN++ {
		ch.blkRows = append(ch.blkRows, splitRowsS(renderMarkdown(Clean(it.blocks[ch.blkN]), w), w)...)
	}
	rows := ch.blkRows[:len(ch.blkRows):len(ch.blkRows)]
	if tail := it.cm.Tail(); strings.TrimSpace(tail) != "" {
		rows = append(rows, splitRowsS(renderMarkdown(Clean(tail), w), w)...)
	}
	return rows
}
