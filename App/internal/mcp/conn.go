package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// JSON-RPC connection
// ---------------------------------------------------------------------------

// protocolVersion is what swarg asks for; supportedVersions is what it accepts in the answer.
const protocolVersion = "2025-11-25"

var supportedVersions = map[string]bool{"2025-03-26": true, "2025-06-18": true, "2025-11-25": true, "2026-07-28": true}

var errExited = errors.New("the server exited")

// rpcError is a JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

type response struct {
	result json.RawMessage
	err    *rpcError
}

// conn matches responses to requests over one Transport; it answers requests from the server
// with "method not found" and ignores its notifications.
type conn struct {
	t       Transport
	mu      sync.Mutex
	next    int
	pending map[int]chan response
	closed  bool
	done    chan struct{}
}

func newConn(t Transport) *conn {
	c := &conn{t: t, pending: map[int]chan response{}, done: make(chan struct{})}
	go c.read()
	return c
}

func (c *conn) read() {
	for msg := range c.t.Recv() {
		var m struct {
			ID     *json.RawMessage `json:"id"`
			Method string           `json:"method"`
			Result json.RawMessage  `json:"result"`
			Error  *rpcError        `json:"error"`
		}
		if json.Unmarshal(msg, &m) != nil || m.Method != "" {
			if m.ID != nil && m.Method != "" { // a request of the server: swarg offers nothing
				c.send(map[string]any{"jsonrpc": "2.0", "id": *m.ID, "error": rpcError{Code: -32601, Message: "method not found"}})
			}
			continue
		}
		var id int
		if m.ID == nil || json.Unmarshal(*m.ID, &id) != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- response{m.Result, m.Error}
		}
	}
	c.mu.Lock()
	c.closed = true
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.done)
}

func (c *conn) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.t.Send(b)
}

// notify sends a notification (no id, no answer).
func (c *conn) notify(method string, params any) error {
	return c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// call sends a request and waits for its response, the timeout or ctx. A late response is dropped.
func (c *conn) call(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errExited
	}
	c.next++
	id := c.next
	ch := make(chan response, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	drop := func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		drop()
		return nil, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r, ok := <-ch:
		if !ok {
			return nil, errExited
		}
		if r.err != nil {
			return nil, fmt.Errorf("%s: %w", method, r.err)
		}
		return r.result, nil
	case <-timer.C:
		drop()
		return nil, fmt.Errorf("no answer in %s", timeout)
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}

// ---------------------------------------------------------------------------
// Handshake and tool list
// ---------------------------------------------------------------------------

// initResult is the part of InitializeResult swarg reads.
type initResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Instructions    string `json:"instructions"`
	ServerInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// handshake runs initialize and notifications/initialized; an unknown protocol version is an error.
func (c *conn) handshake(ctx context.Context) (initResult, error) {
	var res initResult
	raw, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "swarg", "version": "0.1"},
	}, startLimit)
	if err != nil {
		return res, err
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return res, fmt.Errorf("initialize: bad result: %w", err)
	}
	if !supportedVersions[res.ProtocolVersion] {
		return res, fmt.Errorf("protocol version %q is not supported", res.ProtocolVersion)
	}
	return res, c.notify("notifications/initialized", map[string]any{})
}

// wireTool is one entry of tools/list.
type wireTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations struct {
		ReadOnlyHint bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

// listTools follows nextCursor until the list is complete.
func (c *conn) listTools(ctx context.Context) ([]wireTool, error) {
	var all []wireTool
	params := map[string]any{}
	for {
		raw, err := c.call(ctx, "tools/list", params, startLimit)
		if err != nil {
			return nil, err
		}
		var page struct {
			Tools      []wireTool `json:"tools"`
			NextCursor string     `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("tools/list: bad result: %w", err)
		}
		all = append(all, page.Tools...)
		if page.NextCursor == "" || len(page.Tools) == 0 {
			return all, nil
		}
		params = map[string]any{"cursor": page.NextCursor}
	}
}

// callResult is the part of CallToolResult swarg reads.
type callResult struct {
	Content []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
		Resource struct {
			URI string `json:"uri"`
		} `json:"resource"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}
