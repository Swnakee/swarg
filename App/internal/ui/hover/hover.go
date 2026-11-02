// Package hover tracks the pointer and the clickable spans of the last draw.
package hover

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// ---------------------------------------------------------------------------
// Hover: the pointer position and the clickable spans of the last draw
//
// Every clickable element registers its screen cells with Add while the screen is drawn; Add
// says whether the pointer is over it, and Apply underlines every registered span under
// the pointer afterwards, so an element gets the hover look by registering alone. A pointer
// Move changes nothing but the look (I-10). The spots of the last draw also serve click
// dispatch (At): what the user clicks is what the last draw showed.
// ---------------------------------------------------------------------------

// SGR matches one SGR escape sequence.
var SGR = regexp.MustCompile("\x1b\\[([0-9;:]*)m")

const (
	UnderlineOn  = "\x1b[4m"
	UnderlineOff = "\x1b[24m"
)

// Spot is one clickable span of screen cells [x0, x1) on screen row y.
type Spot struct {
	ID     string
	Y      int
	X0, X1 int
}

// Hover is the pointer and the hotspots registered during the last draw.
type Hover struct {
	x, y  int
	on    bool // a pointer position is known
	spots []Spot
}

// Move records the pointer cell (a motion event without a button, or a press).
func (h *Hover) Move(x, y int) { h.x, h.y, h.on = x, y, true }

// Begin forgets the spots of the previous draw; call it when a draw starts.
func (h *Hover) Begin() { h.spots = h.spots[:0] }

// Add registers a clickable span and reports whether the pointer is over it. An empty span
// registers nothing.
func (h *Hover) Add(id string, y, x0, x1 int) bool {
	if x1 <= x0 {
		return false
	}
	h.spots = append(h.spots, Spot{id, y, x0, x1})
	return h.Over(y, x0, x1)
}

// Over reports whether the pointer is inside the span.
func (h *Hover) Over(y, x0, x1 int) bool {
	return h.on && h.y == y && h.x >= x0 && h.x < x1
}

// At returns the id of the registered spot under screen cell (x, y); of overlapping spots the
// last registered wins.
func (h *Hover) At(x, y int) (string, bool) {
	for i := len(h.spots) - 1; i >= 0; i-- {
		if s := h.spots[i]; s.Y == y && x >= s.X0 && x < s.X1 {
			return s.ID, true
		}
	}
	return "", false
}

// CutSpan splits a styled row around cells [c0, c1): the part before, the span (widened to
// whole graphemes, trailing blank cells of the row left out) and the part after; ok is false
// when the span is empty. Shared by Span and reverseSpan.
func CutSpan(row string, c0, c1 int) (pre, mid, post string, ok bool) {
	plain := ansi.Strip(row)
	c1 = min(c1, ansi.StringWidth(strings.TrimRight(plain, " ")))
	from, to, _ := CellSpan(plain, c0, c1)
	if to <= from {
		return "", "", "", false
	}
	w := ansi.StringWidth(row)
	return ansi.Cut(row, 0, from), ansi.Cut(row, from, to), ansi.Cut(row, to, w), true
}

// Span underlines cells [c0, c1) of a styled row; its colours are kept, trailing blank
// cells are left alone. Every SGR sequence inside the span is followed by a fresh underline,
// so a reset in the middle of the span does not end it.
func Span(row string, c0, c1 int) string {
	pre, mid, post, ok := CutSpan(row, c0, c1)
	if !ok {
		return row
	}
	mid = SGR.ReplaceAllStringFunc(mid, func(s string) string { return s + UnderlineOn })
	return pre + UnderlineOn + mid + UnderlineOff + post
}

// Apply underlines the spot under the pointer and every other spot with the same id (an
// element spread over several rows, such as a wrapped head, is hovered as a whole); rows are
// the screen rows 0..
func (h *Hover) Apply(rows []string) {
	if !h.on {
		return
	}
	hot := map[string]bool{}
	for _, s := range h.spots {
		if h.Over(s.Y, s.X0, s.X1) {
			hot[s.ID] = true
		}
	}
	for _, s := range h.spots {
		if hot[s.ID] && s.Y >= 0 && s.Y < len(rows) {
			rows[s.Y] = Span(rows[s.Y], s.X0, s.X1)
		}
	}
}

// CellSpan returns the part of plain covering cells [c0, c1), widened to whole graphemes (a
// wide character that is half inside counts whole), with the cell range it really covers.
func CellSpan(plain string, c0, c1 int) (from, to int, text string) {
	var b strings.Builder
	col, from, to := 0, -1, 0
	g := uniseg.NewGraphemes(plain)
	for g.Next() {
		w := g.Width()
		in := col+w > c0 && col < c1
		if w == 0 {
			in = col >= c0 && col < c1
		}
		if in {
			if from < 0 {
				from = col
			}
			to = col + w
			b.WriteString(g.Str())
		}
		col += w
	}
	if from < 0 {
		from = 0
	}
	return from, to, b.String()
}
