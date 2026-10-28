//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"swarg/internal/chat"
	"swarg/internal/config"
	"swarg/internal/llm/llmtest"
	"swarg/internal/session"
)

// ---------------------------------------------------------------------------
// End-to-end combinations of flags and an old config file (real main(), plain mode).
// ---------------------------------------------------------------------------

// A T2_3 config (no session/context sections, its own extra_body) must run unchanged:
// its sampling values reach the wire untouched and the new defaults (save on) apply.
func TestMain_OldConfigFileRunsWithNewDefaults(t *testing.T) {
	e := newEnv(t)
	cfgDir, _ := config.Dir()
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"provider":"local","providers":{"local":{"type":"openai","base_url":"http://x/v1","model":"m",
		"context_limit":16384,"max_tokens":512,"extra_body":{"repetition_penalty":1.1}}},
		"prompt":{"warn_share":0.25},"tools":{"max_steps":9}}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := llmtest.New(t, say("hello"))
	out, errOut, code := e.swarg(t, srv, "go", "--yes")
	if code != 0 || !strings.Contains(out, "hello") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	req := srv.Requests()[0]
	if req["repetition_penalty"] != 1.1 {
		t.Errorf("repetition_penalty = %v, want the old file's 1.1", req["repetition_penalty"])
	}
	if _, ok := req["temperature"]; ok {
		t.Errorf("temperature %v came from nowhere: an old extra_body must stay as written", req["temperature"])
	}
	if req["max_tokens"] != float64(512) {
		t.Errorf("max_tokens = %v, want 512", req["max_tokens"])
	}
	if len(files(t, e.sdir(t), "*.jsonl")) != 1 {
		t.Error("new default session.save=true did not apply to an old config")
	}
}

// --sessions only lists: it wins over --continue and never asks the model.
func TestMain_SessionsFlagWinsOverContinue(t *testing.T) {
	e := newEnv(t)
	seed(t, e.sdir(t), "abc111", "first question", e.fixedNow())
	out, errOut, code := e.swarg(t, nil, "ignored", "--sessions", "--continue")
	if code != 0 || !strings.Contains(out, "first question") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// --continue --no-save resumes the history but leaves the old file byte-identical.
func TestMain_ContinueWithNoSaveLeavesTheFileAlone(t *testing.T) {
	e := newEnv(t)
	seed(t, e.sdir(t), "abc111", "old question", e.fixedNow())
	path := files(t, e.sdir(t), "*.jsonl")[0]
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := llmtest.New(t, say("new answer"))
	out, errOut, code := e.swarg(t, srv, "next", "--yes", "--continue", "--no-save")
	if code != 0 || !strings.Contains(out, "new answer") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if got := roleContent(srv.Requests()[0]); !contains(got, "user:old question") || !contains(got, "user:next") {
		t.Errorf("request %v lacks the resumed history or the new text", got)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Error("--no-save rewrote the resumed SessionFile")
	}
	if len(files(t, e.sdir(t), "*")) != 1 {
		t.Errorf("extra files with --no-save: %v", files(t, e.sdir(t), "*"))
	}
}

// A saved session that holds a WorkSummary resumes, sends the summary as it is and continues.
func TestMain_ContinueSessionWithAWorkSummary(t *testing.T) {
	e := newEnv(t)
	w, err := session.Create(e.sdir(t), session.Header{ID: "sum111", Model: "m", Started: e.fixedNow()})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []chat.Message{
		{Role: chat.RoleUser, Content: "old question", TurnStart: true},
		{Role: chat.RoleAssistant, Content: "[work summary] read a.go and b.go", Summary: true},
		{Role: chat.RoleAssistant, Content: "old answer"},
	} {
		if _, err := w.Message(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	srv := llmtest.New(t, say("continued"))
	out, errOut, code := e.swarg(t, srv, "next", "--yes", "--continue")
	if code != 0 || !strings.Contains(out, "continued") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	got := roleContent(srv.Requests()[0])
	for _, want := range []string{"user:old question", "assistant:[work summary] read a.go and b.go", "assistant:old answer", "user:next"} {
		if !contains(got, want) {
			t.Errorf("request %v lacks %q", got, want)
		}
	}
}
