package guard

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Loop guard (PLAN 2.10): stops an answer that started repeating itself
// ---------------------------------------------------------------------------

const (
	loopWindow    = 40  // completed lines looked at by the line rule
	loopRepeats   = 4   // copies of one line inside the window that trip the guard
	loopMinLine   = 8   // shortest normalized line (runes) the line rule counts
	loopTailRunes = 300 // the tail rule: the last N runes already seen twice before

	loopMinPeriod = 8   // the period rule: shortest repeating unit (bytes)
	loopMaxPeriod = 400 // longest repeating unit (bytes)
	loopCopies    = 3   // copies of the unit at the end of the current line that trip the guard
	loopMinSpan   = 160 // shortest repeating run (bytes): short units need more copies
	loopRunPeriod = 8   // a run of one or two symbols (a ruler "────") repeats with a period below this
)

// Loop watches the text of one model Step as it streams. The zero value is ready.
// Rules: (1) one normalized line of at least loopMinLine runes occurs loopRepeats times among
// the last loopWindow completed lines (lines inside a code fence are not counted: repeated
// code such as "if err != nil {" is normal); (2) the last loopTailRunes runes already occurred
// twice before, without overlapping the tail; (3) the current line ends with loopCopies copies of
// one unit (a phrase repeating inside one long line), outside a code fence.
type Loop struct {
	buf     []byte
	scanned int // bytes of buf already split into lines
	inFence bool
	lines   []guardLine // the last loopWindow counted lines
}

type guardLine struct {
	norm  string
	start int // byte offset of the line in buf
}

// Feed adds a streamed chunk. When the text repeats itself it returns the text cut before the
// repetition (the first copy is kept) and true.
func (g *Loop) Feed(chunk string) (cut string, tripped bool) {
	g.buf = append(g.buf, chunk...)
	for {
		i := bytes.IndexByte(g.buf[g.scanned:], '\n')
		if i < 0 {
			break
		}
		start := g.scanned
		g.scanned += i + 1
		if at, ok := g.addLine(string(g.buf[start:start+i]), start); ok {
			return g.cutAt(at), true
		}
	}
	if at, ok := g.tailRepeat(); ok {
		return g.cutAt(at), true
	}
	if at, ok := g.periodRepeat(); ok {
		return g.cutAt(at), true
	}
	return "", false
}

// periodRepeat applies the period rule; at is the start of the second copy of the unit.
func (g *Loop) periodRepeat() (at int, tripped bool) {
	if g.inFence {
		return 0, false
	}
	n := len(g.buf)
	ls := g.scanned // start of the current, unfinished line
	line := g.buf[ls:]
	if len(line) < loopMinSpan || periodic(line[len(line)-loopMinSpan:], 1, loopRunPeriod-1) {
		return 0, false
	}
	for p := loopMinPeriod; p <= loopMaxPeriod && loopCopies*p <= len(line); p++ {
		need := max(loopCopies*p, loopMinSpan)
		if need > len(line) || !bytes.Equal(g.buf[n-need+p:], g.buf[n-need:n-p]) {
			continue
		}
		s := n - need // walk back to the first copy
		for s-p >= ls && bytes.Equal(g.buf[s-p:s], g.buf[s:s+p]) {
			s -= p
		}
		at = s + p
		for at < n && !utf8.RuneStart(g.buf[at]) {
			at++
		}
		return at, true
	}
	return 0, false
}

// periodic reports whether b repeats with some period in [lo, hi] bytes.
func periodic(b []byte, lo, hi int) bool {
	for p := lo; p <= hi && p < len(b); p++ {
		if bytes.Equal(b[p:], b[:len(b)-p]) {
			return true
		}
	}
	return false
}

// addLine applies the line rule to one completed line; at is the start of the second copy.
func (g *Loop) addLine(line string, start int) (at int, tripped bool) {
	if strings.HasPrefix(strings.TrimSpace(line), "```") {
		g.inFence = !g.inFence
		return 0, false
	}
	norm := normalizeLine(line)
	if g.inFence || norm == "" {
		return 0, false
	}
	g.lines = append(g.lines, guardLine{norm, start})
	if len(g.lines) > loopWindow {
		g.lines = g.lines[len(g.lines)-loopWindow:]
	}
	if utf8.RuneCountInString(norm) < loopMinLine {
		return 0, false
	}
	var starts []int
	for _, l := range g.lines {
		if l.norm == norm {
			starts = append(starts, l.start)
		}
	}
	if len(starts) >= loopRepeats {
		return starts[1], true
	}
	return 0, false
}

// tailRepeat applies the tail rule; at is the start of the second earlier copy.
func (g *Loop) tailRepeat() (at int, tripped bool) {
	if len(g.buf) < 2*loopTailRunes {
		return 0, false
	}
	p := len(g.buf)
	for n := 0; n < loopTailRunes; n++ {
		_, size := utf8.DecodeLastRune(g.buf[:p])
		p -= size
	}
	tail, head := g.buf[p:], g.buf[:p]
	var found []int
	for from := 0; from+len(tail) <= len(head); {
		i := bytes.Index(head[from:], tail)
		if i < 0 {
			break
		}
		found = append(found, from+i)
		from += i + 1
	}
	if len(found) < 2 {
		return 0, false
	}
	return found[1], true
}

// cutAt returns the text before byte offset at, without trailing blanks.
func (g *Loop) cutAt(at int) string {
	return strings.TrimRight(string(g.buf[:at]), " \t\r\n")
}

// normalizeLine strips what varies between copies of a repeated list item: numbering, bullets,
// markdown marks, digits, case and runs of spaces.
func normalizeLine(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsDigit(r), r == '*', r == '_', r == '`', r == '#', r == '>':
			return -1
		}
		return unicode.ToLower(r)
	}, s)
	s = strings.TrimLeft(strings.Join(strings.Fields(s), " "), ".)-+•: ")
	return s
}
