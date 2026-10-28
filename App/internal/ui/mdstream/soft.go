package mdstream

import (
	"strconv"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------------------
// RenderSoft: Render plus zero-width marks that tell the ui how to copy each row
// ---------------------------------------------------------------------------

// Row marks. They are APC sequences (zero width, never printed): the ui strips them into its
// row cache. MarkSoft opens a row that glamour wrapped out of the previous one (the copy joins
// it with a space). A lead mark (see Lead) says how many leading cells of the row are renderer
// margin or padding, to be dropped from the copy.
const (
	MarkSoft = "\x1b_W\x1b\\"
	LeadOpen = "\x1b_L"
	LeadEnd  = "\x1b\\"
)

const (
	docMargin  = 2 // glamour Document margin of the project style (asserted by a test)
	codeMargin = 2 // glamour CodeBlock margin of the project style
)

// Lead is the mark for n leading cells of renderer margin; "" for n <= 0.
func Lead(n int) string {
	if n <= 0 {
		return ""
	}
	return LeadOpen + strconv.Itoa(n) + LeadEnd
}

// RenderSoft is Render with copy marks in front of the rows:
//   - a row glamour wrapped inside one paragraph, list item or heading gets MarkSoft and a
//     lead covering its indentation (and a quote bar), so a copy joins it to the row above;
//   - a code row gets a lead of the renderer's indentation of its block (the least leading
//     spaces of the block), so the copy keeps only the code's own indentation;
//   - any other text row gets a lead of the document margin.
//
// Soft wraps are found by rendering the block a second time wide enough to wrap nothing and
// matching rows to its logical lines; when they do not match (a table that wraps its cells)
// no row is marked soft. Code rows are the rows that move when the code block margin is
// removed. The visible text is exactly Render's.
func RenderSoft(block string, width int) (out string) {
	out = Render(block, width)
	defer func() {
		if recover() != nil {
			out = Render(block, width)
		}
	}()
	if width <= 0 {
		width = defaultWidth
	}
	rows := strings.Split(out, "\n")
	soft := softRows(block, width, rows)
	code := codeRows(block, width, rows)
	var b strings.Builder
	for i, row := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		plain := xansi.Strip(row)
		switch {
		case strings.TrimSpace(plain) == "":
		case soft[i]:
			b.WriteString(MarkSoft + Lead(contLead(plain)))
		case code[i] > 0:
			b.WriteString(Lead(code[i]))
		default:
			b.WriteString(Lead(min(spaces(plain), docMargin)))
		}
		b.WriteString(row)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// soft wraps
// ---------------------------------------------------------------------------

// softRows flags the rows of rows (Render's output at width) that continue the row above.
// All false when the rows cannot be matched to the unwrapped render.
func softRows(block string, width int, rows []string) []bool {
	soft := make([]bool, len(rows))
	if len(block)+8 <= width {
		return soft // nothing can wrap
	}
	wide := strings.Split(renderWith(block, len(block)+width+64, GlamourStyle()), "\n")
	i, j := 0, 0
	for {
		for i < len(rows) && isBlankANSI(rows[i]) {
			i++
		}
		for j < len(wide) && isBlankANSI(wide[j]) {
			j++
		}
		if i == len(rows) || j == len(wide) {
			if i == len(rows) && j == len(wide) {
				return soft
			}
			return make([]bool, len(rows))
		}
		want, acc := key(wide[j]), key(rows[i])
		i++
		for acc != want {
			if !strings.HasPrefix(want, acc) || i >= len(rows) || isBlankANSI(rows[i]) {
				return make([]bool, len(rows))
			}
			soft[i] = true
			acc += key(xansi.Strip(rows[i])[contLead(xansi.Strip(rows[i])):])
			i++
		}
		j++
	}
}

// key is the text of a row without whitespace: wraps move spaces, never letters.
func key(row string) string {
	return strings.Join(strings.Fields(xansi.Strip(row)), "")
}

// contLead is the number of cells (bytes of ASCII spaces plus quote bars) before the text of a
// wrapped row: its indentation and a blockquote bar.
func contLead(plain string) int {
	n := 0
	for _, r := range plain {
		if r != ' ' && r != '│' {
			break
		}
		n++
	}
	return n
}

// spaces counts the leading ASCII spaces of s.
func spaces(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// ---------------------------------------------------------------------------
// code rows
// ---------------------------------------------------------------------------

// codeRows returns for every row the renderer's indentation in cells when it is a code row,
// else 0. A run of code rows (blank rows between them included) shares the least leading
// spaces among them.
func codeRows(block string, width int, rows []string) []int {
	lead := make([]int, len(rows))
	if !hasCode(block) {
		return lead
	}
	style := GlamourStyle()
	zero := uint(0)
	style.CodeBlock.Margin = &zero
	alt := strings.Split(renderWith(block, width, style), "\n")
	if len(alt) != len(rows) {
		return lead
	}
	var run []int
	flush := func() {
		least := -1
		for _, i := range run {
			if n := spaces(xansi.Strip(rows[i])); least < 0 || n < least {
				least = n
			}
		}
		for _, i := range run {
			lead[i] = least
		}
		run = run[:0]
	}
	for i, row := range rows {
		plain := xansi.Strip(row)
		switch {
		case strings.TrimSpace(plain) == "":
		case plain != xansi.Strip(alt[i]) && spaces(plain) >= docMargin+codeMargin:
			run = append(run, i)
		default:
			flush()
		}
	}
	flush()
	return lead
}

// hasCode reports whether block may hold a code block (fenced or indented).
func hasCode(block string) bool {
	if strings.Contains(block, "```") || strings.Contains(block, "~~~") {
		return true
	}
	for _, l := range strings.Split(block, "\n") {
		if strings.HasPrefix(l, "    ") || strings.HasPrefix(l, "\t") {
			return true
		}
	}
	return false
}
