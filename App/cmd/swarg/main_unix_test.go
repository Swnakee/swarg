//go:build !windows

package main

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// The real main() runs as a helper process (this test binary re-executed) against a
// fake model server in plain mode with --yes. The tests check that no background job
// survives swarg on any exit path.
// ---------------------------------------------------------------------------

const helperEnv = "SWARG_TEST_MAIN"

// TestMain turns the test binary into swarg itself when re-executed as the helper.
func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		main() // exits
	}
	os.Exit(m.Run())
}

// bgReply asks for a background shell call that records its pid and becomes a long sleep.
func bgReply() llmtest.Reply {
	return llmtest.Reply{ToolCalls: []chat.ToolCall{{
		ID: "c1", Name: "shell",
		Args: `{"command":"echo $$ > pid.txt; exec sleep 300","background":true}`,
	}}}
}

// waitReply asks for a foreground call that returns once the background command wrote its
// pid, so the tests never race swarg's exit against the job's first instructions.
func waitReply() llmtest.Reply {
	return llmtest.Reply{ToolCalls: []chat.ToolCall{{
		ID: "c2", Name: "shell", Args: `{"command":"until [ -s pid.txt ]; do sleep 0.02; done"}`,
	}}}
}

// startSwarg starts the helper swarg in a fresh work dir with "go" on stdin.
func startSwarg(t *testing.T, srv *llmtest.Server) (*exec.Cmd, string) {
	t.Helper()
	work := t.TempDir()
	// no background grace and no shell_output wait: the tests drive the timing themselves
	cfg := filepath.Join(work, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"tools":{"background_grace_sec":0,"output_wait_sec":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "--config", cfg, "--yes", "--base-url", srv.URL, "--model", "m")
	cmd.Dir = work
	cmd.Stdin = strings.NewReader("go")
	cmd.Env = append(os.Environ(), helperEnv+"=1", "HOME="+work, "XDG_CONFIG_HOME="+work, "GORACE=atexit_sleep_ms=0")
	return cmd, work
}

// pidOf reads the pid the background command wrote.
func pidOf(t *testing.T, work string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(work, "pid.txt"))
	if err != nil {
		t.Fatalf("no pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		t.Fatalf("bad pid %q", b)
	}
	return pid
}

// wantGone fails unless the process no longer exists.
func wantGone(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		syscall.Kill(pid, syscall.SIGKILL) // do not leak it from the test either
		t.Fatalf("background job pid %d survived swarg (kill 0 = %v)", pid, err)
	}
}

// exitCode returns the exit code of a finished command.
func exitCode(t *testing.T, err error) int {
	t.Helper()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.ExitCode()
	}
	t.Fatalf("wait: %v", err)
	return -1
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// R-15
func TestMain_NoChildSurvives(t *testing.T) {
	srv := llmtest.New(t, bgReply(), waitReply(), llmtest.Reply{Chunks: []string{"started"}})
	cmd, work := startSwarg(t, srv)

	out, err := cmd.CombinedOutput()
	if code := exitCode(t, err); code != 0 {
		t.Fatalf("exit %d, output %q", code, out)
	}
	wantGone(t, pidOf(t, work))
}

// R-15
func TestMain_SignalKillsJobs(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) { signalKillsJobs(t, sig) })
	}
}

// signalKillsJobs sends sig to swarg while its background job runs; the job must die with it.
func signalKillsJobs(t *testing.T, sig syscall.Signal) {
	gate := make(chan struct{})
	srv := llmtest.New(t, bgReply(), waitReply(), llmtest.Reply{Chunks: []string{"x"}, Gate: gate})
	t.Cleanup(func() { close(gate) })
	cmd, work := startSwarg(t, srv)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// The second "└" line is printed when the waiting call ended: the job runs and wrote its pid.
	sc := bufio.NewScanner(stdout)
	for n := 0; n < 2 && sc.Scan(); {
		if strings.HasPrefix(strings.TrimSpace(sc.Text()), "└") {
			n++
		}
	}
	pid := pidOf(t, work)
	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	for sc.Scan() {
	}
	if code := exitCode(t, cmd.Wait()); code != 1 {
		t.Errorf("exit code = %d, want 1 after %v", code, sig)
	}
	wantGone(t, pid)
}

func TestMain_ExitCodePathsKillJobs(t *testing.T) {
	srv := llmtest.New(t, bgReply(), waitReply(), llmtest.Reply{Status: 500})
	cmd, work := startSwarg(t, srv)

	out, err := cmd.CombinedOutput()
	if code := exitCode(t, err); code != 1 {
		t.Fatalf("exit %d, want 1 (turn error); output %q", code, out)
	}
	wantGone(t, pidOf(t, work))
}
