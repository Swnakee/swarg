package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func args(s string) json.RawMessage { return json.RawMessage(s) }

func TestStub_ReadFile(t *testing.T) {
	env := newTestEnv(t, map[string]string{"a.go": strings.Repeat("x\n", 500)})
	a := args(`{"path":"a.go","offset":10,"limit":120}`)
	res, err := runTool(ReadFile{}, env, string(a))
	if err != nil {
		t.Fatal(err)
	}
	want := "[read_file a.go: 120 lines shown earlier; read it again if needed]"
	if got := (ReadFile{}).Stub(a, res); got != want {
		t.Errorf("Stub = %q, want %q", got, want)
	}
}

func TestStub_GrepAndListDir(t *testing.T) {
	out := strings.TrimSpace(strings.Repeat("a.go:1: x\n", 12))
	if got := (Grep{}).Stub(args(`{"pattern":"x"}`), Result{Output: out}); got != `[grep "x": 12 matches shown earlier]` {
		t.Errorf("grep stub %q", got)
	}
	long := strings.Repeat("p", 60)
	got := (Grep{}).Stub(args(`{"pattern":"`+long+`"}`), Result{Output: out + "\n[... more matches]"})
	if !strings.Contains(got, strings.Repeat("p", 40)+"...") || strings.Contains(got, long) || !strings.Contains(got, "12+ matches") {
		t.Errorf("long pattern / more note: %q", got)
	}
	if got := (Grep{}).Stub(args(`{"pattern":"x"}`), Result{Output: "no matches"}); !strings.Contains(got, " 0 matches") {
		t.Errorf("no matches: %q", got)
	}
	if got := (ListDir{}).Stub(args(`{"path":"internal"}`), Result{Output: "a.go\nb/\nc.go"}); got != "[list_dir internal: 3 entries shown earlier]" {
		t.Errorf("list_dir stub %q", got)
	}
	if got := (ListDir{}).Stub(args(`{}`), Result{Output: "a"}); !strings.Contains(got, "list_dir .:") {
		t.Errorf("default path: %q", got)
	}
}

func TestStub_ShellTail(t *testing.T) {
	body := func(n int) string {
		var b strings.Builder
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&b, "out %d\n", i)
		}
		return strings.TrimRight(b.String(), "\n")
	}
	a := args(`{"command":"go test ./..."}`)
	cases := []struct {
		name   string
		res    Result
		head   string
		lines  []string
		absent string
	}{
		{"success last 3", Result{Output: body(10) + "\n[exit 0]", Brief: "exit 0"}, "[shell go test ./...: exit 0]", []string{"out 8", "out 9", "out 10"}, "out 7"},
		{"fewer than 3", Result{Output: body(2) + "\n[exit 0]", Brief: "exit 0"}, "[shell go test ./...: exit 0]", []string{"out 1", "out 2"}, ""},
		{"failure last 15", Result{Output: body(40) + "\n[exit 1]", Brief: "exit 1"}, "[shell go test ./...: exit 1]", []string{"out 26", "out 40"}, "out 25"},
		{"killed keeps marker", Result{Output: body(5) + "\n[killed: timeout]", Brief: "killed: timeout"}, "[shell go test ./...: killed: timeout]", []string{"out 5"}, ""},
		{"no output", Result{Output: "[exit 0]", Brief: "exit 0"}, "[shell go test ./...: exit 0]", nil, ""},
	}
	for _, c := range cases {
		got := (Shell{}).Stub(a, c.res)
		if !strings.HasPrefix(got, c.head) {
			t.Errorf("%s: head of %q, want %q", c.name, got, c.head)
		}
		for _, l := range c.lines {
			if !strings.Contains(got, l) {
				t.Errorf("%s: %q missing %q", c.name, got, l)
			}
		}
		if c.absent != "" && strings.Contains(got, c.absent+"\n") || c.absent != "" && strings.HasSuffix(got, c.absent) {
			t.Errorf("%s: %q contains %q", c.name, got, c.absent)
		}
	}
}

func TestStub_ShellOutput(t *testing.T) {
	got := (ShellOutput{}).Stub(args(`{"id":"bg1"}`), Result{Output: "a\nb\nc\nd\n[exit 2]", Brief: "exit 2"})
	if want := "[shell_output bg1: exit 2]\nb\nc\nd"; got != want {
		t.Errorf("stub %q, want %q", got, want)
	}
	got = (ShellOutput{}).Stub(args(`{"id":"bg3"}`), Result{Output: "line\n[running]"})
	if !strings.HasPrefix(got, "[shell_output bg3: running]") {
		t.Errorf("running: %q", got)
	}
	got = (ShellOutput{}).Stub(args(`{}`), Result{Output: "bg7: line\n[exit 0]"})
	if !strings.HasPrefix(got, "[shell_output bg7: exit 0]") {
		t.Errorf("no id: %q", got)
	}
}

func TestStub_TinyToolsReturnEmpty(t *testing.T) {
	s, _ := NewSet(All(), nil)
	for _, n := range []string{"edit_file", "write_file", "shell_kill"} {
		if got := s.Stub(n, args(`{"path":"a.go"}`), "ok: edited a.go, 1 replacement and more words", "ok"); got != "" {
			t.Errorf("%s stub %q, want empty", n, got)
		}
	}
	for _, n := range []string{"read_file", "grep", "shell", "list_dir", "shell_output"} {
		if got := s.Stub(n, args(`{"path":"a.go","pattern":"x","command":"ls","id":"bg1"}`), "error: file not found", ""); got != "" {
			t.Errorf("%s error result stubbed: %q", n, got)
		}
	}
}

func TestStub_NeverLongerThanResultAndNamesTheCall(t *testing.T) {
	s, _ := NewSet(All(), nil)
	big := strings.Repeat("1: some line of code\n", 100)
	tests := []struct {
		name, args, out, brief, mention string
	}{
		{"read_file", `{"path":"pkg/a.go"}`, big, "100 lines", "pkg/a.go"},
		{"read_file", `{"path":"pkg/a.go"}`, "1: x", "1 lines", ""}, // tiny: stub would be longer
		{"grep", `{"pattern":"Foo"}`, big, "100 matches", "Foo"},
		{"list_dir", `{"path":"src"}`, big, "100 entries", "src"},
		{"shell", `{"command":"make all"}`, big + "[exit 0]", "exit 0", "make all"},
		{"shell_output", `{"id":"bg2"}`, big + "[running]", "running", "bg2"},
	}
	for _, tt := range tests {
		got := s.Stub(tt.name, args(tt.args), tt.out, tt.brief)
		if len(got) > len(tt.out) {
			t.Errorf("%s: stub longer than the output", tt.name)
		}
		if strings.Contains(got, tt.out) && got != "" {
			t.Errorf("%s: stub holds the whole output", tt.name)
		}
		if got != "" && (!strings.Contains(got, tt.name) || !strings.Contains(got, tt.mention)) {
			t.Errorf("%s: stub %q does not name the call (%q)", tt.name, got, tt.mention)
		}
	}
}

func TestTouched_FileTools(t *testing.T) {
	s, _ := NewSet(All(), nil)
	for _, n := range []string{"edit_file", "write_file"} {
		if got := s.Touched(n, args(`{"path":"./pkg/../a.go"}`)); got != "a.go" {
			t.Errorf("%s touched %q, want a.go", n, got)
		}
		if got := s.Touched(n, args(`{"path":"a.go"}`)); got != s.Touched(n, args(`{"path":"./a.go"}`)) {
			t.Errorf("%s: ./a.go and a.go differ", n)
		}
		if got := s.Touched(n, args(`not json`)); got != "" {
			t.Errorf("%s bad args: %q", n, got)
		}
	}
	for _, n := range []string{"read_file", "grep", "shell", "list_dir", "shell_output", "shell_kill"} {
		if got := s.Touched(n, args(`{"path":"a.go"}`)); got != "" {
			t.Errorf("%s touched %q, want empty", n, got)
		}
	}
}

func TestSet_StubAndTouchedDispatch(t *testing.T) {
	s := newFakeSet(t) // fakes implement neither interface
	if s.Stub("zz", nil, "out", "") != "" || s.Touched("zz", nil) != "" {
		t.Error("unknown tool must give empty")
	}
	if s.Stub("a", nil, "output", "") != "" || s.Touched("a", nil) != "" {
		t.Error("tool without the interface must give empty")
	}
}
