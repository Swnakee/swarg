// Package mcp is the MCP client of swarg: it starts the configured MCP Servers, offers their
// tools to the model as `mcp__<server>__<tool>` and runs the calls (R-85..R-88). Protocol:
// JSON-RPC 2.0, one message per line over stdio (specification 2025-11-25). A failing server
// never stops swarg (I-31).
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"swarg/internal/config"
	"swarg/internal/keys"
)

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

// Transport carries JSON-RPC messages to and from one MCP Server.
type Transport interface {
	Send(msg []byte) error
	Recv() <-chan []byte // one message per element; closed when the server is gone
	Close() error        // ends the server; idempotent
	Stderr() string      // the tail of the server's stderr, "" when none
}

const (
	stderrTail  = 4096             // bytes of stderr kept per server
	maxLine     = 32 << 20         // longest message accepted
	closeGrace  = 2 * time.Second  // Close waits this long before killing
	startLimit  = 10 * time.Second // handshake and tools/list
	readBufSize = 64 * 1024
)

// stdioTransport is a subprocess with newline-delimited JSON-RPC on stdin/stdout.
type stdioTransport struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    chan []byte
	errs   *tail
	exited chan struct{}
	once   sync.Once
	wmu    sync.Mutex
}

// dialStdio starts the server program in dir with the configured args and environment.
func dialStdio(dir string, s config.MCPServer) (Transport, error) {
	cmd := exec.Command(s.Command, s.Args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for k, v := range s.Env {
		cmd.Env = append(cmd.Env, k+"="+keys.User().Expand(v))
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	t := &stdioTransport{cmd: cmd, in: in, out: make(chan []byte, 16), errs: &tail{}, exited: make(chan struct{})}
	cmd.Stderr = t.errs
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go t.read(out)
	go func() { cmd.Wait(); close(t.exited) }()
	return t, nil
}

// read forwards stdout lines; a line that is not a JSON object ends the stream (the spec forbids
// anything but MCP messages on stdout) and the server counts as crashed.
func (t *stdioTransport) read(r io.Reader) {
	defer close(t.out)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, readBufSize), maxLine)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if !json.Valid(line) {
			t.errs.Write([]byte("\n[swarg] stdout is not JSON: " + string(line[:min(len(line), 120)])))
			return
		}
		t.out <- append([]byte(nil), line...)
	}
}

func (t *stdioTransport) Send(msg []byte) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	_, err := t.in.Write(append(msg, '\n'))
	return err
}

func (t *stdioTransport) Recv() <-chan []byte { return t.out }

// Close ends the server: stdin is closed, then the process is killed after closeGrace.
func (t *stdioTransport) Close() error {
	t.once.Do(func() {
		t.in.Close()
		select {
		case <-t.exited:
		case <-time.After(closeGrace):
			t.cmd.Process.Kill()
			<-t.exited
		}
	})
	return nil
}

func (t *stdioTransport) Stderr() string { return t.errs.String() }

// tail keeps the last stderrTail bytes written to it.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > stderrTail {
		t.buf = t.buf[len(t.buf)-stderrTail:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
