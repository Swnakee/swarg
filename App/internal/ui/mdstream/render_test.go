package mdstream

import (
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// plain strips ANSI sequences from rendered text.
func plain(s string) string { return stripANSI(s) }

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestRender_PlainFallback(t *testing.T) {
	got := plain(Render("```nosuchlang\nsecret_code(1)\n```\n", 80))
	if !strings.Contains(got, "secret_code(1)") {
		t.Fatalf("code text lost: %q", got)
	}
}

func TestRender_NeverPanics(t *testing.T) {
	cases := []struct {
		name  string
		block string
		width int
	}{
		{"nul bytes", "a\x00b\x00\n\n", 80},
		{"long line", strings.Repeat("x", 100*1024) + "\n", 80},
		{"unterminated fence", "```go\nfunc f() {\n", 80},
		{"width zero", "# T\n\ntext\n", 0},
		{"width one", "# T\n\nsome text here\n", 1},
		{"negative width", "text\n", -5},
		{"empty", "", 80},
		{"invalid utf8", "\xff\xfe text\n", 80},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { _ = Render(tc.block, tc.width) })
	}
}

func TestRender_ContentPreserved(t *testing.T) {
	in := "Это AI-агент, well-known --flag и дефис-слово.\n"
	got := plain(Render(in, 80))
	for _, w := range []string{"AI-агент", "well-known", "--flag", "дефис-слово", "Это"} {
		if !strings.Contains(got, w) {
			t.Errorf("%q missing in %q", w, got)
		}
	}
}

func TestRender_WidthWrap(t *testing.T) {
	in := strings.Repeat("слово word ", 40) + "\n"
	for _, w := range []int{20, 40, 80} {
		for _, l := range strings.Split(plain(Render(in, w)), "\n") {
			if n := utf8.RuneCountInString(l); n > w {
				t.Errorf("width %d: line has %d runes: %q", w, n, l)
			}
		}
	}
}

func TestRender_TrimsBlankEdges(t *testing.T) {
	for _, in := range []string{"\n\nhello\n\n\n", "# Title\n\nbody\n\n", "- a\n- b\n\n"} {
		got := Render(in, 80)
		lines := strings.Split(got, "\n")
		if strings.TrimSpace(plain(lines[0])) == "" || strings.TrimSpace(plain(lines[len(lines)-1])) == "" {
			t.Errorf("blank edge in %q", got)
		}
	}
	if got := plain(Render("\n\n", 80)); strings.TrimSpace(got) != "" {
		t.Errorf("blank block must render empty, got %q", got)
	}
}

func TestRender_SingleLines(t *testing.T) {
	if got := plain(Render("some **bold** and `code`\n", 80)); !strings.Contains(got, "bold") || strings.Contains(got, "**") {
		t.Errorf("inline style not applied: %q", got)
	}
	if got := Render("\n", 80); got != "" {
		t.Errorf("blank line block must render empty (UI prints one empty line), got %q", got)
	}
	if got := plain(Render("- one\n", 80)); !strings.Contains(got, "• one") && !strings.Contains(got, "- one") {
		t.Errorf("one-item list lost its marker: %q", got)
	}
	if got := plain(Render("# Title\n", 80)); !strings.Contains(got, "Title") || strings.Contains(got, "\n#") {
		t.Errorf("heading: %q", got)
	}
}

func TestRender_ConcurrentSafe(t *testing.T) {
	in := "# T\n\nsome **bold** `code`\n\n```go\nfunc f() {}\n```\n"
	want := Render(in, 60)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if got := Render(in, 60); got != want {
					t.Errorf("concurrent Render differs from the sequential result")
					return
				}
			}
		}()
	}
	wg.Wait()
}
