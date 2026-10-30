// Package mcptest is an in-process fake MCP Server for tests: it speaks the handshake, pages
// tools/list and answers tools/call from scripted tools, and can crash or fall silent.
package mcptest

import (
	"encoding/json"
	"sync"

	"swarg/internal/config"
	"swarg/internal/mcp"
)

// Tool is one scripted tool; Handler returns the text (and whether it is an error result).
type Tool struct {
	Name, Description string
	Schema            json.RawMessage
	ReadOnly          bool
	Handler           func(args json.RawMessage) (text string, isError bool)
}

// Server is the fake; one Dial = one connection (link), so Restart gets a fresh one.
type Server struct {
	Tools        []Tool
	Instructions string
	Version      string // protocol version answered; "" = the client's
	PageSize     int    // tools per tools/list page; 0 = all
	Silent       bool   // tools/call is never answered
	FailInit     bool   // initialize answers an error
	Stderr       string

	mu    sync.Mutex
	link  *link
	Calls []string // tool names called, in order
}

// New returns a fake with the given tools.
func New(tools ...Tool) *Server { return &Server{Tools: tools} }

// Dial is the mcp.Deps.Dial seam.
func (s *Server) Dial(string, config.MCPServer) (mcp.Transport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.link = &link{s: s, out: make(chan []byte, 64)}
	return s.link, nil
}

// Crash ends the current connection as an exited process would.
func (s *Server) Crash() {
	s.mu.Lock()
	l := s.link
	s.mu.Unlock()
	if l != nil {
		l.Close()
	}
}

// Connected reports whether the current connection is open.
func (s *Server) Connected() bool {
	s.mu.Lock()
	l := s.link
	s.mu.Unlock()
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return !l.closed
}

// link is one connection of the fake.
type link struct {
	s      *Server
	out    chan []byte
	mu     sync.Mutex
	closed bool
}

func (l *link) Recv() <-chan []byte { return l.out }
func (l *link) Stderr() string      { return l.s.Stderr }

func (l *link) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.closed = true
		close(l.out)
	}
	return nil
}

// Send handles one client message and queues the answer.
func (l *link) Send(msg []byte) error {
	reply, err := l.s.handle(msg)
	if err != nil || reply == nil {
		return err
	}
	l.push(reply)
	return nil
}

// handle answers one JSON-RPC message of the client; nil for a notification or a silenced call.
func (s *Server) handle(msg []byte) ([]byte, error) {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ProtocolVersion string          `json:"protocolVersion"`
			Cursor          string          `json:"cursor"`
			Name            string          `json:"name"`
			Arguments       json.RawMessage `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal(msg, &req); err != nil {
		return nil, err
	}
	if len(req.ID) == 0 { // a notification
		return nil, nil
	}
	reply := func(result any) ([]byte, error) {
		return json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	fail := func(code int, text string) ([]byte, error) {
		return json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": code, "message": text}})
	}
	switch req.Method {
	case "initialize":
		if s.FailInit {
			return fail(-32603, "init refused")
		}
		v := s.Version
		if v == "" {
			v = req.Params.ProtocolVersion
		}
		return reply(map[string]any{"protocolVersion": v, "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]string{"name": "fake", "version": "1"}, "instructions": s.Instructions})
	case "tools/list":
		start := 0
		if req.Params.Cursor != "" {
			json.Unmarshal([]byte(req.Params.Cursor), &start)
		}
		end := len(s.Tools)
		if s.PageSize > 0 && start+s.PageSize < end {
			end = start + s.PageSize
		}
		var list []map[string]any
		for _, t := range s.Tools[start:end] {
			schema := t.Schema
			if schema == nil {
				schema = json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
			}
			list = append(list, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": schema,
				"annotations": map[string]bool{"readOnlyHint": t.ReadOnly}})
		}
		res := map[string]any{"tools": list}
		if end < len(s.Tools) {
			b, _ := json.Marshal(end)
			res["nextCursor"] = string(b)
		}
		return reply(res)
	case "tools/call":
		s.mu.Lock()
		s.Calls = append(s.Calls, req.Params.Name)
		s.mu.Unlock()
		if s.Silent {
			return nil, nil
		}
		for _, t := range s.Tools {
			if t.Name == req.Params.Name {
				text, isErr := "ok", false
				if t.Handler != nil {
					text, isErr = t.Handler(req.Params.Arguments)
				}
				return reply(map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": isErr})
			}
		}
		return fail(-32602, "unknown tool "+req.Params.Name)
	case "ping":
		return reply(map[string]any{})
	}
	return fail(-32601, "method not found")
}

func (l *link) push(b []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.out <- b
	}
}
