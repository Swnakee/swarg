package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// grep
// ---------------------------------------------------------------------------

func TestGrep_MatchesSortedByPath(t *testing.T) {
	env := newTestEnv(t, map[string]string{
		"b.txt": "Foo here\n",
		"a.go":  "package x\nfunc Foo()\n",
	})
	res, err := runTool(Grep{}, env, `{"pattern":"Foo"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "a.go:2: func Foo()\nb.txt:1: Foo here" || res.Brief != "2 matches" {
		t.Fatalf("output %q brief %q", res.Output, res.Brief)
	}
}

func TestGrep_GlobFilter(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a.go": "Foo\n", "b.txt": "Foo\n", "sub/c.go": "Foo\n"})
	res, err := runTool(Grep{}, env, `{"pattern":"Foo","glob":"*.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "a.go:1: Foo\nsub/c.go:1: Foo" {
		t.Fatalf("output %q", res.Output)
	}
}

func TestGrep_PathScopes(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a/x.txt": "Foo\n", "b/y.txt": "Foo\n"})
	res, err := runTool(Grep{}, env, `{"pattern":"Foo","path":"a"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "a/x.txt:1: Foo" {
		t.Fatalf("output %q", res.Output)
	}
}

func TestGrep_SkipsGitNodeModulesAndBinary(t *testing.T) {
	env := newTestEnv(t, map[string]string{
		".git/config":       "Foo\n",
		"node_modules/x.js": "Foo\n",
		"bin.dat":           "\x00Foo\n",
		"src/keep.txt":      "Foo\n",
	})
	res, err := runTool(Grep{}, env, `{"pattern":"Foo"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "src/keep.txt:1: Foo" {
		t.Fatalf("output %q", res.Output)
	}
}

func TestGrep_SkipsFilesOver2MB(t *testing.T) {
	big := "Foo" + strings.Repeat("x", 2<<20)
	env := newTestEnv(t, map[string]string{"big.txt": big, "small.txt": "Foo\n"})
	res, err := runTool(Grep{}, env, `{"pattern":"Foo"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "small.txt:1: Foo" {
		t.Fatalf("output %q", res.Output)
	}
}

func TestGrep_InvalidPattern(t *testing.T) {
	env := newTestEnv(t, nil)
	_, err := runTool(Grep{}, env, `{"pattern":"("}`)
	if err == nil || !strings.HasPrefix(err.Error(), "invalid pattern: ") {
		t.Fatalf("err = %v", err)
	}
}

func TestGrep_InvalidGlob(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a.txt": "Foo\n"})
	if _, err := runTool(Grep{}, env, `{"pattern":"Foo","glob":"["}`); err == nil {
		t.Fatal("bad glob: no error")
	}
}

func TestGrep_CapsAt60Matches(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 250; i++ {
		fmt.Fprintf(&b, "Foo %d\n", i)
	}
	env := newTestEnv(t, map[string]string{"many.txt": b.String()})
	res, err := runTool(Grep{}, env, `{"pattern":"Foo"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.Output, "many.txt:60: Foo 59\n[... more matches]") || res.Brief != "60 matches" {
		t.Fatalf("tail %q brief %q", res.Output[len(res.Output)-60:], res.Brief)
	}
}

func TestGrep_NoMatch(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a.txt": "bar\n"})
	res, err := runTool(Grep{}, env, `{"pattern":"Foo"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "no matches" || res.Brief != "0 matches" {
		t.Fatalf("output %q brief %q", res.Output, res.Brief)
	}
}

func TestGrep_LineCutAt160Bytes(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a.txt": "Foo" + strings.Repeat("x", 500) + "\n"})
	res, err := runTool(Grep{}, env, `{"pattern":"Foo"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimPrefix(res.Output, "a.txt:1: "); len(got) != 160 {
		t.Fatalf("shown line is %d bytes, want 160", len(got))
	}
}

func TestGrep_EscapeRejected(t *testing.T) {
	env := newTestEnv(t, nil)
	_, err := runTool(Grep{}, env, `{"pattern":"x","path":"../"}`)
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("err = %v", err)
	}
}

func TestGrep_HonoursCancel(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a.txt": "Foo\n"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Grep{}.Run(ctx, env, json.RawMessage(`{"pattern":"Foo"}`))
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestGrep_Summary(t *testing.T) {
	if got := (Grep{}).Summary(json.RawMessage(`{"pattern":"Foo","path":"src"}`)); got != "Foo in src" {
		t.Errorf("summary %q", got)
	}
	if got := (Grep{}).Summary(json.RawMessage(`{"pattern":"Foo"`)); got != "" {
		t.Errorf("bad JSON summary %q", got)
	}
}
