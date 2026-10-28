package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// list_dir
// ---------------------------------------------------------------------------

func TestListDir_SortedWithDirSlash(t *testing.T) {
	env := newTestEnv(t, map[string]string{"b.txt": "", "a.txt": "", "dir/x": ""})
	res, err := runTool(ListDir{}, env, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "a.txt\nb.txt\ndir/" || res.Brief != "3 entries" {
		t.Fatalf("output %q brief %q", res.Output, res.Brief)
	}
}

func TestListDir_SubPath(t *testing.T) {
	env := newTestEnv(t, map[string]string{"sub/x.go": "", "other.go": ""})
	res, err := runTool(ListDir{}, env, `{"path":"sub"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "x.go" {
		t.Fatalf("output %q", res.Output)
	}
}

func TestListDir_CapsAt500(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 510; i++ {
		files[fmt.Sprintf("f%03d", i)] = ""
	}
	env := newTestEnv(t, files)
	res, err := runTool(ListDir{}, env, `{"path":"."}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output[len(res.Output)-len("[... 10 more entries]"):] != "[... 10 more entries]" || res.Brief != "510 entries" {
		t.Fatalf("tail %q brief %q", res.Output[len(res.Output)-40:], res.Brief)
	}
}

func TestListDir_EmptyDir(t *testing.T) {
	env := newTestEnv(t, map[string]string{"empty/.keep": ""})
	res, err := runTool(ListDir{}, env, `{"path":"empty"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != ".keep" {
		t.Fatalf("output %q", res.Output)
	}
	env2 := newTestEnv(t, map[string]string{"x/y": ""})
	if res, err := runTool(ListDir{}, env2, `{"path":"x/y"}`); err == nil {
		t.Errorf("file path: no error, got %q", res.Output)
	}
	if err := os.Mkdir(filepath.Join(env2.Dir, "none"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err = runTool(ListDir{}, env2, `{"path":"none"}`)
	if err != nil || res.Output != "" || res.Brief != "0 entries" {
		t.Fatalf("empty dir: output %q brief %q err %v", res.Output, res.Brief, err)
	}
}

func TestListDir_FileIsError(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a.txt": "1"})
	if _, err := runTool(ListDir{}, env, `{"path":"a.txt"}`); err == nil {
		t.Fatal("file path: no error")
	}
}

func TestListDir_EscapeRejected(t *testing.T) {
	env := newTestEnv(t, nil)
	_, err := runTool(ListDir{}, env, `{"path":"../"}`)
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("err = %v", err)
	}
}

func TestListDir_Summary(t *testing.T) {
	if got := (ListDir{}).Summary([]byte(`{}`)); got != "." {
		t.Errorf("default summary %q", got)
	}
	if got := (ListDir{}).Summary([]byte(`{"path":`)); got != "" {
		t.Errorf("bad JSON summary %q", got)
	}
}

// R-45 neighbour (B3): glob lists matching files at any depth, project-relative, skipping .git.
func TestListDir_GlobFindsFilesAtAnyDepth(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a_test.go": "", "a.go": "", "sub/b_test.go": "", ".git/c_test.go": "", "node_modules/d_test.go": ""})
	res, err := runTool(ListDir{}, env, `{"glob":"*_test.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "a_test.go\nsub/b_test.go" || res.Brief != "2 files" {
		t.Fatalf("output %q brief %q", res.Output, res.Brief)
	}
}

func TestListDir_GlobUnderPathAndNoMatch(t *testing.T) {
	env := newTestEnv(t, map[string]string{"sub/x.go": "", "sub/deep/y.go": "", "z.go": ""})
	res, err := runTool(ListDir{}, env, `{"path":"sub","glob":"*.go"}`)
	if err != nil || res.Output != "sub/deep/y.go\nsub/x.go" {
		t.Fatalf("got %q %v", res.Output, err)
	}
	res, err = runTool(ListDir{}, env, `{"glob":"*.rs"}`)
	if err != nil || res.Output != "no matches" {
		t.Fatalf("got %q %v", res.Output, err)
	}
}

func TestListDir_GlobBadPatternAndCap(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a.go": ""})
	if _, err := runTool(ListDir{}, env, `{"glob":"[x"}`); err == nil || !strings.Contains(err.Error(), "invalid glob") {
		t.Errorf("bad glob: %v", err)
	}
	files := map[string]string{}
	for i := 0; i < maxListEntries+5; i++ {
		files[fmt.Sprintf("f%04d.txt", i)] = ""
	}
	res, err := runTool(ListDir{}, newTestEnv(t, files), `{"glob":"*.txt"}`)
	if err != nil || !strings.HasSuffix(res.Output, "[... 5 more files]") {
		t.Errorf("cap: %v ...%q", err, res.Output[len(res.Output)-30:])
	}
}
