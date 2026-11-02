package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	shellBufLimit   = 1 << 20         // bytes kept while the command runs
	shellWaitDelay  = 2 * time.Second // Wait gives up on inherited pipes after this
	shellDefTimeout = 120 * time.Second
	progressEvery   = 50 * time.Millisecond // minimum gap between Progress calls
	shellBodyLimit  = MaxOutput - 160       // room left for the cut note and the final "[exit N]" line
)

// ---------------------------------------------------------------------------
// Tool
// ---------------------------------------------------------------------------

// Shell runs a shell command in the project folder.
type Shell struct{ askTool }

// shellCommand builds the command to run; tests replace it to run a helper
// process. It must use exec.CommandContext(ctx, ...).
var shellCommand = defaultShellCommand

// Spec describes the tool to the model.
func (Shell) Spec() chat.ToolSpec {
	return chat.ToolSpec{
		Name:        "shell",
		Description: "Run a shell command in the project folder",
		Schema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string",` +
			`"description":"the command line"},"background":{"type":"boolean",` +
			`"description":"only for servers/watchers that never end"}},"required":["command"]}`),
	}
}

// Block is the system prompt text for the tool.
func (Shell) Block() string {
	return "shell: run a command (build, test, git) in the project folder; returns output and exit code.\n" +
		"\"background\": true only for a server or watcher that never ends; then use shell_output / shell_kill.\n" +
		`Example: shell {"command": "go test ./..."}`
}

// Summary is the first line of the command, at most 80 characters.
func (Shell) Summary(args json.RawMessage) string {
	var a shellArgs
	if Decode(args, &a, "command") != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(a.Command), "\n")
	if r := []rune(line); len(r) > 80 {
		line = string(r[:80])
	}
	return line
}

// shellArgs are the arguments of the shell tool.
type shellArgs struct {
	Command    string `json:"command"`
	Background bool   `json:"background"`
}

// startedFormat is the result line of a command that went to the background by itself.
const startedFormat = "started in background as %s; read it with shell_output {\"id\": \"%[1]s\"}"

// Run executes the command and returns its combined output ending with a status line.
// The process runs under a context that is never cancelled: timeout, cancel and detach are
// handled here by hand, so a detached process outlives the call context.
func (t Shell) Run(ctx context.Context, env Env, args json.RawMessage) (Result, error) {
	var a shellArgs
	if err := DecodeFor(t, args, &a, "command"); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(a.Command) == "" {
		return Result{}, errors.New("command is empty")
	}
	if a.Background && env.Jobs == nil {
		return Result{}, errors.New("background commands are not available")
	}
	if ctx.Err() != nil {
		return Result{Output: "\n[killed: cancelled]", Brief: "killed: cancelled"}, nil
	}
	timeout := env.ShellTimeout
	if timeout <= 0 {
		timeout = shellDefTimeout
	}

	cmd := shellCommand(context.WithoutCancel(ctx), env, a.Command)
	cmd.Dir = env.Dir
	out := newRing(shellBufLimit)
	sink := env.Progress
	if a.Background && env.BackgroundGrace <= 0 {
		sink = nil // adopted at once: nothing streams
	}
	w := &sinkWriter{out: out, sink: sink}
	cmd.Stdout, cmd.Stderr = w, w
	cmd.WaitDelay = shellWaitDelay
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start shell: %w", err)
	}
	done := make(chan error, 1) // the only cmd.Wait caller
	go func() { done <- cmd.Wait() }()

	if a.Background && env.BackgroundGrace <= 0 {
		return adoptResult(env.Jobs, cmd, a.Command, out, done, startedFormat, true)
	}
	// A background command runs in the foreground for its grace (live output, Ctrl+B) and is
	// adopted only when the grace ends; the shell timeout does not apply to it.
	limit := timeout
	if a.Background {
		limit = env.BackgroundGrace
	}
	timer := time.NewTimer(limit)
	defer timer.Stop()
	detach := env.Detach
	if env.Jobs == nil {
		detach = nil // no manager: Ctrl+B is ignored
	}
	for {
		var fired string
		select {
		case err := <-done:
			w.stop()
			return exitResult(out, err)
		case <-ctx.Done():
			fired = "cancelled"
		case <-timer.C:
			fired = "timeout"
		case <-detach:
			fired = "detach"
		}
		// A signal fired. Cancel wins; then a process that already exited wins.
		if ctx.Err() != nil {
			return killResult(cmd, out, w, done, "cancelled"), nil
		}
		select {
		case err := <-done:
			w.stop()
			return exitResult(out, err)
		default:
		}
		if fired == "timeout" && a.Background {
			w.stop()
			return adoptResult(env.Jobs, cmd, a.Command, out, done, startedFormat, true)
		}
		if fired == "timeout" {
			return killResult(cmd, out, w, done, "timeout"), nil
		}
		w.stop()
		res, err := adoptResult(env.Jobs, cmd, a.Command, out, done, "moved to background as %s by the user", false)
		if err == nil {
			return res, nil
		}
		detach = nil // could not adopt (too many jobs): keep waiting in the foreground
		w.resume(env.Progress)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// exitResult builds the result of a command that ended by itself.
func exitResult(out *ring, err error) (Result, error) {
	var ee *exec.ExitError
	switch {
	case err == nil || errors.Is(err, exec.ErrWaitDelay):
		return Result{Output: out.output() + "\n[exit 0]", Brief: "exit 0"}, nil
	case errors.As(err, &ee):
		return Result{Output: fmt.Sprintf("%s\n[exit %d]", out.output(), ee.ExitCode()),
			Brief: fmt.Sprintf("exit %d", ee.ExitCode())}, nil
	}
	return Result{}, fmt.Errorf("start shell: %w", err)
}

// killResult kills the process group, waits until it is reaped and reports why.
func killResult(cmd *exec.Cmd, out *ring, w *sinkWriter, done <-chan error, why string) Result {
	killTree(cmd.Process)
	<-done
	w.stop()
	status := "killed: " + why
	return Result{Output: out.output() + "\n[" + status + "]", Brief: status}
}

// adoptResult hands the running process to the job manager and describes it; format has one %s
// for the job id; killOnFail stops the process when it cannot be adopted. The output so far is returned and counted as read.
func adoptResult(jobs *Jobs, cmd *exec.Cmd, command string, out *ring, done <-chan error, format string, killOnFail bool) (Result, error) {
	id, err := jobs.Adopt(cmd, command, out, done)
	if err != nil {
		if killOnFail { // a background start has no foreground to fall back to
			killTree(cmd.Process)
			<-done
		}
		return Result{}, err
	}
	body, _, _ := jobs.Read(id)
	line := fmt.Sprintf("["+format+"]", id)
	if body != "" {
		line = body + "\n" + line
	}
	return Result{Output: line, Brief: "background " + id}, nil
}

// defaultShellCommand picks the platform shell and builds its command.
func defaultShellCommand(ctx context.Context, env Env, command string) *exec.Cmd {
	name := env.Shell
	if name == "" {
		goos := env.OS
		if goos == "" {
			goos = runtime.GOOS
		}
		name = "sh"
		if goos == "windows" {
			name = "cmd"
			if _, err := exec.LookPath("pwsh"); err == nil {
				name = "pwsh"
			}
		}
	}
	switch name {
	case "pwsh":
		return exec.CommandContext(ctx, "pwsh", "-NoProfile", "-NonInteractive", "-Command", command)
	case "cmd":
		cmd := exec.CommandContext(ctx, "cmd")
		setCmdLine(cmd, command)
		return cmd
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

// sinkWriter stores the output in the ring and forwards it to the live sink: at most one call
// per progressEvery, the rest is held back and flushed by stop. After stop (or in
// background mode, where sink is nil) nothing is emitted.
type sinkWriter struct {
	out     *ring
	mu      sync.Mutex
	sink    func(string)
	pending []byte
	last    time.Time
}

// Write implements io.Writer; it never fails so the process is never blocked.
func (w *sinkWriter) Write(p []byte) (int, error) {
	w.out.Write(p)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sink != nil {
		w.pending = append(w.pending, p...)
		if time.Since(w.last) >= progressEvery {
			w.flush()
		}
	}
	return len(p), nil
}

// flush emits the pending bytes. Caller holds w.mu.
func (w *sinkWriter) flush() {
	if len(w.pending) > 0 {
		w.sink(string(w.pending))
		w.pending = w.pending[:0]
	}
	w.last = time.Now()
}

// stop flushes what is pending and silences the sink for good; Progress is never called after.
func (w *sinkWriter) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sink != nil {
		w.flush()
		w.sink = nil
	}
}

// resume re-attaches the sink after a failed detach.
func (w *sinkWriter) resume(sink func(string)) {
	w.mu.Lock()
	w.sink = sink
	w.mu.Unlock()
}
