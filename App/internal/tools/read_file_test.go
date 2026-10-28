package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers shared by the read-only tool tests
// ---------------------------------------------------------------------------

// newTestEnv creates a project folder in a temp dir with files (relative path -> body).
func newTestEnv(t *testing.T, files map[string]string) Env {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return Env{Root: root, Dir: dir}
}

// runTool runs t with a JSON args string.
func runTool(t Tool, env Env, args string) (Result, error) {
	return t.Run(context.Background(), env, json.RawMessage(args))
}

// ---------------------------------------------------------------------------
// read_file
// ---------------------------------------------------------------------------

func TestReadFile_NumbersLines(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a.txt": "a\nb\nc\n"})
	res, err := runTool(ReadFile{}, env, `{"path":"a.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "1: a\n2: b\n3: c" || res.Brief != "3 lines" {
		t.Fatalf("output %q brief %q", res.Output, res.Brief)
	}
}

func TestReadFile_OffsetAndLimit(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a.txt": "l1\nl2\nl3\nl4\nl5\n"})
	res, err := runTool(ReadFile{}, env, `{"path":"a.txt","offset":2,"limit":2}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "2: l2\n3: l3\n[... 2 more lines; use offset 4]"
	if res.Output != want || res.Brief != "2 lines" {
		t.Fatalf("output %q brief %q", res.Output, res.Brief)
	}
}

func TestReadFile_OffsetPastEOF(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a.txt": "1\n2\n3\n"})
	_, err := runTool(ReadFile{}, env, `{"path":"a.txt","offset":4}`)
	if err == nil || err.Error() != "offset 4 is past the end of the file (3 lines)" {
		t.Fatalf("err = %v", err)
	}
}

func TestReadFile_NonPositiveOffsetOrLimit(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a.txt": "1\n"})
	for _, args := range []string{
		`{"path":"a.txt","offset":0}`,
		`{"path":"a.txt","limit":0}`,
		`{"path":"a.txt","limit":-1}`,
	} {
		if _, err := runTool(ReadFile{}, env, args); err == nil {
			t.Errorf("%s: no error", args)
		}
	}
}

func TestReadFile_Default200Lines(t *testing.T) {
	numbered := func(n int) string {
		var b strings.Builder
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&b, "line%d\n", i)
		}
		return b.String()
	}
	env := newTestEnv(t, map[string]string{"big.txt": numbered(500), "exact.txt": numbered(200)})
	res, err := runTool(ReadFile{}, env, `{"path":"big.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Output, "1: line1\n") ||
		!strings.HasSuffix(res.Output, "200: line200\n[... 300 more lines; use offset 201]") || res.Brief != "200 lines" {
		t.Fatalf("tail %q brief %q", res.Output[max(0, len(res.Output)-80):], res.Brief)
	}
	res, err = runTool(ReadFile{}, env, `{"path":"exact.txt"}`)
	if err != nil || strings.Contains(res.Output, "[...") || res.Brief != "200 lines" {
		t.Fatalf("exactly 200 lines: %q %v", res.Output[max(0, len(res.Output)-60):], err)
	}
	res, err = runTool(ReadFile{}, env, `{"path":"big.txt","limit":450}`)
	if err != nil || !strings.Contains(res.Output, "450: line450\n[... 50 more lines;") {
		t.Fatalf("explicit limit above the default: %q %v", res.Output[max(0, len(res.Output)-80):], err)
	}
}

func TestReadFile_NULInFirst8KBIsBinary(t *testing.T) {
	env := newTestEnv(t, map[string]string{"bin": "ab\x00cd\n"})
	_, err := runTool(ReadFile{}, env, `{"path":"bin"}`)
	if err == nil || !strings.Contains(err.Error(), "binary file") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadFile_NULAfter8KBIsText(t *testing.T) {
	env := newTestEnv(t, map[string]string{"late": strings.Repeat("a", 9000) + "\x00\n"})
	res, err := runTool(ReadFile{}, env, `{"path":"late"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Output, "1: aaaa") {
		t.Fatalf("output head %q", res.Output[:20])
	}
}

func TestReadFile_CRLFIsStripped(t *testing.T) {
	env := newTestEnv(t, map[string]string{"win.txt": "a\r\nb\r\n"})
	res, err := runTool(ReadFile{}, env, `{"path":"win.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "1: a\n2: b" {
		t.Fatalf("output %q", res.Output)
	}
}

func TestReadFile_MissingAndDirectory(t *testing.T) {
	env := newTestEnv(t, map[string]string{"sub/x": "1\n"})
	if _, err := runTool(ReadFile{}, env, `{"path":"nope.txt"}`); err == nil {
		t.Error("missing file: no error")
	}
	if _, err := runTool(ReadFile{}, env, `{"path":"sub"}`); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("directory: err = %v", err)
	}
}

func TestReadFile_EscapeRejected(t *testing.T) {
	env := newTestEnv(t, nil)
	_, err := runTool(ReadFile{}, env, `{"path":"../x"}`)
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadFile_Summary(t *testing.T) {
	if got := (ReadFile{}).Summary(json.RawMessage(`{"path":"README.md"}`)); got != "README.md" {
		t.Errorf("summary %q", got)
	}
	if got := (ReadFile{}).Summary(json.RawMessage(`{"path":`)); got != "" {
		t.Errorf("bad JSON summary %q", got)
	}
}

func TestReadFile_RootStylePathMeansProjectFile(t *testing.T) {
	env := newTestEnv(t, map[string]string{"words.txt": "alpha"})
	res, err := runTool(ReadFile{}, env, `{"path":"/words.txt"}`)
	if err != nil || !strings.Contains(res.Output, "alpha") {
		t.Fatalf("got %q, %v", res.Output, err)
	}
	if _, err := runTool(ReadFile{}, env, `{"path":"/../etc/passwd"}`); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("escape: err = %v", err)
	}
}
