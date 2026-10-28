package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helper process
// ---------------------------------------------------------------------------

// TestMain turns the test binary into a helper when SWARG_TEST_HELPER is set:
// exit3, block, echo, grandchild (prints "pid N"), big, pwd, plus the modes below that
// read their stdin: gated, partial, exit5, bigafter. seq and lines only print.
func TestMain(m *testing.M) {
	switch os.Getenv("SWARG_TEST_HELPER") {
	case "":
		os.Exit(m.Run())
	case "exit3":
		os.Exit(3)
	case "block":
		time.Sleep(time.Hour)
	case "echo":
		fmt.Println("out")
		fmt.Fprintln(os.Stderr, "err")
	case "pwd":
		wd, _ := os.Getwd()
		fmt.Println(wd)
	case "big":
		chunk := []byte(strings.Repeat("x", 1023) + "\n")
		for i := 0; i < 3*1024; i++ {
			os.Stdout.Write(chunk)
		}
	case "grandchild": // child keeps our stdout open, then both block
		c := exec.Command(os.Args[0])
		c.Env = helperEnv("block")
		c.Stdout = os.Stdout
		if c.Start() != nil {
			os.Exit(9)
		}
		fmt.Printf("pid %d\n", c.Process.Pid)
		time.Sleep(time.Hour)
	case "gated": // one, wait for a line, two, wait for EOF, three
		in := bufio.NewReader(os.Stdin)
		fmt.Println("one")
		in.ReadString('\n')
		fmt.Println("two")
		io.Copy(io.Discard, in)
		fmt.Println("three")
	case "partial": // "ab" without newline, wait for a line, "c\nd" without trailing newline
		fmt.Print("ab")
		bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Print("c\nd")
	case "exit5":
		bufio.NewReader(os.Stdin).ReadString('\n')
		os.Exit(5)
	case "bigafter": // first, wait for a line, 3 MB of 1 KB lines ending with LAST
		fmt.Println("first")
		bufio.NewReader(os.Stdin).ReadString('\n')
		chunk := []byte(strings.Repeat("x", 1023) + "\n")
		for i := 0; i < 3*1024-1; i++ {
			os.Stdout.Write(chunk)
		}
		fmt.Println(strings.Repeat("y", 1018) + "LAST")
	case "seq": // 50000 numbered lines
		w := bufio.NewWriter(os.Stdout)
		for i := 0; i < seqLines; i++ {
			fmt.Fprintf(w, "%06d\n", i)
		}
		w.Flush()
	case "lines": // many short lines, one write each
		for i := 0; i < 1000; i++ { // stays under MaxOutput
			fmt.Printf("line %d\n", i)
		}
	}
}

const seqLines = 50000

// helperEnv is the environment of a helper process. The race detector sleeps 1 s at exit by
// default; GORACE turns that off so the helper tests stay fast.
func helperEnv(mode string) []string {
	return append(os.Environ(), "SWARG_TEST_HELPER="+mode, "GORACE=atexit_sleep_ms=0")
}

// useHelper makes Shell run this test binary in the given helper mode.
func useHelper(t *testing.T) {
	old := shellCommand
	shellCommand = func(ctx context.Context, _ Env, mode string) *exec.Cmd {
		c := exec.CommandContext(ctx, os.Args[0])
		c.Env = helperEnv(mode)
		return c
	}
	t.Cleanup(func() { shellCommand = old })
}

func runShell(ctx context.Context, env Env, cmd string) (Result, error) {
	args, _ := json.Marshal(map[string]string{"command": cmd})
	return Shell{}.Run(ctx, env, args)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestShellOutputAndExit(t *testing.T) {
	useHelper(t)
	res, err := runShell(context.Background(), Env{Dir: t.TempDir()}, "echo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "out") || !strings.Contains(res.Output, "err") {
		t.Errorf("stdout and stderr must both be present: %q", res.Output)
	}
	if !strings.HasSuffix(res.Output, "\n[exit 0]") || res.Brief != "exit 0" {
		t.Errorf("got %q / %q", res.Output, res.Brief)
	}
}

func TestShellExitCode(t *testing.T) {
	useHelper(t)
	res, err := runShell(context.Background(), Env{Dir: t.TempDir()}, "exit3")
	if err != nil {
		t.Fatalf("non-zero exit is not an error: %v", err)
	}
	if !strings.HasSuffix(res.Output, "[exit 3]") || res.Brief != "exit 3" {
		t.Errorf("got %q / %q", res.Output, res.Brief)
	}
}

func TestShellCwd(t *testing.T) {
	useHelper(t)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	res, err := runShell(context.Background(), Env{Dir: dir}, "pwd")
	if err != nil {
		t.Fatal(err)
	}
	got, _, _ := strings.Cut(res.Output, "\n")
	if got, _ = filepath.EvalSymlinks(strings.TrimSpace(got)); got != dir {
		t.Errorf("cwd %q, want %q", got, dir)
	}
}

func TestShellTimeout(t *testing.T) {
	useHelper(t)
	start := time.Now()
	res, err := runShell(context.Background(), Env{Dir: t.TempDir(), ShellTimeout: 100 * time.Millisecond}, "block")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.Output, "[killed: timeout]") || res.Brief != "killed: timeout" {
		t.Errorf("got %q / %q", res.Output, res.Brief)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestShellKillsGrandchild(t *testing.T) {
	useHelper(t)
	start := time.Now()
	res, err := runShell(context.Background(), Env{Dir: t.TempDir(), ShellTimeout: 200 * time.Millisecond}, "grandchild")
	if err != nil {
		t.Fatal(err)
	}
	if res.Brief != "killed: timeout" {
		t.Errorf("got %q", res.Brief)
	}
	// Process-group kill closes the grandchild's pipe at once; WaitDelay (2 s) is the fallback.
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Errorf("grandchild kept Run waiting for %v", d)
	}
}

func TestShellCancel(t *testing.T) {
	useHelper(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	res, err := runShell(ctx, Env{Dir: t.TempDir()}, "block")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.Output, "[killed: cancelled]") || res.Brief != "killed: cancelled" {
		t.Errorf("got %q / %q", res.Output, res.Brief)
	}
}

func TestShellOutputBounded(t *testing.T) {
	useHelper(t)
	res, err := runShell(context.Background(), Env{Dir: t.TempDir()}, "big")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Output) > MaxOutput {
		t.Errorf("output %d bytes, over MaxOutput", len(res.Output))
	}
	// 3 MB written, 1 MB kept: the note counts everything left out; the tail and the status survive.
	if !strings.Contains(res.Output, "bytes cut;") || !strings.HasSuffix(res.Output, "\n[exit 0]") {
		t.Errorf("missing cap note or exit line: ...%q", res.Output[len(res.Output)-80:])
	}
}

// R-48: the cut note of a command tells how to get the rest of a command, not of a file.
func TestShellOutputBounded_NoteSaysRerun(t *testing.T) {
	useHelper(t)
	res, err := runShell(context.Background(), Env{Dir: t.TempDir()}, "big")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Output, "read_file") || !strings.Contains(res.Output, "rerun with a filter") {
		t.Errorf("wrong cut note on shell output: %q", res.Output[:200])
	}
}

func TestShellEmptyAndBadArgs(t *testing.T) {
	for _, a := range []string{`{"command":""}`, `{"command":"  "}`, `{}`, `nope`} {
		if _, err := (Shell{}).Run(context.Background(), Env{}, json.RawMessage(a)); err == nil {
			t.Errorf("%s: want error", a)
		}
	}
}

func TestShellSpecAndSummary(t *testing.T) {
	s := Shell{}
	if s.Spec().Name != "shell" || s.ReadOnly() || !s.Confirm() {
		t.Error("bad spec or flags")
	}
	if n := len(strings.Split(s.Block(), "\n")); n < 2 || n > 4 {
		t.Errorf("block has %d lines", n)
	}
	if got := s.Summary(json.RawMessage(`{"command":"ls -l\nwc"}`)); got != "ls -l" {
		t.Errorf("summary %q", got)
	}
	long, _ := json.Marshal(map[string]string{"command": strings.Repeat("a", 200)})
	if got := s.Summary(long); len(got) != 80 {
		t.Errorf("summary length %d", len(got))
	}
	if got := s.Summary(json.RawMessage(`bad`)); got != "" {
		t.Errorf("bad JSON summary %q", got)
	}
}

// TestShellRealShell runs a real command through the platform shell.
func TestShellRealShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			if _, err := exec.LookPath("cmd"); err != nil {
				t.Skip("no pwsh or cmd")
			}
		}
	}
	res, err := runShell(context.Background(), Env{Dir: t.TempDir()}, "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Output, "hi") || !strings.HasSuffix(res.Output, "[exit 0]") {
		t.Errorf("got %q", res.Output)
	}
}

// ---------------------------------------------------------------------------
// Gated helper: a command the test controls through stdin
// ---------------------------------------------------------------------------

// gate is the stdin pipe of the helper process started by Shell.
type gate struct {
	ready chan struct{} // closed once the process was built
	w     io.WriteCloser
}

// useGated makes Shell run the helper in the given stdin-driven mode (gated, partial, ...).
func useGated(t *testing.T, mode string) *gate {
	g := &gate{ready: make(chan struct{})}
	old := shellCommand
	shellCommand = func(ctx context.Context, _ Env, _ string) *exec.Cmd {
		c := exec.CommandContext(ctx, os.Args[0])
		c.Env = helperEnv(mode)
		g.w, _ = c.StdinPipe()
		close(g.ready)
		return c
	}
	t.Cleanup(func() {
		shellCommand = old
		if g.w != nil {
			g.w.Close()
		}
	})
	return g
}

func (g *gate) line(t *testing.T) {
	t.Helper()
	select {
	case <-g.ready:
	case <-time.After(testWait):
		t.Fatal("helper was not started")
	}
	io.WriteString(g.w, "\n")
}

// progress collects Progress chunks and signals the first one.
type progress struct {
	mu     sync.Mutex
	chunks []string
	times  []time.Time
	first  chan struct{}
	after  atomic.Bool // set by the test once Run returned
	t      *testing.T
}

func newProgress(t *testing.T) *progress { return &progress{first: make(chan struct{}), t: t} }

func (p *progress) add(c string) {
	if p.after.Load() {
		p.t.Error("Progress called after Run returned")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.chunks) == 0 {
		close(p.first)
	}
	p.chunks = append(p.chunks, c)
	p.times = append(p.times, time.Now())
}

func (p *progress) text() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.chunks, "")
}

func (p *progress) waitFirst(t *testing.T) {
	t.Helper()
	select {
	case <-p.first:
	case <-time.After(testWait):
		t.Fatal("no Progress chunk")
	}
}

// runAsync runs Shell in a goroutine and returns the channel with its outcome.
type outcome struct {
	res Result
	err error
}

func runAsync(ctx context.Context, env Env, args string) <-chan outcome {
	ch := make(chan outcome, 1)
	go func() {
		res, err := Shell{}.Run(ctx, env, json.RawMessage(args))
		ch <- outcome{res, err}
	}()
	return ch
}

func await(t *testing.T, ch <-chan outcome) Result {
	t.Helper()
	select {
	case o := <-ch:
		if o.err != nil {
			t.Fatal(o.err)
		}
		return o.res
	case <-time.After(testWait):
		t.Fatal("Run did not return")
	}
	return Result{}
}

func testEnv(t *testing.T) Env { return Env{Dir: t.TempDir(), Jobs: newJobs(t)} }

// ---------------------------------------------------------------------------
// Progress
// ---------------------------------------------------------------------------

func TestShellProgress_Chunks(t *testing.T) {
	useHelper(t)
	p := newProgress(t)
	env := Env{Dir: t.TempDir(), Progress: p.add}
	start := time.Now()
	res, err := runShell(context.Background(), env, "lines")
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	p.after.Store(true)
	if got, want := strings.TrimRight(p.text(), "\r\n"), strings.TrimSuffix(res.Output, "\n[exit 0]"); got != want {
		t.Errorf("chunks differ from output: %d vs %d bytes", len(got), len(want))
	}
	if n, max := len(p.chunks), int(elapsed/progressEvery)+2; n > max {
		t.Errorf("%d Progress calls in %v, at most %d expected", n, elapsed, max)
	}
	for i := 1; i < len(p.times); i++ { // the final flush may come early, all others are spaced
		if gap := p.times[i].Sub(p.times[i-1]); gap < progressEvery && i < len(p.times)-1 {
			t.Errorf("chunks %d and %d are only %v apart", i-1, i, gap)
		}
	}
}

func TestShellProgress_PartialFlushedAtEnd(t *testing.T) {
	g := useGated(t, "partial")
	p := newProgress(t)
	env := Env{Dir: t.TempDir(), Progress: p.add}
	ch := runAsync(context.Background(), env, `{"command":"x"}`)
	p.waitFirst(t) // "ab" goes out at once
	g.line(t)
	res := await(t, ch)
	p.after.Store(true)
	if got := p.text(); got != "abc\nd" {
		t.Errorf("chunks %q, want %q (the unterminated tail must be delivered)", got, "abc\nd")
	}
	if !strings.HasSuffix(res.Output, "d\n[exit 0]") {
		t.Errorf("output %q", res.Output)
	}
}

func TestShellProgress_NoEmitAfterReturn(t *testing.T) {
	g := useGated(t, "gated")
	p := newProgress(t)
	env := testEnv(t)
	env.Progress = p.add
	detach := make(chan struct{})
	env.Detach = detach
	ch := runAsync(context.Background(), env, `{"command":"x"}`)
	p.waitFirst(t)
	close(detach)
	res := await(t, ch)
	p.after.Store(true)
	g.line(t)
	g.w.Close()
	waitDone(t, env.Jobs, "bg1") // more output was produced after the return
	if !strings.Contains(res.Output, "moved to background as bg1 by the user") {
		t.Errorf("not detached: %q", res.Output)
	}
}

func TestShellProgress_DoesNotChangeResult(t *testing.T) {
	useHelper(t)
	plain, err := runShell(context.Background(), Env{Dir: t.TempDir()}, "echo")
	if err != nil {
		t.Fatal(err)
	}
	live, err := runShell(context.Background(), Env{Dir: t.TempDir(), Progress: func(string) {}}, "echo")
	if err != nil {
		t.Fatal(err)
	}
	if plain != live {
		t.Errorf("%+v vs %+v", plain, live)
	}
}

// ---------------------------------------------------------------------------
// Background
// ---------------------------------------------------------------------------

func TestShellBackground_StartsAndReturnsAtOnce(t *testing.T) {
	g := useGated(t, "gated")
	env := testEnv(t)
	res, err := Shell{}.Run(context.Background(), env, json.RawMessage(`{"command":"x","background":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, `started in background as bg1; read it with shell_output {"id": "bg1"}`) || res.Brief != "background bg1" {
		t.Errorf("got %q / %q", res.Output, res.Brief)
	}
	if l := env.Jobs.List(); len(l) != 1 || !l[0].Running {
		t.Errorf("jobs %+v", l)
	}
	<-g.ready
	g.w.Close()
	waitDone(t, env.Jobs, "bg1")
	if out, _, _ := env.Jobs.Read("bg1"); !strings.Contains(out, "one\n") {
		t.Errorf("job output %q", out)
	}
}

func TestShellBackground_NoJobsIsError(t *testing.T) {
	started := false
	old := shellCommand
	shellCommand = func(ctx context.Context, env Env, c string) *exec.Cmd { started = true; return old(ctx, env, c) }
	t.Cleanup(func() { shellCommand = old })
	_, err := Shell{}.Run(context.Background(), Env{Dir: t.TempDir()}, json.RawMessage(`{"command":"echo","background":true}`))
	if err == nil || started {
		t.Errorf("err %v, process started %v", err, started)
	}
}

func TestShellBackground_TimeoutNotApplied(t *testing.T) {
	useHelper(t)
	env := testEnv(t)
	env.ShellTimeout = time.Nanosecond
	if _, err := (Shell{}).Run(context.Background(), env, json.RawMessage(`{"command":"echo","background":true}`)); err != nil {
		t.Fatal(err)
	}
	waitDone(t, env.Jobs, "bg1")
	out, info, _ := env.Jobs.Read("bg1")
	if info.ExitCode != 0 || !strings.Contains(out, "out") {
		t.Errorf("job %+v output %q", info, out)
	}
}

func TestShellBackground_SurvivesCallContext(t *testing.T) {
	g := useGated(t, "gated")
	env := testEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := (Shell{}).Run(ctx, env, json.RawMessage(`{"command":"x","background":true}`)); err != nil {
		t.Fatal(err)
	}
	cancel()
	<-g.ready
	g.line(t)
	g.w.Close()
	waitDone(t, env.Jobs, "bg1")
	out, info, _ := env.Jobs.Read("bg1")
	if info.ExitCode != 0 || !strings.HasSuffix(out, "three\n") {
		t.Errorf("job killed by the call context: %+v %q", info, out)
	}
}

func TestShellBackground_SpecAndBlock(t *testing.T) {
	if !strings.Contains(string(Shell{}.Spec().Schema), `"background"`) || !strings.Contains(Shell{}.Block(), "background") {
		t.Error("shell must advertise background")
	}
	if got := (Shell{}).Summary(json.RawMessage(`{"command":"sleep 9","background":true}`)); got != "sleep 9" {
		t.Errorf("summary %q", got)
	}
}

// ---------------------------------------------------------------------------
// Detach
// ---------------------------------------------------------------------------

func TestShellDetach_MidRunReturnsAndJobKeepsRunning(t *testing.T) {
	g := useGated(t, "gated")
	p := newProgress(t)
	env := testEnv(t)
	env.Progress = p.add
	detach := make(chan struct{})
	env.Detach = detach
	ch := runAsync(context.Background(), env, `{"command":"x"}`)
	p.waitFirst(t)
	close(detach)
	res := await(t, ch)
	if !strings.HasPrefix(res.Output, "one\n") || !strings.Contains(res.Output, "[moved to background as bg1 by the user]") ||
		res.Brief != "background bg1" {
		t.Errorf("got %q / %q", res.Output, res.Brief)
	}
	g.line(t)
	g.w.Close()
	waitDone(t, env.Jobs, "bg1")
	if out, info, _ := env.Jobs.Read("bg1"); out != "two\nthree\n" || info.ExitCode != 0 {
		t.Errorf("later output %q (%+v): the output returned at detach must not be repeated", out, info)
	}
}

func TestShellDetach_SurvivesCallContext(t *testing.T) {
	g := useGated(t, "gated")
	p := newProgress(t)
	env := testEnv(t)
	env.Progress = p.add
	detach := make(chan struct{})
	env.Detach = detach
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(ctx, env, `{"command":"x"}`)
	p.waitFirst(t)
	close(detach)
	await(t, ch)
	cancel() // the Turn ends: the job must live on
	g.line(t)
	g.w.Close()
	waitDone(t, env.Jobs, "bg1")
	if _, info, _ := env.Jobs.Read("bg1"); info.ExitCode != 0 {
		t.Errorf("job killed by the call context: %+v", info)
	}
}

func TestShellDetach_NoJobsIgnored(t *testing.T) {
	g := useGated(t, "gated")
	p := newProgress(t)
	detach := make(chan struct{})
	close(detach)
	env := Env{Dir: t.TempDir(), Progress: p.add, Detach: detach}
	ch := runAsync(context.Background(), env, `{"command":"x"}`)
	p.waitFirst(t)
	select {
	case <-ch:
		t.Fatal("Run returned although Jobs is nil")
	default:
	}
	g.line(t)
	g.w.Close()
	if res := await(t, ch); !strings.HasSuffix(res.Output, "[exit 0]") {
		t.Errorf("got %q", res.Output)
	}
}

func TestShellDetach_AfterEnd(t *testing.T) {
	useHelper(t)
	env := testEnv(t)
	detach := make(chan struct{})
	env.Detach = detach
	if res, err := runShell(context.Background(), env, "echo"); err != nil || res.Brief != "exit 0" {
		t.Fatalf("%v %+v", err, res)
	}
	close(detach)
	if n := len(env.Jobs.List()); n != 0 {
		t.Errorf("%d jobs", n)
	}
}

// TestShellDetach_RaceWithExit: the command ends at the moment Detach closes. Exactly one
// outcome is valid and nothing may leak or be adopted twice.
func TestShellDetach_RaceWithExit(t *testing.T) {
	useHelper(t)
	for i := 0; i < 5; i++ {
		env := testEnv(t)
		detach := make(chan struct{})
		env.Detach = detach
		go close(detach)
		res, err := runShell(context.Background(), env, "echo")
		if err != nil {
			t.Fatal(err)
		}
		jobs := env.Jobs.List()
		switch {
		case strings.HasSuffix(res.Output, "[exit 0]") && len(jobs) == 0:
		case strings.Contains(res.Output, "moved to background as bg1 by the user") && len(jobs) == 1:
			waitDone(t, env.Jobs, "bg1")
			if info := env.Jobs.List()[0]; info.ExitCode != 0 {
				t.Errorf("adopted job: %+v", info)
			}
		default:
			t.Fatalf("invalid outcome: %q with %d jobs", res.Output, len(jobs))
		}
		env.Jobs.KillAll()
	}
}

func TestShellDetach_RaceWithCancel(t *testing.T) {
	g := useGated(t, "gated")
	p := newProgress(t)
	env := testEnv(t)
	env.Progress = p.add
	detach := make(chan struct{})
	env.Detach = detach
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(ctx, env, `{"command":"x"}`)
	p.waitFirst(t)
	cancel()
	close(detach)
	res := await(t, ch)
	_ = g
	if res.Brief != "killed: cancelled" || len(env.Jobs.List()) != 0 {
		t.Errorf("got %q / %q with %d jobs", res.Output, res.Brief, len(env.Jobs.List()))
	}
}

func TestShellDetach_TooManyJobsKeepsForeground(t *testing.T) {
	g := useGated(t, "gated")
	env := testEnv(t)
	for i := 0; i < maxJobs; i++ { // fill the manager with fake running jobs
		done := make(chan error)
		t.Cleanup(func() { close(done) })
		if _, err := env.Jobs.Adopt(&exec.Cmd{}, "fake", newRing(16), done); err != nil {
			t.Fatal(err)
		}
	}
	p := newProgress(t)
	env.Progress = p.add
	detach := make(chan struct{})
	env.Detach = detach
	ch := runAsync(context.Background(), env, `{"command":"x"}`)
	p.waitFirst(t)
	close(detach)
	g.line(t)
	g.w.Close()
	if res := await(t, ch); !strings.HasSuffix(res.Output, "[exit 0]") {
		t.Errorf("command must finish in the foreground: %q", res.Output)
	}
}

// ---------------------------------------------------------------------------
// Background grace (live run 2026-10-27: Qwen3.5-2B sent even `ls` and `go version` to the
// background and then polled them; a short command must just return its result)
// ---------------------------------------------------------------------------

func TestShellBackground_ShortCommandReturnsAsForeground(t *testing.T) {
	useHelper(t)
	env := testEnv(t)
	env.BackgroundGrace = time.Minute // returns as soon as the command ends, not after the grace
	res, err := Shell{}.Run(context.Background(), env, json.RawMessage(`{"command":"echo","background":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Brief != "exit 0" || !strings.Contains(res.Output, "out") || !strings.HasSuffix(res.Output, "[exit 0]") {
		t.Errorf("got %q / %q, want a normal result", res.Output, res.Brief)
	}
	if l := env.Jobs.List(); len(l) != 0 {
		t.Errorf("a short command became a job: %+v", l)
	}
}

func TestShellBackground_LongCommandAdoptedAfterGrace(t *testing.T) {
	g := useGated(t, "gated")
	env := testEnv(t)
	env.BackgroundGrace = 20 * time.Millisecond
	res, err := Shell{}.Run(context.Background(), env, json.RawMessage(`{"command":"x","background":true}`))
	if err != nil || res.Brief != "background bg1" {
		t.Fatalf("got %q / %q / %v", res.Output, res.Brief, err)
	}
	<-g.ready
	g.w.Close()
	waitDone(t, env.Jobs, "bg1")
}

func TestShellBackground_CancelDuringGraceKills(t *testing.T) {
	g := useGated(t, "gated")
	env := testEnv(t)
	env.BackgroundGrace = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-g.ready; cancel() }()
	res, err := Shell{}.Run(ctx, env, json.RawMessage(`{"command":"x","background":true}`))
	if err != nil || res.Brief != "killed: cancelled" {
		t.Fatalf("got %q / %q / %v", res.Output, res.Brief, err)
	}
	if l := env.Jobs.List(); len(l) != 0 {
		t.Errorf("a cancelled command became a job: %+v", l)
	}
}

// Live run 2026-10-27: a 12 s loop went to the background and the model polled it 17 times.
// During the grace the command behaves like a foreground one: live output and Ctrl+B work.
func TestShellBackground_GraceStreamsProgress(t *testing.T) {
	g := useGated(t, "gated")
	p := newProgress(t)
	env := testEnv(t)
	env.Progress = p.add
	env.BackgroundGrace = time.Minute
	ch := runAsync(context.Background(), env, `{"command":"x","background":true}`)
	p.waitFirst(t)
	g.line(t)
	g.w.Close()
	res := await(t, ch)
	p.after.Store(true)
	if res.Brief != "exit 0" || !strings.HasPrefix(res.Output, "one\ntwo\nthree") {
		t.Errorf("got %q / %q", res.Output, res.Brief)
	}
	if l := env.Jobs.List(); len(l) != 0 {
		t.Errorf("jobs %+v", l)
	}
}

func TestShellBackground_CtrlBDuringGraceAdopts(t *testing.T) {
	g := useGated(t, "gated")
	p := newProgress(t)
	env := testEnv(t)
	env.Progress = p.add
	env.BackgroundGrace = time.Minute
	detach := make(chan struct{})
	env.Detach = detach
	ch := runAsync(context.Background(), env, `{"command":"x","background":true}`)
	p.waitFirst(t)
	close(detach)
	res := await(t, ch)
	p.after.Store(true)
	if res.Brief != "background bg1" {
		t.Errorf("got %q / %q", res.Output, res.Brief)
	}
	g.line(t)
	g.w.Close()
	waitDone(t, env.Jobs, "bg1")
}
