package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"

	"swarg/internal/chat"
	"swarg/internal/config"
	"swarg/internal/tools"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Prefix starts every MCP tool name: Prefix + server + "__" + tool.
const Prefix = "mcp__"

const (
	descriptionCut = 200 // runes of a tool description offered to the model (R-86)
	labelCut       = 40
)

// State of a server as /mcp shows it.
type State string

const (
	StateRunning State = "running"
	StateOff     State = "off"     // disabled (config or /mcp off before it ran)
	StateFailed  State = "failed"  // did not start
	StateCrashed State = "crashed" // exited after the start
	// StateNeedsApproval: a project server the user has not approved yet (R-97); nothing of it runs
	StateNeedsApproval State = "needs approval"
)

// Deps are the client's dependencies; Dial is the transport seam (nil = stdio subprocess).
type Deps struct {
	Dir     string // the project folder: the servers' working directory
	Confirm bool   // mcp.confirm: ask for approval unless the server is trusted and the tool read-only
	Dial    func(name string, s config.MCPServer) (Transport, error)
	// Project names the servers defined by the project's config (R-97): they wait for the user's
	// approval (Approved) before anything runs; OnApprove persists an approval (nil = not stored).
	Project   map[string]bool
	Approved  func(name string) bool
	OnApprove func(name string) error
}

// ServerInfo is one line of /mcp.
type ServerInfo struct {
	Name      string
	State     State
	Reason    string // why failed or crashed
	Tools     int    // offered to the model
	Enabled   bool
	Source    string // "global" or "project"
	Transport string // "stdio" or "http"
}

// ToolInfo is one tool of /mcp <server>.
type ToolInfo struct {
	Name, Description string
	ReadOnly, Offered bool
}

type tool struct {
	name     string // server-local
	full     string // Prefix + server + "__" + name
	desc     string
	schema   json.RawMessage
	readOnly bool
	offered  bool
}

type server struct {
	name         string
	cfg          config.MCPServer
	enabled      bool
	state        State
	reason       string
	instructions string
	tr           Transport
	conn         *conn
	tools        []tool
	gen          int // start generation; a watcher of an older one is stale
	project      bool
	approved     bool
}

// Client holds the servers and routes the tools. Safe for concurrent use.
type Client struct {
	deps    Deps
	mu      sync.Mutex
	servers map[string]*server
	names   []string // sorted
}

// New prepares the servers from the config; nothing runs before Start.
func New(cfg map[string]config.MCPServer, deps Deps) *Client {
	if deps.Dial == nil {
		deps.Dial = func(_ string, s config.MCPServer) (Transport, error) { return dial(deps.Dir, s) }
	}
	c := &Client{deps: deps, servers: map[string]*server{}}
	for name, s := range cfg {
		sv := &server{name: name, cfg: s, enabled: !s.Disabled, state: StateOff, project: deps.Project[name], approved: true}
		if sv.project && (deps.Approved == nil || !deps.Approved(name)) {
			sv.approved, sv.state = false, StateNeedsApproval
		}
		c.servers[name] = sv
		c.names = append(c.names, name)
	}
	sort.Strings(c.names)
	return c
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start starts every enabled server in parallel and waits for all of them (each bounded by
// startLimit). The warnings name the servers that failed; swarg runs on without them.
func (c *Client) Start(ctx context.Context) (warnings []string) {
	var wg sync.WaitGroup
	var wmu sync.Mutex
	for _, name := range c.names {
		s := c.servers[name]
		if !s.enabled || !s.approved {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.start(ctx, s); err != nil {
				wmu.Lock()
				warnings = append(warnings, fmt.Sprintf("mcp: server %q failed to start: %v", s.name, err))
				wmu.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Strings(warnings)
	return warnings
}

// start dials, shakes hands and lists the tools of s; on failure s is failed with the reason.
func (c *Client) start(ctx context.Context, s *server) error {
	ctx, cancel := context.WithTimeout(ctx, startLimit)
	defer cancel()
	tr, err := c.deps.Dial(s.name, s.cfg)
	if err != nil {
		return c.fail(s, err)
	}
	cn := newConn(tr)
	res, err := cn.handshake(ctx)
	if err == nil {
		var list []wireTool
		if list, err = cn.listTools(ctx); err == nil {
			c.mu.Lock()
			s.gen++
			s.tr, s.conn, s.state, s.reason, s.instructions = tr, cn, StateRunning, "", res.Instructions
			s.tools = c.wire(s, list)
			gen := s.gen
			c.mu.Unlock()
			go c.watch(s, cn, gen)
			return nil
		}
	}
	tr.Close()
	return c.fail(s, err)
}

func (c *Client) fail(s *server, err error) error {
	c.mu.Lock()
	s.state, s.reason, s.tr, s.conn, s.tools = StateFailed, err.Error(), nil, nil, nil
	c.mu.Unlock()
	return err
}

// wire turns the listed tools into offered tools (the allowlist decides, R-88); unknown
// allowlisted names are recorded as a reason, not an error.
func (c *Client) wire(s *server, list []wireTool) []tool {
	allow, all := map[string]bool{}, len(s.cfg.Tools) == 0
	for _, n := range s.cfg.Tools {
		allow[n] = true
	}
	var out []tool
	for _, w := range list {
		schema := w.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		out = append(out, tool{name: w.Name, full: Prefix + s.name + "__" + w.Name, desc: w.Description, schema: schema,
			readOnly: w.Annotations.ReadOnlyHint, offered: all || allow[w.Name]})
		delete(allow, w.Name)
	}
	if len(allow) > 0 {
		s.reason = "tools not offered by the server: " + strings.Join(slices.Sorted(maps.Keys(allow)), ", ")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// watch marks s crashed when its connection ends while it is still the running one.
func (c *Client) watch(s *server, cn *conn, gen int) {
	<-cn.done
	c.mu.Lock()
	defer c.mu.Unlock()
	if s.gen == gen && s.state == StateRunning {
		s.state, s.reason = StateCrashed, "exited"
		if tail := strings.TrimSpace(s.tr.Stderr()); tail != "" {
			s.reason += ": " + lastLine(tail)
		}
	}
}

// Restart stops a server if it runs and starts it again; it also switches it on.
func (c *Client) Restart(ctx context.Context, name string) error {
	c.mu.Lock()
	s := c.servers[name]
	if s == nil {
		c.mu.Unlock()
		return c.unknown(name)
	}
	if !s.approved {
		c.mu.Unlock()
		return fmt.Errorf("mcp server %q is a project server waiting for your approval; /mcp approve %s", name, name)
	}
	tr := s.tr
	s.enabled, s.tr, s.conn, s.state = true, nil, nil, StateOff
	c.mu.Unlock()
	if tr != nil {
		tr.Close()
	}
	return c.start(ctx, s)
}

// Approve records the user's approval of a project server (through Deps.OnApprove) and starts it.
func (c *Client) Approve(ctx context.Context, name string) error {
	c.mu.Lock()
	s := c.servers[name]
	if s == nil {
		c.mu.Unlock()
		return c.unknown(name)
	}
	if s.approved {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	if c.deps.OnApprove != nil {
		if err := c.deps.OnApprove(name); err != nil {
			return err
		}
	}
	c.mu.Lock()
	s.approved, s.state = true, StateOff
	c.mu.Unlock()
	return c.Restart(ctx, name)
}

// Close ends every running server (stdin closed, then killed after the grace).
func (c *Client) Close() {
	c.mu.Lock()
	var trs []Transport
	for _, s := range c.servers {
		if s.tr != nil {
			trs = append(trs, s.tr)
			s.tr, s.conn, s.state = nil, nil, StateOff
		}
		if !s.approved {
			s.state = StateNeedsApproval
		}
	}
	c.mu.Unlock()
	for _, tr := range trs {
		tr.Close()
	}
}

// SetEnabled hides (off) or shows (on) the tools of a server for this run; a server switched on
// that never ran is started by Restart, not here.
func (c *Client) SetEnabled(name string, on bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.servers[name]
	if s == nil {
		return c.unknown(name)
	}
	s.enabled = on
	return nil
}

func (c *Client) unknown(name string) error {
	return fmt.Errorf("unknown mcp server %q; known servers: %s", name, strings.Join(c.names, ", "))
}

// ---------------------------------------------------------------------------
// Tools for the agent
// ---------------------------------------------------------------------------

// Owns reports whether name is an MCP tool name.
func Owns(name string) bool { return strings.HasPrefix(name, Prefix) }

// Specs lists the offered tools of the running, enabled servers (name order).
func (c *Client) Specs() []chat.ToolSpec {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []chat.ToolSpec
	for _, name := range c.names {
		s := c.servers[name]
		if !s.enabled || s.state == StateOff || s.state == StateFailed {
			continue
		}
		for _, t := range s.tools {
			if t.offered {
				out = append(out, chat.ToolSpec{Name: t.full, Description: tools.ClipRunes(t.desc, descriptionCut), Schema: t.schema})
			}
		}
	}
	return out
}

// Kind: known for an offered tool of an enabled server (a crashed server keeps its tools known
// so the call explains the state); read-only only when the server is trusted and the tool says
// so (R-87); approval unless read-only or mcp.confirm is off.
func (c *Client) Kind(name string) (known, readOnly, confirm bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, t := c.find(name)
	if t == nil || !s.enabled || s.state == StateOff || s.state == StateFailed {
		return false, false, false
	}
	readOnly = s.cfg.Trust && t.readOnly
	return true, readOnly, c.deps.Confirm && !readOnly
}

// Label is the one-line description of a call, like a built-in Tool's Summary: the first string
// argument (the UI shows `mcp__demo__lookup(A-100)` from it; T8 eval showed the doubled name).
// The argument is the first string one in key order, clipped; "" for none or bad JSON.
func (c *Client) Label(_, args string) string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return ""
	}
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if s, ok := m[k].(string); ok && s != "" {
			return tools.ClipRunes(s, labelCut)
		}
	}
	return ""
}

// Stub is the history stub of a finished call.
func (c *Client) Stub(name string) string { return "[" + name + ": result shown earlier]" }

// Run calls the tool. A server that is not running answers with its state; a tool error
// (isError) and a protocol error are errors the model sees; the text content is the output.
func (c *Client) Run(ctx context.Context, name, args string) (output, brief string, err error) {
	c.mu.Lock()
	s, t := c.find(name)
	if t == nil {
		c.mu.Unlock()
		return "", "", fmt.Errorf("unknown tool %q", name)
	}
	cn, state, reason, timeout := s.conn, s.state, s.reason, s.cfg.Timeout()
	c.mu.Unlock()
	if state != StateRunning {
		return "", "", fmt.Errorf("mcp server %q is %s (%s); /mcp restart %s", s.name, state, reason, s.name)
	}
	a := json.RawMessage(strings.TrimSpace(args))
	if len(a) == 0 || string(a) == "null" {
		a = json.RawMessage("{}")
	}
	params := map[string]any{"name": t.name, "arguments": a}
	raw, err := cn.call(ctx, "tools/call", params, timeout)
	if expired(err) { // the server forgot the session: one new session, one more try (R-93)
		if err = c.Restart(ctx, s.name); err == nil {
			c.mu.Lock()
			cn = s.conn
			c.mu.Unlock()
			raw, err = cn.call(ctx, "tools/call", params, timeout)
		}
	}
	if err != nil {
		if errors.Is(err, errExited) {
			return "", "", fmt.Errorf("mcp server %q exited; /mcp restart %s", s.name, s.name)
		}
		return "", "", fmt.Errorf("mcp %q: %w", s.name, err)
	}
	var res callResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", "", fmt.Errorf("mcp %q: bad result: %w", s.name, err)
	}
	text := render(res)
	if res.IsError {
		return "", "", errors.New(text)
	}
	return tools.Cap(text), lastLine(firstLine(text)), nil
}

// expired reports a session-expired error of the HTTP transport.
func expired(err error) bool {
	var re *rpcError
	return errors.As(err, &re) && re.Code == codeSessionExpired
}

// render joins the content blocks: text as is, other kinds as one line each.
func render(res callResult) string {
	var parts []string
	for _, b := range res.Content {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "image", "audio":
			parts = append(parts, fmt.Sprintf("[%s %s, %d bytes]", b.Type, b.MimeType, len(b.Data)*3/4))
		case "resource", "resource_link":
			parts = append(parts, "[resource "+b.Resource.URI+"]")
		default:
			parts = append(parts, "["+b.Type+"]")
		}
	}
	if len(parts) == 0 && len(res.StructuredContent) > 0 {
		parts = append(parts, string(res.StructuredContent))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// find resolves a full tool name; caller holds mu.
func (c *Client) find(name string) (*server, *tool) {
	rest, ok := strings.CutPrefix(name, Prefix)
	if !ok {
		return nil, nil
	}
	sname, tname, ok := strings.Cut(rest, "__")
	s := c.servers[sname]
	if !ok || s == nil {
		return nil, nil
	}
	for i := range s.tools {
		if s.tools[i].name == tname && s.tools[i].offered {
			return s, &s.tools[i]
		}
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// For the UI and the prompt
// ---------------------------------------------------------------------------

// Servers lists every configured server in name order.
func (c *Client) Servers() []ServerInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ServerInfo, 0, len(c.names))
	for _, name := range c.names {
		s := c.servers[name]
		n := 0
		for _, t := range s.tools {
			if t.offered {
				n++
			}
		}
		source, transport := "global", "stdio"
		if s.project {
			source = "project"
		}
		if s.cfg.URL != "" {
			transport = "http"
		}
		out = append(out, ServerInfo{Name: name, State: s.state, Reason: s.reason, Tools: n, Enabled: s.enabled, Source: source, Transport: transport})
	}
	return out
}

// Tools lists every tool of one server (offered or not) and the tail of its stderr.
func (c *Client) Tools(name string) (list []ToolInfo, stderr string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.servers[name]
	if s == nil {
		return nil, "", c.unknown(name)
	}
	for _, t := range s.tools {
		list = append(list, ToolInfo{Name: t.name, Description: t.desc, ReadOnly: t.readOnly, Offered: t.offered})
	}
	if s.tr != nil {
		stderr = s.tr.Stderr()
	}
	return list, stderr, nil
}

// Instructions is the prompt block text: `<server>: <instructions>` per running server that
// gave some, "" when none.
func (c *Client) Instructions() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var lines []string
	for _, name := range c.names {
		s := c.servers[name]
		if s.enabled && s.state == StateRunning && s.instructions != "" {
			lines = append(lines, name+": "+tools.ClipRunes(strings.Join(strings.Fields(s.instructions), " "), descriptionCut))
		}
	}
	return strings.Join(lines, "\n")
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return tools.ClipRunes(s, 60)
}

func lastLine(s string) string {
	if i := strings.LastIndex(strings.TrimRight(s, "\n"), "\n"); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return strings.TrimSpace(s)
}
