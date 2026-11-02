package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"swarg/internal/ui/mdstream"
)

// ---------------------------------------------------------------------------
// Wrapping: every row of the conversation list is at most `width` cells wide
// ---------------------------------------------------------------------------

const (
	tabSpaces = "    "
	resetSGR  = "\x1b[m"
)

// Soft-wrap marks. The marked wrappers (wrapRowS, wrapTextS, splitRowsS) put one of these
// at the start of every continuation row; conv strips them into itemCache.cont. Clean removes
// control characters (ESC included) from all text, so a mark (an APC sequence, zero width, kept by lipgloss) never collides with content.
const (
	markHard = "\x1b_H\x1b\\"    // the row continues the previous one cut at a cell boundary
	markWord = mdstream.MarkSoft // the row continues the previous one cut at a space (or a glamour wrap)
)

// Continuation kinds kept in itemCache.cont.
const (
	contNone byte = iota
	contHard
	contWord
)

// wrapRow hard-wraps one row (no newline inside) into rows of at most w cells. Wide runes
// and ANSI sequences are respected; a row that already fits is returned as is. A piece cut
// in the middle of styled text ends with a reset so the style never leaks past the row.
func wrapRow(s string, w int) []string { return wrapRowK(s, w, false) }

// wrapRowS is wrapRow with continuation rows marked (markHard).
func wrapRowS(s string, w int) []string { return wrapRowK(s, w, true) }

func wrapRowK(s string, w int, marks bool) []string {
	w = max(w, 1)
	if ansi.StringWidth(s) <= w {
		return []string{s}
	}
	parts := strings.Split(ansi.Hardwrap(s, w, true), "\n")
	for i, p := range parts[:len(parts)-1] {
		if strings.Contains(p, "\x1b[") {
			parts[i] = p + resetSGR
		}
	}
	if marks {
		for i := 1; i < len(parts); i++ {
			parts[i] = markHard + parts[i]
		}
	}
	return parts
}

func wrapRowsK(rows []string, w int, marks bool) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, wrapRowK(r, w, marks)...)
	}
	return out
}

// wrapText word-wraps text (it may hold newlines) to w cells and hard-wraps what is still
// too wide (a long unbroken word). Tabs become spaces. An empty line stays one empty row.
func wrapText(text string, w int) []string { return wrapTextK(text, w, false) }

// wrapTextS is wrapText with continuation rows marked (markWord, or markHard inside a long word).
func wrapTextS(text string, w int) []string { return wrapTextK(text, w, true) }

func wrapTextK(text string, w int, marks bool) []string {
	w = max(w, 1)
	text = strings.ReplaceAll(text, "\t", tabSpaces)
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		rest := ansi.Strip(line)
		for j, r := range strings.Split(ansi.Wrap(line, w, ""), "\n") {
			pieces := wrapRowK(r, w, marks)
			// Wrap drops the space at a break; the text skipped before this piece tells a
			// break at a space (word) from one inside a long word (hard).
			plain := ansi.Strip(r)
			at := strings.Index(rest, plain)
			if at < 0 || plain == "" {
				at = 0
			}
			if marks && j > 0 {
				mark := markHard
				if strings.Contains(rest[:at], " ") {
					mark = markWord
				}
				pieces[0] = mark + pieces[0]
			}
			rest = rest[min(at+len(plain), len(rest)):]
			out = append(out, pieces...)
		}
	}
	return out
}

// splitRowsS splits rendered multi-line text into rows and wraps them to w cells, marking continuation rows.
func splitRowsS(s string, w int) []string { return wrapRowsK(strings.Split(s, "\n"), w, true) }

// popMark removes a soft-wrap mark from row and returns the cleaned row and its kind.
func popMark(row string) (string, byte) {
	switch {
	case strings.Contains(row, markHard):
		return strings.Replace(row, markHard, "", 1), contHard
	case strings.Contains(row, markWord):
		return strings.Replace(row, markWord, "", 1), contWord
	}
	return row, contNone
}

// popLead removes a lead mark (mdstream.Lead: cells of renderer margin to drop from a copy)
// from row and returns the cleaned row and the cell count, 0 without a mark.
func popLead(row string) (string, int) {
	at := strings.Index(row, mdstream.LeadOpen)
	if at < 0 {
		return row, 0
	}
	end := strings.Index(row[at:], mdstream.LeadEnd)
	if end < 0 {
		return row, 0
	}
	n, _ := strconv.Atoi(row[at+len(mdstream.LeadOpen) : at+end])
	return row[:at] + row[at+end+len(mdstream.LeadEnd):], n
}
