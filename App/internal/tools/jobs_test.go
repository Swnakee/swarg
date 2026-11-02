package tools

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const testWait = 20 * time.Second

// proc is a helper process started by the test, not yet adopted.
type proc struct {
	cmd   *exec.Cmd
	ring  *ring
	done  chan error
	stdin io.WriteCloser
	wrote chan struct{} // one token per output write, non-blocking
}

// tee writes into the ring and signals the test.
type tee struct{ p *proc }

func (t tee) Write(b []byte) (int, error) {
	n, err := t.p.ring.Write(b)
	select {
	case t.p.wrote <- struct{}{}:
	default:
	}
	return n, err
}

// spawn starts the test binary in a helper mode with a stdin pipe and its own process group.
func spawn(t *testing.T, mode string) *proc {
	t.Helper()
	c := exec.Command(os.Args[0])
	c.Env = helperEnv(mode)
	setProcessGroup(c)
	in, err := c.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	p := &proc{cmd: c, ring: newRing(shellBufLimit), done: make(chan error, 1), stdin: in,
		wrote: make(chan struct{}, 1)}
	c.Stdout, c.Stderr = tee{p}, tee{p}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.done <- c.Wait() }()
	t.Cleanup(func() { killTree(c.Process); in.Close() })
	return p
}

// waitBytes blocks until the process has written at least n bytes.
func (p *proc) waitBytes(t *testing.T, n int64) {
	t.Helper()
	for p.ring.total() < n {
		select {
		case <-p.wrote:
		case <-time.After(testWait):
			t.Fatalf("timeout waiting for %d bytes, have %d", n, p.ring.total())
		}
	}
}

// newJobs returns a Jobs that is cleaned up at the end of the test.
func newJobs(t *testing.T) *Jobs {
	j := NewJobs()
	t.Cleanup(j.KillAll)
	return j
}

// adoptFinished adopts a process-less job that has already exited with code 0 (no spawn cost).
func adoptFinished(t *testing.T, j *Jobs) string {
	t.Helper()
	done := make(chan error, 1)
	done <- nil
	id, err := j.Adopt(&exec.Cmd{}, "fake", newRing(16), done)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, j, id)
	return id
}

// start spawns a helper and adopts it.
func start(t *testing.T, j *Jobs, mode string) (*proc, string) {
	t.Helper()
	p := spawn(t, mode)
	id, err := j.Adopt(p.cmd, mode, p.ring, p.done)
	if err != nil {
		t.Fatal(err)
	}
	return p, id
}

func waitDone(t *testing.T, j *Jobs, id string) {
	t.Helper()
	select {
	case <-j.Done(id):
	case <-time.After(testWait):
		t.Fatalf("timeout waiting for %s to finish", id)
	}
}

func mustRead(t *testing.T, j *Jobs, id string) (string, JobInfo) {
	t.Helper()
	out, info, err := j.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	return out, info
}

// splitRead separates the notes of a Read result from the body.
func splitRead(s string) (lost int, body string, more int) {
	if strings.HasPrefix(s, "[... ") {
		if line, rest, ok := strings.Cut(s, "\n"); ok && strings.HasSuffix(line, " bytes lost]") {
			fmt.Sscanf(line, "[... %d bytes lost]", &lost)
			s = rest
		}
	}
	if i := strings.LastIndex(s, "\n[... "); i >= 0 && strings.HasSuffix(s, "; call shell_output again]") {
		fmt.Sscanf(s[i+1:], "[... %d more bytes;", &more)
		s = s[:i]
	}
	return lost, s, more
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestJobs_AdoptReturnsSequentialIDs(t *testing.T) {
	j := newJobs(t)
	for i := 1; i <= 3; i++ {
		id := adoptFinished(t, j)
		if want := fmt.Sprintf("bg%d", i); id != want {
			t.Errorf("got %q, want %q", id, want)
		}
		waitDone(t, j, id)
	}
}

func TestJobs_ReadReturnsOnlyNewOutput(t *testing.T) {
	j := newJobs(t)
	p, id := start(t, j, "gated")
	p.waitBytes(t, 4)
	if out, info := mustRead(t, j, id); out != "one\n" || !info.Running {
		t.Errorf("got %q running=%v", out, info.Running)
	}
	if out, _ := mustRead(t, j, id); out != "" {
		t.Errorf("second read returned %q", out)
	}
	io.WriteString(p.stdin, "\n")
	p.waitBytes(t, 8)
	if out, _ := mustRead(t, j, id); out != "two\n" {
		t.Errorf("got %q", out)
	}
	p.stdin.Close()
	waitDone(t, j, id)
	out, info := mustRead(t, j, id)
	if out != "three\n" || info.Running || info.ExitCode != 0 {
		t.Errorf("got %q %+v", out, info)
	}
	if out, _ := mustRead(t, j, id); out != "" {
		t.Errorf("read after the end returned %q", out)
	}
}

func TestJobs_ReadPartialLine(t *testing.T) {
	j := newJobs(t)
	p, id := start(t, j, "partial")
	p.waitBytes(t, 2)
	if out, _ := mustRead(t, j, id); out != "ab" {
		t.Errorf("got %q", out)
	}
	io.WriteString(p.stdin, "\n")
	waitDone(t, j, id)
	if out, _ := mustRead(t, j, id); out != "c\nd" {
		t.Errorf("got %q", out)
	}
}

// readAll reads until the job has nothing more; it returns the lost total and the bodies.
func readAll(t *testing.T, j *Jobs, id string) (lost int, bodies []string) {
	t.Helper()
	for {
		out, _ := mustRead(t, j, id)
		l, body, more := splitRead(out)
		lost += l
		bodies = append(bodies, body)
		if more == 0 {
			return lost, bodies
		}
	}
}

func TestJobs_ReadAfterCapDrop(t *testing.T) {
	j := newJobs(t)
	p, id := start(t, j, "bigafter")
	p.waitBytes(t, 6)
	if out, _ := mustRead(t, j, id); out != "first\n" {
		t.Fatalf("got %q", out)
	}
	io.WriteString(p.stdin, "\n")
	waitDone(t, j, id)
	first, _ := mustRead(t, j, id)
	if l, _, _ := splitRead(first); l <= 0 {
		t.Fatalf("no lost-bytes marker: %.60q", first)
	}
	// Rewind is impossible (offset advanced), so re-read the rest and check the tail.
	_, bodies := readAll(t, j, id)
	if last := bodies[len(bodies)-1]; !strings.HasSuffix(last, "LAST\n") {
		t.Errorf("tail does not end with LAST: ...%q", last[max(0, len(last)-40):])
	}
}

func TestJobs_ReadCappedToMaxOutput(t *testing.T) {
	j := newJobs(t)
	p, id := start(t, j, "bigafter")
	io.WriteString(p.stdin, "\n")
	waitDone(t, j, id)
	total := j.List()[0].Bytes
	lost, bodies := readAll(t, j, id)
	sum := 0
	for _, b := range bodies {
		if len(b) > MaxOutput-200 {
			t.Errorf("body of %d bytes exceeds MaxOutput-200", len(b))
		}
		sum += len(b)
	}
	if len(bodies) < 2 {
		t.Fatal("expected continuation reads")
	}
	if lost <= 0 || lost+sum != total {
		t.Errorf("lost %d + read %d != written %d: the offset must advance only by what was returned", lost, sum, total)
	}
	// Each Read result, notes included, stays within the tool result cap.
	out, _ := mustRead(t, j, id)
	if out != "" {
		t.Errorf("job should be drained, got %q", out)
	}
}

func TestJobs_ReadWhileWriting(t *testing.T) {
	j := newJobs(t)
	_, id := start(t, j, "seq")
	var got strings.Builder
	done := j.Done(id)
	for {
		finished := false
		select {
		case <-done:
			finished = true
		default:
		}
		out, _ := mustRead(t, j, id) // the pass that starts after Done drains the rest
		_, body, more := splitRead(out)
		got.WriteString(body)
		if finished && more == 0 {
			break
		}
	}
	var want strings.Builder
	for i := 0; i < seqLines; i++ {
		fmt.Fprintf(&want, "%06d\n", i)
	}
	if got.String() != want.String() {
		t.Errorf("concatenated reads differ: got %d bytes, want %d", got.Len(), want.Len())
	}
}

func TestJobs_ReadUnknownID(t *testing.T) {
	j := newJobs(t)
	start(t, j, "echo")
	for _, id := range []string{"bg9", "", "BG1", "1"} {
		_, _, err := j.Read(id)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", id)) {
			t.Errorf("Read(%q): err %v", id, err)
		}
	}
}

func TestJobs_StatusAndExitCode(t *testing.T) {
	j := newJobs(t)
	p, id := start(t, j, "exit5")
	if info := j.List()[0]; !info.Running || info.ID != id || info.Command != "exit5" || info.Started.IsZero() {
		t.Errorf("running job: %+v", info)
	}
	io.WriteString(p.stdin, "\n")
	waitDone(t, j, id)
	if info := j.List()[0]; info.Running || info.ExitCode != 5 {
		t.Errorf("finished job: %+v", info)
	}
	_, info := mustRead(t, j, id)
	if info.Running || info.ExitCode != 5 {
		t.Errorf("Read info: %+v", info)
	}
}

func TestJobs_KillTwiceAndFinished(t *testing.T) {
	j := newJobs(t)
	_, id := start(t, j, "block")
	if err := j.Kill(id); err != nil {
		t.Fatal(err)
	}
	waitDone(t, j, id)
	if info := j.List()[0]; info.Running {
		t.Errorf("still running after Kill: %+v", info)
	}
	if err := j.Kill(id); !errors.Is(err, ErrExited) {
		t.Errorf("second Kill: %v", err)
	}
	_, id2 := start(t, j, "echo")
	waitDone(t, j, id2)
	if err := j.Kill(id2); !errors.Is(err, ErrExited) {
		t.Errorf("Kill of a finished job: %v", err)
	}
	if err := j.Kill("bg99"); err == nil || errors.Is(err, ErrExited) {
		t.Errorf("Kill of an unknown id: %v", err)
	}
}

// R-15
func TestJobs_KillAll(t *testing.T) {
	j := newJobs(t)
	var ids []string
	for i := 0; i < 3; i++ {
		_, id := start(t, j, "block")
		ids = append(ids, id)
	}
	j.KillAll()
	for _, id := range ids { // KillAll waits, so Done is already closed
		select {
		case <-j.Done(id):
		default:
			t.Errorf("%s not finished after KillAll", id)
		}
	}
	j.KillAll() // twice is safe
	NewJobs().KillAll()
	p := spawn(t, "block")
	if _, err := j.Adopt(p.cmd, "late", p.ring, p.done); err == nil {
		t.Error("Adopt after KillAll must fail")
	}
}

func TestJobs_MaxEight(t *testing.T) {
	j := newJobs(t)
	for i := 0; i < maxJobs; i++ {
		start(t, j, "block")
	}
	p := spawn(t, "block")
	if _, err := j.Adopt(p.cmd, "ninth", p.ring, p.done); err == nil || !strings.Contains(err.Error(), "8") {
		t.Errorf("9th Adopt: %v", err)
	}
	// A finished job frees its slot.
	if err := j.Kill("bg1"); err != nil {
		t.Fatal(err)
	}
	waitDone(t, j, "bg1")
	if _, err := j.Adopt(p.cmd, "ninth", p.ring, p.done); err != nil {
		t.Errorf("Adopt after a slot was freed: %v", err)
	}
}

func TestJobs_ListOrder(t *testing.T) {
	j := newJobs(t)
	for i := 0; i < 11; i++ {
		adoptFinished(t, j)
	}
	list := j.List()
	if len(list) != 11 {
		t.Fatalf("got %d jobs", len(list))
	}
	for i, info := range list {
		if want := fmt.Sprintf("bg%d", i+1); info.ID != want {
			t.Errorf("position %d: %s, want %s", i, info.ID, want)
		}
	}
}

func TestJobs_DoneOfUnknownIDIsClosed(t *testing.T) {
	select {
	case <-NewJobs().Done("bg1"):
	default:
		t.Error("Done of an unknown id must not block")
	}
}

func TestJobs_FinishedJobsAreEvicted(t *testing.T) {
	j := newJobs(t)
	for i := 0; i < maxFinished+5; i++ {
		adoptFinished(t, j)
	}
	if n := len(j.List()); n > maxFinished+1 {
		t.Errorf("%d jobs kept", n)
	}
	if _, _, err := j.Read("bg1"); err == nil {
		t.Error("the oldest finished job should be gone")
	}
}

func TestJobs_Concurrent(t *testing.T) {
	j := newJobs(t)
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := spawn(t, "echo")
			id, err := j.Adopt(p.cmd, "echo", p.ring, p.done)
			if err != nil {
				return // too many running at once is fine here
			}
			j.List()
			j.Read(id)
			j.Kill(id)
			<-j.Done(id)
			j.Read(id)
		}()
	}
	wg.Wait()
}
