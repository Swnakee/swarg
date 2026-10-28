package ui

import (
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// Sanitizing model- and file-sourced text
// ---------------------------------------------------------------------------

// Clean strips ESC and the other C0/C1 control characters (and DEL) so text from the
// model or from files can never drive the terminal. Newline and tab are kept.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
