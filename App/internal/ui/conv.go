package ui

import (
	"math"
	"sort"

	"swarg/internal/ui/mdstream"
)

// ---------------------------------------------------------------------------
// Conversation list: items, cached rows, heights
// ---------------------------------------------------------------------------

type itemKind int

const (
	itemUser itemKind = iota
	itemAssistant
	itemNotice
	itemTool
)

// convItem is one entry of the conversation. Only the fields of its kind are used.
type convItem struct {
	kind    itemKind
	text    string   // user / notice text; assistant: the raw markdown received so far
	rec     *callRec // tool: its own copy of the call record
	open    bool     // tool: expanded
	version int      // bumped on every change of the content
	depth   int      // Task nesting: 0 = the Session, d = inside d running Tasks (a gutter per level)
	parent  int      // list index of the Task block this item sits in, -1 = none

	live        bool // assistant: still streaming
	interrupted bool // assistant: ended by an interrupt
	cm          *mdstream.Committer
	blocks      []string // assistant, live: raw markdown blocks committed so far

	cache itemCache
}

// itemCache holds the rendered rows of an item under the key (width, open, hidden, version, frame).
type itemCache struct {
	ok       bool
	width    int
	open     bool
	hidden   bool // inside a closed Task block: no rows
	version  int
	frame    int // only for a running tool (its spinner), else 0
	rows     []string
	cont     []byte // per row: contNone, contHard or contWord (a soft-wrapped continuation)
	lead     []int  // per row: leading cells of renderer margin that a copy drops (markdown rows)
	headRows int    // tool: leading rows of the head line

	// live assistant: rows of the first blkN committed blocks at blkWidth, kept between versions
	blkWidth int
	blkN     int
	blkRows  []string
}

// conv is the conversation list. It knows nothing about Bubble Tea: the caller feeds it
// events, sets the width and asks for the rows of a window. Heights are exact and measured
// lazily: only the items up to the end of the asked window are rendered, once per width.
type conv struct {
	items   []*convItem
	width   int
	frame   int   // spinner frame, used by running tools
	pre     []int // pre[i] = rows above item i, valid for i < len(pre); pre[0] = 0
	renders int   // item renders so far (a counter for tests)
}

// renderMarkdown is mdstream.RenderSoft (Render plus copy marks); tests swap it to count renders.
var renderMarkdown = mdstream.RenderSoft

func newConv() *conv { return &conv{width: defaultCol, pre: []int{0}} }

// ---------------------------------------------------------------------------
// Changing the list
// ---------------------------------------------------------------------------

// add appends an item and returns its index.
func (c *conv) add(it *convItem) int {
	it.parent = -1
	c.items = append(c.items, it)
	return len(c.items) - 1
}

// nest puts item i inside the Task block parent, depth levels deep.
func (c *conv) nest(i, depth, parent int) {
	if it := c.at(i); it != nil {
		it.depth, it.parent = depth, parent
	}
}

// hidden says whether item i sits inside a closed Task block (any ancestor closed).
func (c *conv) hidden(i int) bool {
	for p := c.items[i].parent; p >= 0; p = c.items[p].parent {
		if !c.items[p].open {
			return true
		}
	}
	return false
}

// openAncestors opens the Task blocks item i sits in, so that i shows.
func (c *conv) openAncestors(i int) {
	for p := c.items[i].parent; p >= 0; p = c.items[p].parent {
		c.setOpen(p, true)
	}
}

// addUser appends a user message.
func (c *conv) addUser(text string) int { return c.add(&convItem{kind: itemUser, text: text}) }

// addNotice appends a dim notice line (it may hold newlines).
func (c *conv) addNotice(text string) int { return c.add(&convItem{kind: itemNotice, text: text}) }

// addTool appends a tool block; the list keeps its own copy of rec.
func (c *conv) addTool(rec callRec) int {
	r := rec
	return c.add(&convItem{kind: itemTool, rec: &r})
}

// addAssistant appends an empty, streaming assistant item.
func (c *conv) addAssistant() int {
	return c.add(&convItem{kind: itemAssistant, live: true, cm: mdstream.New()})
}

// feed adds streamed text to a live assistant item. The text must already be Clean.
func (c *conv) feed(i int, delta string) {
	it := c.at(i)
	if it == nil || it.kind != itemAssistant || !it.live || delta == "" {
		return
	}
	it.text += delta
	it.blocks = append(it.blocks, it.cm.Write(delta)...)
	c.bump(i)
}

// endAssistant finishes a streaming item. It is then rendered once as a whole from its raw
// text. After an interrupt an open fence is closed first and a row "interrupted" is added.
func (c *conv) endAssistant(i int, interrupted bool) {
	it := c.at(i)
	if it == nil || it.kind != itemAssistant || !it.live {
		return
	}
	if marker := it.cm.OpenFence(); marker != "" && interrupted {
		if it.text != "" && it.text[len(it.text)-1] != '\n' {
			it.text += "\n"
		}
		it.text += marker + "\n"
	}
	it.live, it.interrupted, it.cm, it.blocks = false, interrupted, nil, nil
	it.cache.blkRows, it.cache.blkN = nil, 0
	c.bump(i)
}

// setTool replaces the record of a tool item (output chunk, end of the call).
func (c *conv) setTool(i int, rec callRec) {
	if it := c.at(i); it != nil && it.kind == itemTool {
		r := rec
		it.rec = &r
		c.bump(i)
	}
}

// setOpen expands or collapses a tool item.
func (c *conv) setOpen(i int, open bool) {
	if it := c.at(i); it != nil && it.kind == itemTool && it.open != open {
		it.open = open
		c.invalidate(i)
	}
}

// toggle flips a tool item and returns its new state.
func (c *conv) toggle(i int) bool {
	it := c.at(i)
	if it == nil || it.kind != itemTool {
		return false
	}
	c.setOpen(i, !it.open)
	return it.open
}

// setWidth sets the wrap width; a change drops every cached row.
func (c *conv) setWidth(w int) {
	w = max(w, 1)
	if w == c.width {
		return
	}
	c.width = w
	c.pre = c.pre[:1]
	for _, it := range c.items {
		it.cache = itemCache{}
	}
}

// setFrame sets the spinner frame; only running tools depend on it.
func (c *conv) setFrame(f int) {
	if f == c.frame {
		return
	}
	c.frame = f
	for i, it := range c.items {
		if it.kind == itemTool && !it.rec.done {
			c.invalidate(i)
		}
	}
}

// reset empties the list.
func (c *conv) reset() {
	c.items, c.pre = nil, c.pre[:1]
}

// ---------------------------------------------------------------------------
// Reading the list
// ---------------------------------------------------------------------------

// count is the number of items.
func (c *conv) count() int { return len(c.items) }

// at returns item i, nil when out of range.
func (c *conv) at(i int) *convItem {
	if i < 0 || i >= len(c.items) {
		return nil
	}
	return c.items[i]
}

// height is the number of rows of item i at the current width.
func (c *conv) height(i int) int {
	it := c.at(i)
	if it == nil {
		return 0
	}
	return len(c.itemRows(i).rows)
}

// total is the number of rows of the whole list (measures every item that is not cached).
func (c *conv) total() int {
	c.extendRows(math.MaxInt)
	return c.pre[len(c.pre)-1]
}

// top is the first row of item i (measures the items above it).
func (c *conv) top(i int) int {
	if i < 0 || i >= len(c.items) {
		return c.total()
	}
	c.extendItems(i)
	return c.pre[i]
}

// rows returns the rows [offset, offset+h) of the list, fewer at the end. Only the items up
// to the end of the window are measured, only the visible ones are copied.
func (c *conv) rows(offset, h int) []string {
	offset = max(offset, 0)
	if h <= 0 {
		return nil
	}
	c.extendRows(offset + h)
	var out []string
	for i := c.itemAtRow(offset); i < len(c.items) && len(out) < h; i++ {
		rows := c.itemRows(i).rows
		start := 0
		if len(out) == 0 {
			start = offset - c.pre[i]
		}
		if start < len(rows) {
			out = append(out, rows[start:min(len(rows), start+h-len(out))]...)
		}
	}
	return out
}

// tailRows returns the last h rows of the list without measuring the items above them
// (the view that follows new text). first is the item of the first returned row and
// firstRow the row inside it; later rows follow in item order.
func (c *conv) tailRows(h int) (rows []string, first, firstRow int) {
	n, i := 0, len(c.items)
	for i > 0 && n < h {
		i--
		n += c.height(i)
	}
	first, firstRow = i, max(n-h, 0)
	for first < len(c.items) && firstRow >= c.height(first) {
		firstRow -= c.height(first)
		first++
	}
	for j := first; j < len(c.items); j++ {
		rows = append(rows, c.itemRows(j).rows...)
	}
	if firstRow < len(rows) {
		rows = rows[firstRow:]
	}
	return rows, first, firstRow
}

// rowAt maps a list row y to the item holding it, the row inside the item and whether it is
// a head row of a tool block (the row a click toggles). ok is false outside the list.
func (c *conv) rowAt(y int) (idx, row int, head, ok bool) {
	if y < 0 || y >= c.total() {
		return 0, 0, false, false
	}
	idx = c.itemAtRow(y)
	row = y - c.pre[idx]
	it := c.items[idx]
	return idx, row, it.kind == itemTool && row < it.cache.headRows, true
}

// ---------------------------------------------------------------------------
// Heights: lazy prefix sums
// ---------------------------------------------------------------------------

// extendRows measures items in order until the rows above the next item reach untilRow
// or every item is measured.
func (c *conv) extendRows(untilRow int) {
	for len(c.pre) <= len(c.items) && c.pre[len(c.pre)-1] < untilRow {
		c.measureNext()
	}
}

// extendItems measures items in order until pre[i] is known.
func (c *conv) extendItems(i int) {
	for len(c.pre) <= min(i, len(c.items)) {
		c.measureNext()
	}
}

// measureNext appends the prefix sum of the next unmeasured item.
func (c *conv) measureNext() {
	c.pre = append(c.pre, c.pre[len(c.pre)-1]+len(c.itemRows(len(c.pre)-1).rows))
}

// itemAtRow is the index of the item containing row y (pre must reach it; see extend).
func (c *conv) itemAtRow(y int) int {
	c.extendRows(y + 1)
	n := len(c.pre) - 1
	return sort.Search(n, func(i int) bool { return c.pre[i+1] > y })
}

// bump marks the content of item i changed.
func (c *conv) bump(i int) {
	c.items[i].version++
	c.invalidate(i)
}

// invalidate forgets the prefix sums from item i on.
func (c *conv) invalidate(i int) {
	if len(c.pre) > i+1 {
		c.pre = c.pre[:i+1]
	}
}
