package tools

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Constants and types
// ---------------------------------------------------------------------------

const (
	maxJobs         = 8               // running background jobs at once
	maxFinished     = 16              // finished jobs kept for reading; older ones are evicted
	killAllWait     = 2 * time.Second // KillAll waits this long for the processes to be reaped
	jobReadReserve  = 200             // room left in a tool result for the notes around a Read
	jobStatusKilled = -1              // JobInfo.ExitCode of a job killed by a signal
)

// ErrExited is returned by Kill for a job that already exited (nothing was done).
var ErrExited = errors.New("already exited")

// JobInfo is a snapshot of one background job.
type JobInfo struct {
	ID, Command string
	Running     bool
	ExitCode    int // -1 when the job was killed
	Started     time.Time
	Bytes       int // total output bytes produced so far
}

// Jobs owns the background processes of one swarg run. It is safe for concurrent use.
type Jobs struct {
	mu     sync.Mutex
	list   map[string]*job
	seq    int
	closed bool
}

type job struct {
	seq      int
	id, cmd  string
	proc     *exec.Cmd
	out      *ring
	started  time.Time
	fin      chan struct{} // closed once the process is reaped and its output is complete
	readPos  int64
	exited   bool
	killed   bool
	exitCode int
}

// NewJobs returns an empty manager.
func NewJobs() *Jobs { return &Jobs{list: map[string]*job{}} }

// ---------------------------------------------------------------------------
// Jobs
// ---------------------------------------------------------------------------

// Adopt takes over a started process. out receives the process output; done is the channel
// of the single goroutine that calls cmd.Wait (buffered, receives exactly once). It returns
// the job id ("bg1", "bg2", ...). On error the process is untouched; the caller must kill it.
func (j *Jobs) Adopt(cmd *exec.Cmd, command string, out *ring, done <-chan error) (string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return "", errors.New("background jobs are shut down")
	}
	running := 0
	for _, o := range j.list {
		if !o.exited {
			running++
		}
	}
	if running >= maxJobs {
		return "", fmt.Errorf("too many background jobs (max %d); kill one with shell_kill", maxJobs)
	}
	j.evict()
	j.seq++
	b := &job{seq: j.seq, id: "bg" + strconv.Itoa(j.seq), cmd: command, proc: cmd, out: out,
		started: time.Now(), fin: make(chan struct{})}
	j.list[b.id] = b
	go j.reap(b, done)
	return b.id, nil
}

// reap records the exit of b.
func (j *Jobs) reap(b *job, done <-chan error) {
	err := <-done
	code := 0
	var ee *exec.ExitError
	switch {
	case err == nil || errors.Is(err, exec.ErrWaitDelay):
	case errors.As(err, &ee) && ee.ExitCode() >= 0:
		code = ee.ExitCode()
	default:
		code = jobStatusKilled
	}
	j.mu.Lock()
	b.exited, b.exitCode = true, code
	j.mu.Unlock()
	close(b.fin)
}

// evict drops the oldest finished jobs beyond maxFinished. Caller holds j.mu.
func (j *Jobs) evict() {
	var fin []*job
	for _, b := range j.list {
		if b.exited {
			fin = append(fin, b)
		}
	}
	if len(fin) < maxFinished {
		return
	}
	sort.Slice(fin, func(a, b int) bool { return fin[a].seq < fin[b].seq })
	for _, b := range fin[:len(fin)-maxFinished+1] {
		delete(j.list, b.id)
	}
}

// Read returns the output produced since the previous Read (at most MaxOutput-200 bytes of
// it, then a note to call again) and the job state. Bytes pushed out of the ring before they
// were read are reported as "[... N bytes lost]".
func (j *Jobs) Read(id string) (string, JobInfo, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	b, ok := j.list[id]
	if !ok {
		return "", JobInfo{}, fmt.Errorf("unknown job %q", id)
	}
	data, pos, lost, more := b.out.readFrom(b.readPos, MaxOutput-jobReadReserve)
	b.readPos = pos
	var sb strings.Builder
	if lost > 0 {
		fmt.Fprintf(&sb, "[... %d bytes lost]\n", lost)
	}
	sb.WriteString(data)
	if more > 0 {
		fmt.Fprintf(&sb, "\n[... %d more bytes; call shell_output again]", more)
	}
	return sb.String(), b.info(), nil
}

// Kill kills the process group of a running job and waits (up to 2 s) until it is reaped.
// A job that already exited is left alone and ErrExited is returned.
func (j *Jobs) Kill(id string) error {
	j.mu.Lock()
	b, ok := j.list[id]
	if !ok {
		j.mu.Unlock()
		return fmt.Errorf("unknown job %q", id)
	}
	if b.exited {
		j.mu.Unlock()
		return ErrExited
	}
	b.killed = true
	err := killTree(b.proc.Process)
	j.mu.Unlock()
	select {
	case <-b.fin:
	case <-time.After(killAllWait):
	}
	if err != nil {
		select {
		case <-b.fin: // it died on its own while we were killing it
			return nil
		default:
		}
		return fmt.Errorf("kill %s: %w", id, err)
	}
	return nil
}

// List returns all known jobs in id order.
func (j *Jobs) List() []JobInfo {
	j.mu.Lock()
	defer j.mu.Unlock()
	all := make([]*job, 0, len(j.list))
	for _, b := range j.list {
		all = append(all, b)
	}
	sort.Slice(all, func(a, b int) bool { return all[a].seq < all[b].seq })
	out := make([]JobInfo, len(all))
	for i, b := range all {
		out[i] = b.info()
	}
	return out
}

// Done returns a channel closed when the job has exited and its output is complete. For an
// unknown id (never adopted, or evicted) the channel is already closed.
func (j *Jobs) Done(id string) <-chan struct{} {
	j.mu.Lock()
	defer j.mu.Unlock()
	if b, ok := j.list[id]; ok {
		return b.fin
	}
	c := make(chan struct{})
	close(c)
	return c
}

// KillAll kills every running job, waits up to 2 s for them to be reaped and refuses further
// Adopt calls. Safe to call twice.
func (j *Jobs) KillAll() {
	j.mu.Lock()
	j.closed = true
	var wait []chan struct{}
	for _, b := range j.list {
		if !b.exited {
			b.killed = true
			killTree(b.proc.Process)
			wait = append(wait, b.fin)
		}
	}
	j.mu.Unlock()
	timeout := time.After(killAllWait)
	for _, c := range wait {
		select {
		case <-c:
		case <-timeout:
			return
		}
	}
}

// info snapshots b. Caller holds j.mu.
func (b *job) info() JobInfo {
	code := b.exitCode
	if b.exited && b.killed {
		code = jobStatusKilled // taskkill on Windows reports 1, not a signal
	}
	return JobInfo{ID: b.id, Command: b.cmd, Running: !b.exited, ExitCode: code,
		Started: b.started, Bytes: int(b.out.total())}
}

// ---------------------------------------------------------------------------
// ring: keeps the last N bytes of a process output with absolute offsets
// ---------------------------------------------------------------------------

type ring struct {
	mu    sync.Mutex
	limit int
	data  []byte
	base  int64 // absolute offset of data[0]
}

func newRing(limit int) *ring { return &ring{limit: limit} }

// Write implements io.Writer; it never fails so the process is never blocked.
func (r *ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	if len(p) >= r.limit { // keep only the tail of a huge write
		r.base += int64(len(r.data) + len(p) - r.limit)
		r.data = append(r.data[:0], p[len(p)-r.limit:]...)
		return n, nil
	}
	r.data = append(r.data, p...)
	if len(r.data) >= 2*r.limit { // compact lazily: amortised O(1) per byte
		drop := len(r.data) - r.limit
		r.base += int64(drop)
		r.data = append(r.data[:0], r.data[drop:]...)
	}
	return n, nil
}

// total is the number of bytes ever written.
func (r *ring) total() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.base + int64(len(r.data))
}

// lo is the absolute offset of the oldest kept byte, on a UTF-8 boundary. Caller holds r.mu.
func (r *ring) lo() int64 {
	total := r.base + int64(len(r.data))
	lo := total - int64(r.limit)
	if lo <= r.base {
		return r.base
	}
	for lo < total && !utf8.RuneStart(r.data[lo-r.base]) {
		lo++
	}
	return lo
}

// readFrom returns up to max bytes from absolute offset pos, the new offset, the number of
// bytes that were lost before pos could be read and the number left beyond max.
func (r *ring) readFrom(pos int64, max int) (data string, next, lost int64, more int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := r.base + int64(len(r.data))
	if lo := r.lo(); pos < lo {
		lost, pos = lo-pos, lo
	}
	if pos > total {
		pos = total
	}
	chunk := r.data[pos-r.base:]
	if len(chunk) > max {
		end := max
		for end > 0 && !utf8.RuneStart(chunk[end]) {
			end--
		}
		more = len(chunk) - end
		chunk = chunk[:end]
	}
	return string(chunk), pos + int64(len(chunk)), lost, more
}

// output returns everything kept as a tool result body: trailing newlines removed, cut to fit
// the result, with a note of how many bytes were left out (lost to the ring or cut here).
func (r *ring) output() string {
	s, _, lost, _ := r.readFrom(0, 1<<30)
	dropped := int(lost)
	s = strings.TrimRight(s, "\r\n")
	head, tail, cut := cutMiddle(s, shellBodyLimit)
	if cut > 0 {
		s = head + "\n" + cutNote(cut+dropped, hintCmd) + "\n" + tail
	} else if dropped > 0 { // only the ring lost bytes (the oldest ones)
		s = cutNote(dropped, hintCmd) + "\n" + s
	}
	return s
}
