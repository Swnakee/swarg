package tools

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func runOutput(env Env, id string) (Result, error) {
	args, _ := json.Marshal(map[string]string{"id": id})
	return ShellOutput{}.Run(context.Background(), env, args)
}

func TestShellOutput_ReadsNewOutputAndStatus(t *testing.T) {
	env := testEnv(t)
	p, id := start(t, env.Jobs, "gated")
	q, id2 := start(t, env.Jobs, "gated")
	p.waitBytes(t, 4)
	q.waitBytes(t, 4)
	res, err := runOutput(env, id)
	if err != nil || res.Output != "one\n[running]" || res.Brief != "4 bytes, running" {
		t.Fatalf("got %q / %q / %v", res.Output, res.Brief, err)
	}
	if res, _ = runOutput(env, id); res.Output != "[running]" {
		t.Errorf("second read: %q", res.Output)
	}
	if res, _ = runOutput(env, id2); !strings.HasPrefix(res.Output, "one\n") { // offsets are per job
		t.Errorf("other job: %q", res.Output)
	}
	io.WriteString(p.stdin, "\n")
	p.stdin.Close()
	waitDone(t, env.Jobs, id)
	res, _ = runOutput(env, id)
	if res.Output != "two\nthree\n[exit 0]" || res.Brief != "exit 0" {
		t.Errorf("got %q / %q", res.Output, res.Brief)
	}
}

func TestShellOutput_BadArgs(t *testing.T) {
	env := testEnv(t)
	start(t, env.Jobs, "echo")
	for _, a := range []string{`{"id":""}`, `{"id":"bg9"}`, `{"id":"bg1","x":1}`, `nope`} {
		if _, err := (ShellOutput{}).Run(context.Background(), env, json.RawMessage(a)); err == nil {
			t.Errorf("%s: want error", a)
		}
	}
	if _, err := (ShellOutput{}).Run(context.Background(), Env{}, json.RawMessage(`{"id":"bg1"}`)); err == nil {
		t.Error("Jobs == nil: want error")
	}
}

func TestShellOutput_Flags(t *testing.T) {
	s := ShellOutput{}
	if s.Spec().Name != "shell_output" || !s.ReadOnly() || s.Confirm() {
		t.Error("bad flags")
	}
	if got := s.Summary(json.RawMessage(`{"id":"bg2"}`)); got != "bg2" {
		t.Errorf("summary %q", got)
	}
}

// A big read stays inside the tool result cap, so Set.Run's Cap never eats the status line.
func TestShellOutput_FitsResultCap(t *testing.T) {
	env := testEnv(t)
	p, id := start(t, env.Jobs, "bigafter")
	io.WriteString(p.stdin, "\n")
	waitDone(t, env.Jobs, id)
	res, err := runOutput(env, id)
	if err != nil {
		t.Fatal(err)
	}
	if Cap(res.Output) != res.Output || !strings.HasSuffix(res.Output, "\n[exit 0]") ||
		!strings.Contains(res.Output, "call shell_output again]") {
		t.Errorf("result %d bytes, tail %q", len(res.Output), res.Output[len(res.Output)-90:])
	}
}

// Small models (Qwen3.5-2B, e2e 2026-10-26) call shell_output with no id; it reads the latest job.
func TestShellOutput_NoIDReadsLatestJob(t *testing.T) {
	env := testEnv(t)
	if _, err := (ShellOutput{}).Run(context.Background(), env, json.RawMessage(`{}`)); err == nil ||
		!strings.Contains(err.Error(), "no background jobs: shell_output only reads commands started with shell background:true; to run a command use shell") {
		t.Fatalf("no jobs: err = %v", err)
	}
	_, first := start(t, env.Jobs, "echo")
	_, latest := start(t, env.Jobs, "echo")
	waitDone(t, env.Jobs, first)
	waitDone(t, env.Jobs, latest)
	if _, err := runOutput(env, first); err != nil { // drain the first job
		t.Fatal(err)
	}
	res, err := (ShellOutput{}).Run(context.Background(), env, json.RawMessage(`{}`))
	if err != nil || !strings.HasPrefix(res.Output, latest+": ") || !strings.HasSuffix(res.Output, "[exit 0]") {
		t.Fatalf("got %q / %v, want output of %s", res.Output, err, latest)
	}
}

// shell_kill never guesses: it still needs the id.
func TestShellKill_NeedsID(t *testing.T) {
	env := testEnv(t)
	start(t, env.Jobs, "echo")
	if _, err := (ShellKill{}).Run(context.Background(), env, json.RawMessage(`{}`)); err == nil {
		t.Error("shell_kill {}: want error")
	}
}

// shell_output waits for the job to end (up to Env.OutputWait), so a model does not burn its
// steps polling a running job (live run 2026-10-27: 17 polls of a 12 s loop).
func TestShellOutput_WaitsForExit(t *testing.T) {
	env := testEnv(t)
	env.OutputWait = time.Minute // returns at the exit, not after the wait
	p, id := start(t, env.Jobs, "gated")
	p.waitBytes(t, 4)
	go func() {
		io.WriteString(p.stdin, "\n")
		p.stdin.Close()
	}()
	res, err := runOutput(env, id)
	if err != nil || !strings.HasSuffix(res.Output, "[exit 0]") || !strings.Contains(res.Output, "three") {
		t.Fatalf("got %q / %v", res.Output, err)
	}
}

func TestShellOutput_WaitEndsRunning(t *testing.T) {
	env := testEnv(t)
	env.OutputWait = 20 * time.Millisecond
	p, id := start(t, env.Jobs, "gated")
	p.waitBytes(t, 4)
	res, err := runOutput(env, id)
	if err != nil || !strings.HasSuffix(res.Output, "[running]") {
		t.Fatalf("got %q / %v", res.Output, err)
	}
	io.WriteString(p.stdin, "\n")
	p.stdin.Close()
	waitDone(t, env.Jobs, id)
}

func TestShellOutput_CancelStopsWait(t *testing.T) {
	env := testEnv(t)
	env.OutputWait = time.Hour
	p, id := start(t, env.Jobs, "gated")
	p.waitBytes(t, 4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	args, _ := json.Marshal(map[string]string{"id": id})
	res, err := ShellOutput{}.Run(ctx, env, args)
	if err != nil || !strings.HasSuffix(res.Output, "[running]") {
		t.Fatalf("got %q / %v", res.Output, err)
	}
	io.WriteString(p.stdin, "\n")
	p.stdin.Close()
	waitDone(t, env.Jobs, id)
}

// An id that names no job lists the known ids (or says there are no jobs at all).
func TestShellOutput_UnknownIDListsKnownIDs(t *testing.T) {
	env := testEnv(t)
	if _, err := runOutput(env, "bg9"); err == nil || !strings.Contains(err.Error(), "no background jobs: shell_output only reads") {
		t.Fatalf("no jobs: err = %v", err)
	}
	_, a := start(t, env.Jobs, "echo")
	_, b := start(t, env.Jobs, "echo")
	_, err := runOutput(env, "bg9")
	if err == nil || !strings.Contains(err.Error(), `unknown job "bg9"`) ||
		!strings.Contains(err.Error(), "known ids: "+a+", "+b) {
		t.Fatalf("unknown id: err = %v", err)
	}
}
