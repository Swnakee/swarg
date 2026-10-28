package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// R-46 for every tool with a required argument: a call without it, or with an empty string,
// fails with an error that names the argument and shows the correct call.
func TestRun_MissingArgumentShowsCall(t *testing.T) {
	cases := []struct {
		tool Tool
		arg  string
		args []string // calls that must fail
	}{
		{ReadFile{}, "path", []string{`{}`, `{"path":""}`, `{"offset":1}`}},
		{Grep{}, "pattern", []string{`{}`, `{"pattern":""}`, `{"path":"."}`}},
		{Shell{}, "command", []string{`{}`, `{"command":""}`}},
		{EditFile{}, "path", []string{`{}`, `{"path":""}`, `{"old":"a","new":"b"}`}},
		{EditFile{}, "old", []string{`{"path":"a.go","new":"b"}`}},
		{EditFile{}, "new", []string{`{"path":"a.go","old":"b"}`}},
		{WriteFile{}, "content", []string{`{"path":"a.go"}`}},
		{ShellKill{}, "id", []string{`{}`, `{"id":""}`}},
		{WebFetch{}, "url", []string{`{}`, `{"url":""}`}},
		{WebSearch{}, "query", []string{`{}`, `{"query":""}`}},
	}
	for _, c := range cases {
		name := c.tool.Spec().Name
		for _, a := range c.args {
			_, err := c.tool.Run(context.Background(), Env{Dir: t.TempDir()}, json.RawMessage(a))
			if err == nil || !strings.Contains(err.Error(), `"`+c.arg+`"`) || !strings.Contains(err.Error(), "call "+name+" like {") {
				t.Errorf("%s %s: error %v does not name %q and show the call", name, a, err, c.arg)
			}
		}
	}
}

func TestExampleOf_StripsTheToolName(t *testing.T) {
	if got := exampleOf(Shell{}); !strings.HasPrefix(got, `{"command"`) {
		t.Fatalf("shell example = %q, want the JSON only", got)
	}
	if got := exampleOf(ReadFile{}); !strings.HasPrefix(got, `{"path"`) {
		t.Fatalf("read_file example = %q", got)
	}
}
