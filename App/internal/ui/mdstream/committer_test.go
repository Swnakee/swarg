package mdstream

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// run feeds chunks and returns committed blocks (after Flush) and the Tail seen before Flush.
func run(chunks ...string) (blocks []string, tail string) {
	c := New()
	for _, ch := range chunks {
		blocks = append(blocks, c.Write(ch)...)
	}
	tail = c.Tail()
	blocks = append(blocks, c.Flush()...)
	return blocks, tail
}

func splitAt(s string, cuts []int) []string {
	var out []string
	prev := 0
	for _, c := range cuts {
		out = append(out, s[prev:c])
		prev = c
	}
	return append(out, s[prev:])
}

// ---------------------------------------------------------------------------
// block table
// ---------------------------------------------------------------------------

func TestCommitter_Blocks(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantWrite []string // committed before Flush
		wantTail  string
		wantFlush []string
	}{
		{"paragraph", "hello\n\n", []string{"hello\n", "\n"}, "", nil},
		{"two paragraphs", "a\n\nb\n\n", []string{"a\n", "\n", "b\n", "\n"}, "", nil},
		{"heading", "# T\n\nbody\n\n", []string{"# T\n", "\n", "body\n", "\n"}, "", nil},
		{"list with blanks stays one block", "- a\n\n- b\n  more\n\npara\n",
			[]string{"- a\n\n- b\n  more\n\n", "para\n"}, "", nil},
		{"numbered list then paragraph", "1. a\n2. b\ntext\n",
			[]string{"1. a\n2. b\n", "text\n"}, "", nil},
		{"table", "| a | b |\n|---|---|\n| 1 | 2 |\n\nx\n\n",
			[]string{"| a | b |\n|---|---|\n| 1 | 2 |\n\n", "x\n", "\n"}, "", nil},
		{"fence with blank lines", "```\na\n\nb\n```\n\nz\n\n",
			[]string{"```\na\n\nb\n```\n", "\n", "z\n", "\n"}, "", nil},
		{"fence with language", "```go\nx := 1\n```\n\n", []string{"```go\nx := 1\n```\n", "\n"}, "", nil},
		{"closing fence trailing spaces", "```\na\n\n```   \n\nq\n\n",
			[]string{"```\na\n\n```   \n", "\n", "q\n", "\n"}, "", nil},
		{"tilde fence", "~~~\na\n\nb\n~~~\n\n", []string{"~~~\na\n\nb\n~~~\n", "\n"}, "", nil},
		{"four backtick fence holds three", "````\n```\n\n````\n\nq\n\n",
			[]string{"````\n```\n\n````\n", "\n", "q\n", "\n"}, "", nil},
		{"fence indented 3", "   ```\n   a\n\n   b\n   ```\n\n",
			[]string{"   ```\n   a\n\n   b\n   ```\n", "\n"}, "", nil},
		{"fence inside list item", "- item\n\n      ```\n      a\n\n      b\n      ```\n\nend\n",
			[]string{"- item\n\n      ```\n      a\n\n      b\n      ```\n\n", "end\n"}, "", nil},
		{"inline triple backticks are not a fence", "use ```x``` here\n\nnext\n\n",
			[]string{"use ```x``` here\n", "\n", "next\n", "\n"}, "", nil},
		{"unclosed fence", "```\na\n\nb\n", nil, "```\na\n\nb\n", []string{"```\na\n\nb\n"}},
		{"no final newline", "abc", nil, "abc", []string{"abc"}},
		{"crlf", "a\r\n\r\nb\r\n\r\n", []string{"a\r\n", "\r\n", "b\r\n", "\r\n"}, "", nil},
		{"hyphen forms are not lists", "--flag\n\n---\n\nwell-known\n\n",
			[]string{"--flag\n", "\n", "---\n", "\n", "well-known\n", "\n"}, "", nil},
		{"leading blank lines are own blocks", "\n\nx\n", []string{"\n", "\n", "x\n"}, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			got := c.Write(tc.in)
			if !reflect.DeepEqual(got, tc.wantWrite) {
				t.Errorf("Write = %q, want %q", got, tc.wantWrite)
			}
			if c.Tail() != tc.wantTail {
				t.Errorf("Tail = %q, want %q", c.Tail(), tc.wantTail)
			}
			if fl := c.Flush(); !reflect.DeepEqual(fl, tc.wantFlush) {
				t.Errorf("Flush = %q, want %q", fl, tc.wantFlush)
			}
		})
	}
}

func TestCommitter_LineLevel(t *testing.T) {
	c := New()
	steps := []struct {
		chunk string
		want  []string
	}{
		{"long ans", nil},
		{"wer line one\nline two", []string{"long answer line one\n"}},
		{"\n", []string{"line two\n"}},
		{"# Head\n", []string{"# Head\n"}},
		{"\n", []string{"\n"}},
		{"- a\n- b\n", nil},
		{"para\n", []string{"- a\n- b\n", "para\n"}},
	}
	for _, st := range steps {
		if got := c.Write(st.chunk); !reflect.DeepEqual(got, st.want) {
			t.Fatalf("Write(%q) = %q, want %q", st.chunk, got, st.want)
		}
	}
	if c.Tail() != "" {
		t.Fatalf("Tail = %q", c.Tail())
	}
}

func TestCommitter_FlushIdempotent(t *testing.T) {
	c := New()
	c.Write("abc")
	if len(c.Flush()) != 1 {
		t.Fatal("first Flush must return the rest")
	}
	if got := c.Flush(); got != nil {
		t.Fatalf("second Flush = %q, want nil", got)
	}
	if c.Tail() != "" {
		t.Fatalf("Tail after Flush = %q", c.Tail())
	}
}

// ---------------------------------------------------------------------------
// properties
// ---------------------------------------------------------------------------

var corpus = []string{
	"Simple prose.\n\nSecond paragraph here.\n\nthird",
	"Привет, мир! Это AI-агент.\n\nВторой абзац — тире и «кавычки».\n\n- пункт один\n- пункт два\n",
	"well-known --flag\n\n- item\n\n---\n\n* star\n+ plus\n1. one\n2) two\n",
	"a\r\n\r\nb\r\n- c\r\n\r\n",
	"text\n\n```go\nfunc f() {\n\n}\n```\n\nafter\n\n~~~\nx\n\ny\n~~~\n",
	"- a\n  - nested\n\n    more\n\n  - deeper\n- b\n\nend\n",
	"| a | b |\n|---|---|\n| 1 | 2 |\n\n````\n```\n````\n",
	"```\nunclosed\n\nstill",
	"",
	"\n\n\n",
}

func TestCommitter_ChunkSplitInvariance(t *testing.T) {
	for i, in := range corpus {
		want, _ := run(in)
		check := func(desc string, chunks []string) {
			got, _ := run(chunks...)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("corpus %d %s: blocks %q, want %q", i, desc, got, want)
			}
		}
		check("whole", []string{in})
		var bytewise []string
		for j := 0; j < len(in); j++ {
			bytewise = append(bytewise, in[j:j+1])
		}
		check("1-byte", bytewise)
		for k := 0; k <= len(in); k++ {
			check("2-way", []string{in[:k], in[k:]})
		}
		for s := int64(0); s < 200; s++ {
			rng := rand.New(rand.NewSource(s))
			var cuts []int
			for p := 0; p < len(in); {
				p += 1 + rng.Intn(8)
				if p < len(in) {
					cuts = append(cuts, p)
				}
			}
			t.Logf("corpus %d seed %d", i, s)
			check("random", splitAt(in, cuts))
		}
	}
}

func TestCommitter_CommittedPlusTailIsInput(t *testing.T) {
	for i, in := range corpus {
		c := New()
		var committed strings.Builder
		for j := 0; j < len(in); j++ {
			for _, b := range c.Write(in[j : j+1]) {
				committed.WriteString(b)
			}
			if got := committed.String() + c.Tail(); got != in[:j+1] {
				t.Fatalf("corpus %d prefix %d: %q != %q", i, j+1, got, in[:j+1])
			}
		}
		for _, b := range c.Flush() {
			committed.WriteString(b)
		}
		if committed.String() != in {
			t.Fatalf("corpus %d after Flush: %q != %q", i, committed.String(), in)
		}
	}
}

// ---------------------------------------------------------------------------
// A1: an unclosed fence is committed in segments
// ---------------------------------------------------------------------------

// fenceInput is an opener, n body lines "l<i>" and no closer.
func fenceInput(opener string, n int) (string, []string) {
	var body []string
	var b strings.Builder
	b.WriteString(opener + "\n")
	for i := 0; i < n; i++ {
		l := "l" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + "\n"
		body = append(body, l)
		b.WriteString(l)
	}
	return b.String(), body
}

// bodyOf returns the lines of a block that are not fence markers.
func bodyOf(block, marker string) []string {
	var out []string
	for _, l := range strings.SplitAfter(block, "\n") {
		if l != "" && !strings.HasPrefix(strings.TrimSpace(l), marker) {
			out = append(out, l)
		}
	}
	return out
}

func TestCommitter_UnclosedFenceCommitsInSegments(t *testing.T) {
	in, body := fenceInput("```", 100)
	c := New()
	var blocks []string
	blocks = append(blocks, c.Write(in)...)
	if len(blocks) != 5 {
		t.Fatalf("want 5 segments of 20 lines while streaming, got %d", len(blocks))
	}
	for i, b := range blocks {
		if !strings.HasPrefix(b, "```\n") || !strings.HasSuffix(b, "\n```\n") {
			t.Errorf("segment %d is not a closed fence: %q", i, b)
		}
		if n := len(bodyOf(b, "```")); n != 20 {
			t.Errorf("segment %d has %d body lines", i, n)
		}
	}
	if tail := c.Tail(); strings.TrimSpace(tail) != "" && tail != "```\n" {
		t.Errorf("tail = %q", tail)
	}
	blocks = append(blocks, c.Flush()...)
	var got []string
	for _, b := range blocks {
		got = append(got, bodyOf(b, "```")...)
	}
	if strings.Join(got, "") != strings.Join(body, "") {
		t.Fatal("body lines lost or reordered across segments")
	}
}

func TestCommitter_SegmentKeepsInfoStringAndIndent(t *testing.T) {
	in, _ := fenceInput("  ```go", 45)
	blocks, _ := run(in)
	if len(blocks) < 3 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	for i, b := range blocks[:2] {
		if !strings.HasPrefix(b, "  ```go\n") || !strings.HasSuffix(b, "\n  ```\n") {
			t.Errorf("segment %d: %q", i, b)
		}
	}
	if !strings.HasPrefix(blocks[2], "  ```go\n") {
		t.Errorf("rest should reopen: %q", blocks[2])
	}
}

func TestCommitter_SegmentedFenceCloserDoesNotMakeEmptyBlock(t *testing.T) {
	in, _ := fenceInput("```", 20)
	blocks, _ := run(in + "```\nafter\n")
	for _, b := range blocks {
		if strings.TrimSpace(strings.ReplaceAll(b, "`", "")) == "" {
			t.Errorf("empty code block %q in %q", b, blocks)
		}
	}
	if last := blocks[len(blocks)-1]; last != "after\n" {
		t.Errorf("last = %q", last)
	}
}

func TestCommitter_SegmentedChunkSplitsDoNotMatter(t *testing.T) {
	in, _ := fenceInput("~~~", 77)
	want, _ := run(in)
	for _, step := range []int{1, 3, 7, 50} {
		var chunks []string
		for i := 0; i < len(in); i += step {
			chunks = append(chunks, in[i:min(i+step, len(in))])
		}
		got, _ := run(chunks...)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d differs", step)
		}
	}
}

func TestCommitter_OpenFence(t *testing.T) {
	c := New()
	c.Write("text\n````go\ncode\n")
	if got := c.OpenFence(); got != "````" {
		t.Errorf("OpenFence = %q", got)
	}
	c.Write("````\n")
	if got := c.OpenFence(); got != "" {
		t.Errorf("after closer OpenFence = %q", got)
	}
}
