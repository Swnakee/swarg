package main

import (
	"path/filepath"
	"strings"
	"testing"

	"swarg/internal/chat"
	"swarg/internal/tools"
)

func newTestRunner(t *testing.T) toolRunner {
	t.Helper()
	set, err := tools.NewSet(tools.All(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return toolRunner{set: set, env: tools.Env{Dir: filepath.Join(t.TempDir(), "proj")}}
}

func TestToolRunner_StubForwardsToTools(t *testing.T) {
	r := newTestRunner(t)
	out := strings.Repeat("1: some code line\n", 8)
	got := r.Stub(chat.ToolCall{Name: "read_file", Args: `{"path":"a.go"}`}, out, "8 lines")
	if want := "[read_file a.go: 8 lines shown earlier; read it again if needed]"; got != want {
		t.Errorf("Stub = %q, want %q", got, want)
	}
	if got := r.Stub(chat.ToolCall{Name: "edit_file", Args: `{"path":"a.go"}`}, "edited a.go with a long enough message", ""); got != "" {
		t.Errorf("edit_file stub = %q, want empty", got)
	}
}

func TestToolRunner_TouchedIsProjectRelative(t *testing.T) {
	r := newTestRunner(t)
	abs := filepath.Join(r.env.Dir, "pkg", "a.go")
	for _, args := range []string{`{"path":"./pkg/a.go"}`, `{"path":"` + filepath.ToSlash(abs) + `"}`} {
		if got := r.Touched(chat.ToolCall{Name: "write_file", Args: args}); got != "pkg/a.go" {
			t.Errorf("Touched(%s) = %q, want pkg/a.go", args, got)
		}
	}
	if got := r.Touched(chat.ToolCall{Name: "read_file", Args: `{"path":"a.go"}`}); got != "" {
		t.Errorf("read_file touched %q", got)
	}
}
