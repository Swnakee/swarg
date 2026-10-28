package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------------------
// Hover: the pointer position and the clickable spans of the last draw
//
// Every clickable element registers its screen cells with add while the screen is drawn; add
// says whether the pointer is over it, and applyHover underlines every registered span under
// the pointer afterwards, so an element gets the hover look by registering alone. A pointer
// move changes nothing but the look (I-10). The spots of the last draw also serve click
// dispatch (at): what the user clicks is what the last draw showed.
// ---------------------------------------------------------------------------

const (
	underlineOn  = "\x1b[4m"
	underlineOff = "\x1b[24m"
)

// hotspot is one clickable span of screen cells [x0, x1) on screen row y.
type hotspot struct {
	id     string
	y      int
	x0, x1 int
}

// hover is the pointer and the hotspots registered during the last draw.
type hover struct {
	x, y  int
	on    bool // a pointer position is known
	spots []hotspot
}

// move records the pointer cell (a motion event without a button, or a press).
func (h *hover) move(x, y int) { h.x, h.y, h.on = x, y, true }

// begin forgets the spots of the previous draw; call it when a draw starts.
func (h *hover) begin() { h.spots = h.spots[:0] }

// add registers a clickable span and reports whether the pointer is over it. An empty span
// registers nothing.
func (h *hover) add(id string, y, x0, x1 int) bool {
	if x1 <= x0 {
		return false
	}
	h.spots = append(h.spots, hotspot{id, y, x0, x1})
	return h.over(y, x0, x1)
}

// over reports whether the pointer is inside the span.
func (h *hover) over(y, x0, x1 int) bool {
	return h.on && h.y == y && h.x >= x0 && h.x < x1
}

// at returns the id of the registered spot under screen cell (x, y); of overlapping spots the
// last registered wins.
func (h *hover) at(x, y int) (string, bool) {
	for i := len(h.spots) - 1; i >= 0; i-- {
		if s := h.spots[i]; s.y == y && x >= s.x0 && x < s.x1 {
			return s.id, true
		}
	}
	return "", false
}

// cutSpan splits a styled row around cells [c0, c1): the part before, the span (widened to
// whole graphemes, trailing blank cells of the row left out) and the part after; ok is false
// when the span is empty. Shared by hoverSpan and reverseSpan.
func cutSpan(row string, c0, c1 int) (pre, mid, post string, ok bool) {
	plain := ansi.Strip(row)
	c1 = min(c1, ansi.StringWidth(strings.TrimRight(plain, " ")))
	from, to, _ := cellSpan(plain, c0, c1)
	if to <= from {
		return "", "", "", false
	}
	w := ansi.StringWidth(row)
	return ansi.Cut(row, 0, from), ansi.Cut(row, from, to), ansi.Cut(row, to, w), true
}

// hoverSpan underlines cells [c0, c1) of a styled row; its colours are kept, trailing blank
// cells are left alone. Every SGR sequence inside the span is followed by a fresh underline,
// so a reset in the middle of the span does not end it.
func hoverSpan(row string, c0, c1 int) string {
	pre, mid, post, ok := cutSpan(row, c0, c1)
	if !ok {
		return row
	}
	mid = sgrRe.ReplaceAllStringFunc(mid, func(s string) string { return s + underlineOn })
	return pre + underlineOn + mid + underlineOff + post
}

// applyHover underlines the spot under the pointer and every other spot with the same id (an
// element spread over several rows, such as a wrapped head, is hovered as a whole); rows are
// the screen rows 0..
func applyHover(h *hover, rows []string) {
	if !h.on {
		return
	}
	hot := map[string]bool{}
	for _, s := range h.spots {
		if h.over(s.y, s.x0, s.x1) {
			hot[s.id] = true
		}
	}
	for _, s := range h.spots {
		if hot[s.id] && s.y >= 0 && s.y < len(rows) {
			rows[s.y] = hoverSpan(rows[s.y], s.x0, s.x1)
		}
	}
}
