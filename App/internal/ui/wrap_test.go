package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// wantFit fails for every row wider than w.
func wantFit(t *testing.T, rows []string, w int) {
	t.Helper()
	for i, r := range rows {
		if got := ansi.StringWidth(r); got > w {
			t.Fatalf("width %d: row %d is %d cells wide: %q", w, i, got, ansi.Strip(r))
		}
		if strings.Contains(r, "\n") {
			t.Fatalf("width %d: row %d holds a newline", w, i)
		}
	}
}

// plainRows strips colours.
func plainRows(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Strip(r)
	}
	return out
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestWrap_LongUnbrokenWord(t *testing.T) {
	word := strings.Repeat("a", 200)
	for w := 10; w <= 120; w++ {
		rows := wrapText(word, w)
		wantFit(t, rows, w)
		if strings.Join(rows, "") != word {
			t.Fatalf("width %d: text changed", w)
		}
	}
}

func TestWrap_WideRunesAreNotSplit(t *testing.T) {
	cjk := strings.Repeat("漢字", 100)
	emoji := strings.Repeat("🙂a", 60)
	for w := 10; w <= 60; w++ {
		for _, s := range []string{cjk, emoji} {
			rows := wrapRow(s, w)
			wantFit(t, rows, w)
			if strings.Join(rows, "") != s {
				t.Fatalf("width %d: runes lost or split", w)
			}
		}
	}
}

func TestWrap_AnsiDoesNotShiftWidth(t *testing.T) {
	s := outStyle.Render(strings.Repeat("x", 150))
	rows := wrapRow(s, 40)
	wantFit(t, rows, 40)
	if len(rows) != 4 {
		t.Errorf("rows = %d, want 4", len(rows))
	}
	if !strings.HasSuffix(rows[0], resetSGR) {
		t.Errorf("a cut styled row must end with a reset: %q", rows[0])
	}
}

func TestWrap_TextKeepsEmptyLinesAndWidensTabs(t *testing.T) {
	rows := wrapText("a\n\n\tb", 20)
	if len(rows) != 3 || rows[1] != "" || rows[2] != "    b" {
		t.Errorf("rows = %q", rows)
	}
}

func TestWrap_RowCountDeterministic(t *testing.T) {
	c := newConv()
	c.addUser(strings.Repeat("word ", 80))
	c.addTool(callRec{n: 1, done: true, output: strings.Repeat("z", 500), call: shellCall})
	c.setWidth(33)
	a := c.total()
	c.setWidth(80)
	c.setWidth(33)
	if b := c.total(); a != b {
		t.Errorf("rows %d then %d at the same width", a, b)
	}
}

// mixedConv has every kind of content that can come out too wide.
func mixedConv() *conv {
	c := newConv()
	c.addUser("привет " + strings.Repeat("u", 150) + " 漢字漢字漢字 🙂🙂🙂")
	c.addNotice(strings.Repeat("n", 130))
	a := c.addAssistant()
	c.feed(a, "# Title "+strings.Repeat("T", 90)+"\n\n"+strings.Repeat("longword", 30)+"\n\n")
	c.feed(a, "| a | b |\n|---|---|\n| "+strings.Repeat("c", 100)+" | 漢字 |\n\n")
	c.feed(a, "```go\nx := \""+strings.Repeat("s", 160)+"\"\n```\n\nend of text\n")
	c.endAssistant(a, false)
	b := c.addAssistant()
	c.feed(b, "- item "+strings.Repeat("i", 140)+"\n```\n"+strings.Repeat("k", 170)+"\n")
	call := callRec{n: 1, call: shellCall, label: strings.Repeat("l", 120), done: true,
		brief: strings.Repeat("b", 140), output: strings.Repeat("o", 300) + "\n漢字" + strings.Repeat("字", 80) + "\n\x1b[31mred\x1b[0m\n"}
	t1 := c.addTool(call)
	c.setOpen(t1, true)
	c.addTool(call)
	c.addTool(callRec{n: 3, call: shellCall, label: "run", output: strings.Repeat("r", 200)})
	return c
}

func TestWrap_EveryRowFitsAtEveryWidth(t *testing.T) {
	for w := 10; w <= 120; w++ {
		c := mixedConv()
		c.setWidth(w)
		rows := c.rows(0, c.total())
		if len(rows) == 0 {
			t.Fatalf("width %d: no rows", w)
		}
		wantFit(t, rows, w)
	}
}
