package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"swarg/internal/config"
	"swarg/internal/keys"
)

// ---------------------------------------------------------------------------
// Streamable HTTP transport (R-91..R-94)
// ---------------------------------------------------------------------------

// Error codes of swarg's own, carried in synthesized JSON-RPC error responses.
const (
	codeHTTP           = -32010 // the HTTP layer failed; the message names the status
	codeSessionExpired = -32011 // 404 while a session id was held: re-initialize (R-93)
)

const (
	bodyCut     = 120                         // bytes of an error body quoted
	resolveWait = 5 * time.Second             // DNS lookup for the plain-http check
	mediaJSON   = "application/json"          //
	mediaSSE    = "text/event-stream"         //
	acceptBoth  = mediaJSON + ", " + mediaSSE // the spec demands both on every POST
	errExpired  = "session expired: the server answered 404 to the session id"
)

// httpTransport posts every message to the MCP endpoint; the answers (one JSON object or the
// events of an SSE stream) and the HTTP failures (as JSON-RPC errors with the request's id)
// land in Recv. Header values stay inside the transport (I-32).
type httpTransport struct {
	url     string
	headers map[string]string
	client  *http.Client
	limit   time.Duration // one HTTP exchange
	out     chan []byte
	errs    *tail
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	once    sync.Once
	mu      sync.Mutex
	session string
	version string
}

// dial picks the transport by the config: url = Streamable HTTP, command = stdio.
func dial(dir string, s config.MCPServer) (Transport, error) {
	if s.URL != "" {
		return dialHTTP(s)
	}
	return dialStdio(dir, s)
}

// dialHTTP prepares the transport; nothing is sent before the first message.
func dialHTTP(s config.MCPServer) (Transport, error) {
	if err := CheckURL(s.URL); err != nil {
		return nil, err
	}
	headers := make(map[string]string, len(s.Headers))
	for k, v := range s.Headers {
		headers[k] = keys.User().Expand(v)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &httpTransport{url: s.URL, headers: headers, limit: max(s.Timeout(), startLimit) + closeGrace,
		out: make(chan []byte, 16), errs: &tail{}, ctx: ctx, cancel: cancel}
	t.client = &http.Client{CheckRedirect: sameHost}
	return t, nil
}

// CheckURL applies R-94: plain http only to localhost or a private address (a host name is
// resolved); https anywhere.
func CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		return nil
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil
	}
	ips := []net.IP{net.ParseIP(host)}
	if ips[0] == nil {
		ctx, cancel := context.WithTimeout(context.Background(), resolveWait)
		defer cancel()
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return fmt.Errorf("cannot resolve %q: %w", host, err)
		}
		ips = ips[:0]
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	for _, ip := range ips {
		if !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
			return fmt.Errorf("plain http is allowed only for localhost and private networks (%s is public); use https", host)
		}
	}
	return nil
}

// sameHost is the redirect policy: the same scheme and host only (R-94).
func sameHost(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	if req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host {
		return errors.New("redirect to another host refused")
	}
	return nil
}

// Send posts the message. A request goes out in the background and its answer (or the HTTP
// failure as a JSON-RPC error with its id) arrives through Recv; a notification or a response of
// the client is posted here and now, so the server sees them in order (initialized before
// tools/list) and no foreign id is ever synthesized.
func (t *httpTransport) Send(msg []byte) error {
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	json.Unmarshal(msg, &m)
	if len(m.ID) == 0 || m.Method == "" {
		err := t.post(msg, nil)
		if err != nil {
			t.errs.Write([]byte("\n[swarg] " + err.Error()))
		}
		return err
	}
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		if err := t.post(msg, m.ID); err != nil {
			t.errs.Write([]byte("\n[swarg] " + err.Error()))
			code := codeHTTP
			if err.Error() == errExpired {
				code = codeSessionExpired
			}
			b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": rpcError{Code: code, Message: err.Error()}})
			t.deliver(b)
		}
	}()
	return nil
}

// post runs one HTTP exchange and delivers every message of the answer; id is nil for a
// notification or a client response (whatever a 2xx carries then was accepted).
func (t *httpTransport) post(msg, id json.RawMessage) error {
	ctx, cancel := context.WithTimeout(t.ctx, t.limit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(msg))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mediaJSON)
	req.Header.Set("Accept", acceptBoth)
	t.setHeaders(req)
	resp, err := t.client.Do(req)
	if err != nil {
		return reachError(t.url, err)
	}
	defer resp.Body.Close()
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.session = sid
		t.mu.Unlock()
	}
	switch {
	case resp.StatusCode == http.StatusAccepted:
		return nil
	case resp.StatusCode == http.StatusNotFound && req.Header.Get("Mcp-Session-Id") != "":
		return errors.New(errExpired)
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("HTTP 401: the server needs authentication; set headers.Authorization (e.g. \"Bearer ${TOKEN}\")")
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, bodyCut))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	media, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";")
	switch strings.TrimSpace(media) {
	case mediaJSON:
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxLine))
		if err != nil {
			return fmt.Errorf("reading the answer: %w", err)
		}
		if !json.Valid(body) {
			return errors.New("bad answer: not JSON")
		}
		t.deliver(body)
		return nil
	case mediaSSE:
		return sseEvents(resp.Body, func(data []byte) {
			if json.Valid(data) {
				t.deliver(data)
			}
		})
	}
	if len(id) == 0 {
		return nil // an answer to a notification: whatever it is, it was accepted
	}
	return fmt.Errorf("bad answer: %s instead of JSON or an event stream", strings.TrimSpace(media))
}

// setHeaders adds the configured headers, the session id and the protocol version.
func (t *httpTransport) setHeaders(req *http.Request) {
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.session != "" {
		req.Header.Set("Mcp-Session-Id", t.session)
	}
	if t.version != "" {
		req.Header.Set("MCP-Protocol-Version", t.version)
	}
}

// deliver hands one message to the connection; the initialize result fixes the protocol version.
func (t *httpTransport) deliver(msg []byte) {
	var m struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if json.Unmarshal(msg, &m) == nil && m.Result.ProtocolVersion != "" {
		t.mu.Lock()
		t.version = m.Result.ProtocolVersion
		t.mu.Unlock()
	}
	select {
	case t.out <- msg:
	case <-t.ctx.Done():
	}
}

func (t *httpTransport) Recv() <-chan []byte { return t.out }

// Close aborts the exchanges in flight, ends the session with DELETE (405 ignored) and closes Recv.
func (t *httpTransport) Close() error {
	t.once.Do(func() {
		t.cancel()
		t.wg.Wait()
		t.mu.Lock()
		sid := t.session
		t.mu.Unlock()
		if sid != "" {
			ctx, cancel := context.WithTimeout(context.Background(), closeGrace)
			defer cancel()
			if req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.url, nil); err == nil {
				t.setHeaders(req)
				if resp, err := t.client.Do(req); err == nil {
					resp.Body.Close()
				}
			}
		}
		close(t.out)
	})
	return nil
}

func (t *httpTransport) Stderr() string { return t.errs.String() }

// reachError names the endpoint once (url.Error repeats it) and drops the context noise.
func reachError(endpoint string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("cannot reach %s: %w", endpoint, err)
}

// ---------------------------------------------------------------------------
// SSE
// ---------------------------------------------------------------------------

// sseEvents reads a text/event-stream and calls fn with the data of every event that has some
// (several data lines are joined by newlines; comments and other fields are skipped).
func sseEvents(r io.Reader, fn func(data []byte)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, readBufSize), maxLine)
	var data [][]byte
	flush := func() {
		if joined := bytes.Join(data, []byte("\n")); len(bytes.TrimSpace(joined)) > 0 {
			fn(joined)
		}
		data = nil
	}
	for sc.Scan() {
		line := bytes.TrimRight(sc.Bytes(), "\r")
		if len(line) == 0 {
			flush()
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := bytes.Cut(line, []byte(":"))
		if string(field) == "data" {
			data = append(data, append([]byte(nil), bytes.TrimPrefix(value, []byte(" "))...))
		}
	}
	flush()
	return sc.Err()
}
