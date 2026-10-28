package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------------------
// Copying rendered markdown: no margin, soft wraps joined, real breaks kept
// ---------------------------------------------------------------------------

const (
	mdPara1 = "lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua ut enim ad minim veniam quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur excepteur sint occaecat cupidatat non proident"
	mdItem1 = "first item lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua ut enim ad minim veniam quis nostrud exercitation ullamco"
	mdItem2 = "second item with a rather long text that has to wrap as well at the narrow width of forty cells"
)

// mdRig is a rig of the given width holding one finished assistant answer.
func mdRig(t *testing.T, width int, md string) *rig {
	r := newRig(t)
	r.send(tea.WindowSizeMsg{Width: width, Height: 60})
	r.m.cv.add(&convItem{kind: itemAssistant, text: md})
	return r
}

// copyAll drags over the whole first item and returns the copied text.
func (r *rig) copyAll() string {
	last := r.m.cv.height(0) - 1
	return r.drag(0, 0, r.m.cv.width-1, last).clip
}

func TestCopyMarkdown_WrappedParagraphJoins(t *testing.T) {
	for _, w := range []int{40, 120} {
		r := mdRig(t, w, mdPara1)
		if r.m.cv.height(0) < 2 {
			t.Fatalf("width %d: not wrapped", w)
		}
		if got := r.copyAll(); got != mdPara1 {
			t.Errorf("width %d: copied %q", w, got)
		}
	}
}

func TestCopyMarkdown_BulletListKeepsItemsAndJoinsWraps(t *testing.T) {
	for _, w := range []int{40, 120} {
		r := mdRig(t, w, "- "+mdItem1+"\n- "+mdItem2+"\n- short\n")
		want := "• " + mdItem1 + "\n• " + mdItem2 + "\n• short"
		if got := r.copyAll(); got != want {
			t.Errorf("width %d: copied %q\nwant %q", w, got, want)
		}
	}
}

func TestCopyMarkdown_NumberedListKeepsNumbers(t *testing.T) {
	r := mdRig(t, 40, "1. "+mdItem1+"\n2. "+mdItem2+"\n")
	want := "1. " + mdItem1 + "\n2. " + mdItem2
	if got := r.copyAll(); got != want {
		t.Errorf("copied %q\nwant %q", got, want)
	}
}

func TestCopyMarkdown_CodeBlockKeepsLinesAndIndentation(t *testing.T) {
	code := "func a() {\n\tif x {\n\t\ty(\"" + strings.Repeat("word ", 5) + "\")\n\t}\n}\n  indented"
	for _, w := range []int{40, 120} {
		r := mdRig(t, w, "```go\n"+code+"\n```\n")
		if got := r.copyAll(); got != code {
			t.Errorf("width %d: copied %q\nwant %q", w, got, code)
		}
	}
}

func TestCopyMarkdown_CodeBlockNeverJoinsLines(t *testing.T) {
	r := mdRig(t, 40, "```\none\ntwo\nthree\n```\n")
	if got := r.copyAll(); got != "one\ntwo\nthree" {
		t.Errorf("copied %q", got)
	}
}

func TestCopyMarkdown_HeadingAndParagraph(t *testing.T) {
	for _, w := range []int{40, 120} {
		r := mdRig(t, w, "## Heading\n\n"+mdPara1+"\n\nsecond paragraph\n")
		got := r.copyAll()
		want := "## Heading\n\n" + mdPara1 + "\n\nsecond paragraph"
		if got != want {
			t.Errorf("width %d: copied %q\nwant %q", w, got, want)
		}
	}
}

func TestCopyMarkdown_TableKeepsRows(t *testing.T) {
	for _, w := range []int{40, 120} {
		r := mdRig(t, w, "| name | value |\n|---|---|\n| alpha | 1 |\n| beta | 2 |\n")
		lines := strings.Split(r.copyAll(), "\n")
		if len(lines) != 4 {
			t.Fatalf("width %d: %d lines %q", w, len(lines), lines)
		}
		for i, want := range []string{"name │ value", "┼", "alpha │ 1", "beta │ 2"} {
			if got := strings.Join(strings.Fields(lines[i]), " "); !strings.Contains(got, want) {
				t.Errorf("width %d line %d: %q, want containing %q", w, i, lines[i], want)
			}
			if strings.HasPrefix(lines[i], "  ") {
				t.Errorf("width %d line %d keeps the margin: %q", w, i, lines[i])
			}
		}
	}
}

func TestCopyMarkdown_PartialSelectionInsideParagraph(t *testing.T) {
	for _, w := range []int{40, 120} {
		r := mdRig(t, w, mdPara1)
		rows := plainRows(r.m.cv.rows(0, r.m.cv.total()))
		if len(rows) < 3 {
			t.Fatalf("width %d: %d rows", w, len(rows))
		}
		// from cell 10 of row 0 to cell 5 of row 2
		got := r.drag(10, 0, 5, 2).clip
		want := strings.TrimRight(ansi.Cut(rows[0], 10, w), " ") + " " + strings.TrimSpace(ansi.Cut(rows[1], 0, w)) +
			" " + strings.TrimSpace(ansi.Cut(rows[2], 0, 6))
		if got != want {
			t.Errorf("width %d: copied %q\nwant %q", w, got, want)
		}
		if strings.Contains(got, "\n") || strings.Contains(got, "  ") {
			t.Errorf("width %d: breaks or double spaces in %q", w, got)
		}
	}
}

func TestCopyMarkdown_SelectionStartingInTheMarginLosesIt(t *testing.T) {
	r := mdRig(t, 40, "short line\n")
	if got := r.drag(0, 0, 39, 0).clip; got != "short line" {
		t.Errorf("copied %q", got)
	}
}

func TestCopyMarkdown_LiveItemCopiesTheSame(t *testing.T) {
	r := newRig(t)
	r.send(tea.WindowSizeMsg{Width: 40, Height: 60})
	i := r.m.cv.addAssistant()
	r.m.cv.feed(i, mdPara1+"\n\nsecond\n\n")
	if got := strings.TrimRight(r.copyAll(), "\n"); got != mdPara1+"\n\nsecond" {
		t.Errorf("copied %q", got)
	}
}
