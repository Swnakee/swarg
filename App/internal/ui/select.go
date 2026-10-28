package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// ---------------------------------------------------------------------------
// Selection: content coordinates, highlight and the copied text
// ---------------------------------------------------------------------------

const (
	toEnd      = 1 << 30 // a column past any row
	reverseOn  = "\x1b[7m"
	reverseOff = "\x1b[27m"
	resultGut  = "  " + glyphResult + " " // the gutter of a result line
)

// selPoint is a cell of the list in content coordinates: the item, the row inside it and the
// cell column. It survives scrolling and changes of other items.
type selPoint struct{ item, row, col int }

func (p selPoint) less(q selPoint) bool {
	if p.item != q.item {
		return p.item < q.item
	}
	if p.row != q.row {
		return p.row < q.row
	}
	return p.col < q.col
}

// selection is the highlighted range from a (where the drag began) to b (where it is now).
type selection struct {
	active bool
	a, b   selPoint
}

// ordered returns the two ends, first one first.
func (s selection) ordered() (lo, hi selPoint) {
	if s.b.less(s.a) {
		return s.b, s.a
	}
	return s.a, s.b
}

// cellSpan returns the part of plain covering cells [c0, c1), widened to whole graphemes (a
// wide character that is half inside counts whole), with the cell range it really covers.
func cellSpan(plain string, c0, c1 int) (from, to int, text string) {
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

// reverseSpan puts cells [c0, c1) of a styled row in reverse video. Blank cells at the end of
// the row (padding) are left alone.
func reverseSpan(row string, c0, c1 int) string {
	pre, mid, post, ok := cutSpan(row, c0, c1)
	if !ok {
		return row
	}
	// The selected cells lose their own colours: reverse video of a coloured cell can put
	// dark text on the dark terminal; plain reverse uses the terminal's colours and stays readable.
	return pre + "\x1b[m" + reverseOn + ansi.Strip(mid) + reverseOff + post
}

// highlight reverses the selected cells in rows, which hold list rows y0, y0+1, ... The
// "new rows" marker row (skip) is never touched.
func (m *model) highlight(rows []string, y0 int, skip int) {
	if !m.sel.active {
		return
	}
	lo, hi := m.sel.ordered()
	if lo.item >= m.cv.count() {
		return
	}
	ylo := m.cv.top(lo.item) + lo.row
	yhi := ylo
	if hi.item < m.cv.count() {
		yhi = m.cv.top(hi.item) + hi.row
	}
	for i := range rows {
		y := y0 + i
		if i == skip || y < ylo || y > yhi {
			continue
		}
		c0, c1 := 0, toEnd
		if y == ylo {
			c0 = lo.col
		}
		if y == yhi {
			c1 = hi.col + 1
		}
		rows[i] = reverseSpan(rows[i], c0, c1)
	}
}

// rowText is the copied text of cells [c0, c1) of one row: the lead cells (Task gutters, renderer
// margin of a markdown row) and then the gutter of a tool block are dropped (the head glyph stays), ANSI is
// gone and trailing padding is trimmed.
func rowText(it *convItem, row string, c0, c1, lead int) string {
	plain, off := ansi.Strip(row), 0
	if lead > 0 {
		off = lead
		if strings.HasPrefix(plain, strings.Repeat(" ", lead)) {
			plain = plain[lead:] // spaces: a tab after them has no cell width, TruncateLeft would eat it
		} else {
			plain = ansi.TruncateLeft(plain, lead, "")
		}
	}
	if it.kind == itemTool {
		for _, g := range []string{outputPrefix, resultGut} {
			if strings.HasPrefix(plain, g) {
				plain, off = plain[len(g):], off+ansi.StringWidth(g)
				break
			}
		}
	}
	if c1 != toEnd {
		c1 -= off
	}
	if c1 <= 0 {
		return ""
	}
	_, _, txt := cellSpan(plain, max(c0-off, 0), c1)
	return strings.TrimRight(txt, " ")
}

// selectionText returns the text of the selection and its number of lines. Soft-wrapped
// continuation rows join the previous one without a newline (with a space when the wrap was
// at a space).
func (m *model) selectionText() (string, int) {
	if !m.sel.active {
		return "", 0
	}
	lo, hi := m.sel.ordered()
	var b strings.Builder
	first := true
	for i := max(lo.item, 0); i <= hi.item && i < m.cv.count(); i++ {
		ic := m.cv.itemRows(i)
		r0, r1 := 0, len(ic.rows)-1
		if i == lo.item {
			r0 = lo.row
		}
		if i == hi.item {
			r1 = min(hi.row, r1)
		}
		for r := max(r0, 0); r <= r1; r++ {
			c0, c1 := 0, toEnd
			if i == lo.item && r == lo.row {
				c0 = lo.col
			}
			if i == hi.item && r == hi.row {
				c1 = hi.col + 1
			}
			if !first {
				switch ic.cont[r] {
				case contHard:
				case contWord:
					b.WriteString(" ")
				default:
					b.WriteString("\n")
				}
			}
			first = false
			b.WriteString(rowText(m.cv.items[i], ic.rows[r], c0, c1, ic.lead[r]))
		}
	}
	s := b.String()
	if strings.TrimSpace(s) == "" {
		return "", 0
	}
	return s, strings.Count(s, "\n") + 1
}
