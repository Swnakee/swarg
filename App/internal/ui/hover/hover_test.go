package hover

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------------------
// The hover struct
// ---------------------------------------------------------------------------

func TestHover_AddReportsPointerAndAtFindsTheLastSpot(t *testing.T) {
	var h Hover
	if h.Add("a", 0, 0, 5) {
		t.Error("over before any pointer position")
	}
	h.Move(3, 0)
	h.Begin()
	if !h.Add("a", 0, 0, 5) {
		t.Error("add must report the pointer over the span")
	}
	if h.Add("b", 0, 5, 9) || h.Add("c", 1, 0, 5) {
		t.Error("spans beside or below the pointer reported as hovered")
	}
	if h.Add("empty", 0, 3, 3) {
		t.Error("an empty span registered")
	}
	h.Add("top", 0, 2, 4) // overlaps "a": the last registered wins
	if id, ok := h.At(3, 0); !ok || id != "top" {
		t.Errorf("at(3,0) = %q %v, want top", id, ok)
	}
	if id, ok := h.At(0, 0); !ok || id != "a" {
		t.Errorf("at(0,0) = %q %v, want a", id, ok)
	}
	if _, ok := h.At(7, 1); ok {
		t.Error("at found a spot where none is")
	}
	h.Begin()
	if _, ok := h.At(3, 0); ok {
		t.Error("begin must forget the spots")
	}
}

func TestHoverSpan_KeepsColoursAndTrailingPadding(t *testing.T) {
	row := "\x1b[31mabc\x1b[m def   "
	got := Span(row, 0, 1<<30)
	if ansi.Strip(got) != ansi.Strip(row) {
		t.Fatalf("text changed: %q", ansi.Strip(got))
	}
	_, tail, _ := strings.Cut(got, UnderlineOff)
	if ansi.Strip(tail) != "   " || strings.Contains(tail, UnderlineOn) {
		t.Errorf("trailing padding must stay outside the underline: %q", got)
	}
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("the colour was lost: %q", got)
	}
	if strings.Count(got, UnderlineOn) < 2 {
		t.Errorf("a reset inside the span must be followed by a fresh underline: %q", got)
	}
	if Span("abc", 5, 9) != "abc" {
		t.Error("a span past the text must leave the row alone")
	}
	part := Span("abcdef", 2, 4)
	if part != "ab"+UnderlineOn+"cd"+UnderlineOff+"ef" {
		t.Errorf("partial span: %q", part)
	}
}
