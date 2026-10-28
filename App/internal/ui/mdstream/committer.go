// Package mdstream splits a streamed markdown text into blocks that can be rendered once
// they are complete (Committer) and renders one block for the terminal (Render).
package mdstream

import "strings"

// ---------------------------------------------------------------------------
// types
// ---------------------------------------------------------------------------

type kind int

const (
	kindNone  kind = iota // only blank lines so far
	kindText              // fence (plain text lines commit at once)
	kindList              // list run
	kindTable             // table run
)

// Committer turns streamed chunks into completed markdown blocks.
//
// A block is a run of complete lines, each with its own terminator. Plain text lines
// (paragraph, heading, any other non-list non-table line) and blank lines are committed one
// line at a time, so a long answer shows up while it streams. Lists, tables and fences are
// held until complete: a list ends before the first non-item, non-indented line (blank lines
// inside stay), a table at a blank or non-"|" line (the blank is included), a fence at its
// closer, but every segmentLines body lines the fence is committed as a closed block (closer
// added) and reopened with the same opener line. Those synthetic marker lines are the only
// text in blocks that is not in the input. Known limits: setext headings and multi-line blockquotes render line by line.
// Blocks are an exact partition of the input: concatenating all committed blocks and Tail()
// gives back everything written so far, byte for byte. A blank-only block renders as "",
// which the UI prints as one empty line.
type Committer struct {
	buf     string // complete lines of the open block
	partial string // text after the last "\n"
	kind    kind
	fence   string // opening fence marker while inside a fence, else ""
	opener  string // the opening fence line (with "\n"), reused when a segment reopens
	body    int    // body lines in the open fence segment
	cut     bool   // the open fence was already committed in segments
}

// segmentLines is how many body lines an open fence collects before it is committed as a
// closed segment, so a long code block (or an answer whose fence never closes) shows up
// while it streams.
const segmentLines = 20

// New returns an empty Committer.
func New() *Committer { return &Committer{} }

// ---------------------------------------------------------------------------
// public API
// ---------------------------------------------------------------------------

// Write adds a chunk and returns the blocks that became complete. Only complete lines are
// classified, so a chunk may end anywhere, even inside a UTF-8 rune.
func (c *Committer) Write(chunk string) []string {
	c.partial += chunk
	var out []string
	for {
		i := strings.IndexByte(c.partial, '\n')
		if i < 0 {
			return out
		}
		line := c.partial[:i+1]
		c.partial = c.partial[i+1:]
		out = c.line(line, out)
	}
}

// Tail returns the open text: the unfinished block plus the incomplete last line.
func (c *Committer) Tail() string {
	if c.cut && c.body == 0 { // only the synthetic reopener: nothing of the input
		return c.partial
	}
	return c.buf + c.partial
}

// OpenFence returns the fence marker (``` or ~~~, 3+ characters) of the fence the text is
// inside right now, "" when there is none. The UI uses it to close the fence of an
// interrupted answer.
func (c *Committer) OpenFence() string { return c.fence }

// Flush returns the rest at turn end (an unclosed fence is committed as is) and resets the
// Committer. A second Flush returns nil.
func (c *Committer) Flush() []string {
	rest := c.Tail()
	if c.cut && c.body == 0 && c.partial == "" {
		rest = ""
	}
	*c = Committer{}
	if rest == "" {
		return nil
	}
	return []string{rest}
}

// ---------------------------------------------------------------------------
// line classification
// ---------------------------------------------------------------------------

// line feeds one complete line and appends any block it completes to out.
func (c *Committer) line(l string, out []string) []string {
	text := strings.TrimRight(l, "\r\n")
	if c.fence != "" {
		return c.fenceLine(l, text, out)
	}
	blank := strings.TrimSpace(text) == ""
	switch c.kind {
	case kindList:
		if blank {
			c.buf += l
			return out
		}
		if !isItem(text) && !isIndented(text) {
			return c.line(l, c.commit(out))
		}
	case kindTable:
		if blank {
			c.buf += l
			return c.commit(out)
		}
		if !strings.HasPrefix(strings.TrimLeft(text, " "), "|") {
			return c.line(l, c.commit(out))
		}
	case kindNone:
		if !blank {
			c.kind = classify(text)
		}
	}
	c.buf += l
	if m := opensFence(text, c.kind == kindList); m != "" {
		c.fence, c.opener, c.body, c.cut = m, text+"\n", 0, false
	} else if blank && c.kind == kindNone || c.kind == kindText {
		return c.commit(out) // plain text and blank lines commit line by line
	}
	return out
}

// fenceLine feeds one line inside a fence: the closer ends it, a body line may complete a segment.
func (c *Committer) fenceLine(l, text string, out []string) []string {
	if closesFence(text, c.fence) {
		if c.cut && c.body == 0 { // the closer right after a segment: nothing left to show
			c.buf, c.fence, c.kind, c.cut = "", "", kindNone, false
			return out
		}
		c.buf += l
		c.fence, c.cut = "", false
		if c.kind == kindText {
			return c.commit(out)
		}
		return out
	}
	c.buf += l
	if c.body++; c.body < segmentLines {
		return out
	}
	indent := c.opener[:len(c.opener)-len(strings.TrimLeft(c.opener, " \t"))]
	out = append(out, c.buf+indent+c.fence+"\n")
	c.buf, c.kind, c.body, c.cut = c.opener, kindText, 0, true
	return out
}

// commit moves the open block to out and starts a new one.
func (c *Committer) commit(out []string) []string {
	out = append(out, c.buf)
	c.buf, c.kind = "", kindNone
	return out
}

// classify picks the block kind from its first non-blank line.
func classify(text string) kind {
	switch {
	case isItem(text):
		return kindList
	case strings.HasPrefix(strings.TrimLeft(text, " "), "|"):
		return kindTable
	}
	return kindText
}

// isIndented reports whether a line starts with a space or tab.
func isIndented(text string) bool {
	return strings.HasPrefix(text, " ") || strings.HasPrefix(text, "\t")
}

// isItem reports whether a line is a list item: -, * or + followed by a space, or digits
// followed by "." or ")" and a space.
func isItem(text string) bool {
	t := strings.TrimLeft(text, " \t")
	if len(t) >= 2 && strings.IndexByte("-*+", t[0]) >= 0 && t[1] == ' ' {
		return true
	}
	n := 0
	for n < len(t) && t[n] >= '0' && t[n] <= '9' {
		n++
	}
	return n > 0 && len(t) > n+1 && (t[n] == '.' || t[n] == ')') && t[n+1] == ' '
}

// opensFence returns the fence marker (3+ identical ` or ~ characters) if the line opens a
// fence: up to 3 spaces of indent, or any indent inside a list. A backtick fence cannot
// have a backtick in its info string, so inline ```code``` never opens one.
func opensFence(text string, inList bool) string {
	t := strings.TrimLeft(text, " \t")
	if !inList && len(text)-len(t) > 3 {
		return ""
	}
	if t == "" || (t[0] != '`' && t[0] != '~') {
		return ""
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	if n < 3 || (t[0] == '`' && strings.Contains(t[n:], "`")) {
		return ""
	}
	return t[:n]
}

// closesFence reports whether a line is the closing fence for marker: the same character at
// least as many times, and only spaces otherwise.
func closesFence(text, marker string) bool {
	t := strings.TrimSpace(text)
	return len(t) >= len(marker) && strings.Trim(t, marker[:1]) == ""
}
