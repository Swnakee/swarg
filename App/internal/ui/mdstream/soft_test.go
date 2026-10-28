package mdstream

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
)

// unmark removes every RenderSoft mark and reports the soft rows and the leads.
func unmark(s string) (rows []string, soft []bool, lead []int) {
	for _, r := range strings.Split(s, "\n") {
		soft = append(soft, strings.Contains(r, MarkSoft))
		r = strings.Replace(r, MarkSoft, "", 1)
		n := 0
		if at := strings.Index(r, LeadOpen); at >= 0 {
			end := strings.Index(r[at:], LeadEnd)
			for _, c := range r[at+len(LeadOpen) : at+end] {
				n = n*10 + int(c-'0')
			}
			r = r[:at] + r[at+end+len(LeadEnd):]
		}
		rows, lead = append(rows, r), append(lead, n)
	}
	return
}

func TestRenderSoft_VisibleTextIsRender(t *testing.T) {
	md := "# T\n\n" + strings.Repeat("word ", 40) + "\n\n- a\n- b\n\n```go\nx := 1\n```\n\n> quote " + strings.Repeat("q ", 30) + "\n"
	rows, _, _ := unmark(RenderSoft(md, 40))
	if got := strings.Join(rows, "\n"); got != Render(md, 40) {
		t.Errorf("marks changed the text:\n%q\n%q", got, Render(md, 40))
	}
}

func TestRenderSoft_WrappedRowsAreSoftAndNewParagraphsAreNot(t *testing.T) {
	rows, soft, _ := unmark(RenderSoft(strings.Repeat("word ", 20)+"\n\nnext\n", 40))
	if !soft[1] || soft[0] {
		t.Errorf("soft %v rows %q", soft, rows)
	}
	if soft[len(soft)-1] {
		t.Error("a new paragraph is not a continuation")
	}
}

func TestRenderSoft_MarginAndCodeLeads(t *testing.T) {
	rows, _, lead := unmark(RenderSoft("text\n\n```\ncode\n  more\n```\n", 40))
	for i, r := range rows {
		switch {
		case strings.Contains(r, "text"):
			if lead[i] != docMargin {
				t.Errorf("text lead %d", lead[i])
			}
		case strings.Contains(r, "code"):
			if lead[i] != docMargin+codeMargin {
				t.Errorf("code lead %d", lead[i])
			}
		case strings.Contains(r, "more"):
			if lead[i] != docMargin+codeMargin {
				t.Errorf("indented code lead %d, the block's own indent must stay", lead[i])
			}
		}
	}
}

func TestRenderSoft_StyleMarginsAreTheConstants(t *testing.T) {
	s := GlamourStyle()
	if s.Document.Margin == nil || int(*s.Document.Margin) != docMargin {
		t.Errorf("document margin %v", s.Document.Margin)
	}
	if s.CodeBlock.Margin == nil || int(*s.CodeBlock.Margin) != codeMargin {
		t.Errorf("code block margin %v", s.CodeBlock.Margin)
	}
}

func TestRenderSoft_UnmatchableBlockMarksNothingSoft(t *testing.T) {
	md := "| a | b |\n|---|---|\n| " + strings.Repeat("long ", 30) + " | x |\n"
	_, soft, _ := unmark(RenderSoft(md, 40))
	for i, s := range soft {
		if s {
			t.Errorf("row %d soft in a wrapped table", i)
		}
	}
}

func TestRenderSoft_NoMarksLeakIntoPlainText(t *testing.T) {
	if got := xansi.Strip(RenderSoft("hi\n", 40)); strings.ContainsRune(got, 0x1b) {
		t.Errorf("escape left in %q", got)
	}
}
