package mdstream

import (
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
)

// ---------------------------------------------------------------------------
// constants
// ---------------------------------------------------------------------------

const defaultWidth = 80

// ---------------------------------------------------------------------------
// public API
// ---------------------------------------------------------------------------

// Render renders one completed markdown block to ANSI text for a terminal width columns
// wide (width <= 0 means 80). The style is GlamourStyle (the project palette) on purpose: no
// terminal queries, which would race with the UI's stdin reader, and never the stock "dark"
// style, whose Chroma colours would be registered first for the whole process. Leading and trailing blank lines are
// trimmed so consecutive blocks join with exactly one blank line. Any error or panic yields
// the plain block instead.
func Render(block string, width int) (out string) {
	defer func() {
		if recover() != nil {
			out = block
		}
	}()
	return renderWith(block, width, GlamourStyle())
}

// renderWith is Render with an explicit style; an error yields the plain block.
func renderWith(block string, width int, style ansi.StyleConfig) string {
	if width <= 0 {
		width = defaultWidth
	}
	r, err := glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(width))
	if err != nil {
		return block
	}
	s, err := r.Render(block)
	if err != nil {
		return block
	}
	return trimBlank(s)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// trimBlank drops leading and trailing lines that are empty or whitespace-only (Glamour pads
// blank lines with styled spaces, so ANSI codes are ignored when judging a line).
func trimBlank(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && isBlankANSI(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && isBlankANSI(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// isBlankANSI reports whether a line has no visible characters.
func isBlankANSI(l string) bool {
	return strings.TrimSpace(stripANSI(l)) == ""
}

// stripANSI removes CSI sequences (ESC [ ... final byte).
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
