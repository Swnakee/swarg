package tools

import (
	"strings"
	"testing"
)

func TestHTMLToText_Cases(t *testing.T) {
	for name, c := range map[string]struct{ in, want string }{
		"empty":           {"", ""},
		"plain text":      {"just text", "just text"},
		"broken markup":   {"<p>one<p>two<b>bold", "one\n\ntwobold"},
		"empty heading":   {"<h1></h1><p>x</p>", "x"},
		"entities":        {"<p>a &amp; b &lt;c&gt; &nbsp;d</p>", "a & b <c> d"},
		"inline no space": {"<p>foo<b>bar</b>baz</p>", "foobarbaz"},
		"only script":     {"<script>alert(1)</script>", ""},
		"nested lists":    {"<ul><li>a<ul><li>b</li></ul></li></ul>", "- a\n\n- b"},
	} {
		got, err := htmlToText(strings.NewReader(c.in), "text/html", nil)
		if err != nil || got != strings.ReplaceAll(c.want, " ", " ") {
			t.Errorf("%s: got %q (%v), want %q", name, got, err, c.want)
		}
	}
}
