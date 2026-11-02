package mcp_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"swarg/internal/config"
	"swarg/internal/keys"
	"swarg/internal/mcp"
	"swarg/internal/mcp/mcptest"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// serve runs the HTTP fake on a loopback httptest server and returns its MCP endpoint URL.
func serve(t *testing.T, h http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL + "/mcp"
}

// startURL builds a client with one url server "docs" (default Dial = the real transports).
func startURL(t *testing.T, cfg config.MCPServer) (*mcp.Client, []string) {
	t.Helper()
	c := mcp.New(map[string]config.MCPServer{"docs": cfg}, mcp.Deps{Confirm: true})
	t.Cleanup(c.Close)
	return c, c.Start(context.Background())
}

func run(t *testing.T, c *mcp.Client, args string) (string, error) {
	t.Helper()
	out, _, err := c.Run(context.Background(), "mcp__docs__a", args)
	return out, err
}

// ---------------------------------------------------------------------------
// Protocol
// ---------------------------------------------------------------------------

func TestHTTP_JSONReplyAndSessionRoundTrip(t *testing.T) {
	fake := mcptest.NewHTTP(echoTool("a", true))
	fake.Sessions = true
	fake.Instructions = "docs server"
	c, warnings := startURL(t, config.MCPServer{URL: serve(t, fake)})
	if len(warnings) != 0 {
		t.Fatalf("warnings %q", warnings)
	}
	if got := specNames(c); got != "mcp__docs__a" || c.Instructions() != "docs: docs server" {
		t.Fatalf("specs %q instructions %q", got, c.Instructions())
	}
	out, err := run(t, c, `{"q":"x"}`)
	if err != nil || out != `a:{"q":"x"}` {
		t.Fatalf("run %q %v", out, err)
	}
	h := fake.Headers // of the tools/call request
	if h.Get("Mcp-Session-Id") != "s-1" || h.Get("MCP-Protocol-Version") != "2025-11-25" {
		t.Fatalf("session/version headers %v", h)
	}
	if a := h.Get("Accept"); !strings.Contains(a, "application/json") || !strings.Contains(a, "text/event-stream") {
		t.Fatalf("accept %q", a)
	}
	if h.Get("Content-Type") != "application/json" {
		t.Fatalf("content type %q", h.Get("Content-Type"))
	}
}

func TestHTTP_SSEReply(t *testing.T) {
	fake := mcptest.NewHTTP(echoTool("a", false))
	fake.SSE = true
	c, warnings := startURL(t, config.MCPServer{URL: serve(t, fake)})
	if len(warnings) != 0 {
		t.Fatalf("warnings %q", warnings)
	}
	out, err := run(t, c, `{"q":"y"}`)
	if err != nil || out != `a:{"q":"y"}` {
		t.Fatalf("run %q %v", out, err)
	}
}

func TestHTTP_CloseSendsDelete(t *testing.T) {
	fake := mcptest.NewHTTP(echoTool("a", false))
	fake.Sessions = true
	c, _ := startURL(t, config.MCPServer{URL: serve(t, fake)})
	c.Close()
	if fake.Deletes != 1 || fake.Headers.Get("Mcp-Session-Id") != "s-1" {
		t.Fatalf("deletes %d headers %v", fake.Deletes, fake.Headers)
	}
}

func TestHTTP_HeadersExpandEnv(t *testing.T) {
	t.Setenv("T9_TOKEN", "secret-value")
	fake := mcptest.NewHTTP(echoTool("a", false))
	fake.Token = "secret-value"
	cfg := config.MCPServer{URL: serve(t, fake), Headers: map[string]string{"Authorization": "Bearer ${T9_TOKEN}", "X-Team": "blue"}}
	c, warnings := startURL(t, cfg)
	if len(warnings) != 0 {
		t.Fatalf("warnings %q", warnings)
	}
	if _, err := run(t, c, "{}"); err != nil || fake.Headers.Get("X-Team") != "blue" {
		t.Fatalf("run %v headers %v", err, fake.Headers)
	}
}

// isolateConfigDir points config.Dir() (and so the key store) into a temp dir.
func isolateConfigDir(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("HOME", d)
	t.Setenv("XDG_CONFIG_HOME", d)
	t.Setenv("AppData", d)
}

// writeKeysFile writes the user's (isolated) keys file, private.
func writeKeysFile(t *testing.T, body string) {
	t.Helper()
	f := keys.User().File
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f, 0o600); err != nil {
		t.Fatal(err)
	}
}

// K4: an unset variable falls back to the keys file; a set one wins; a restart re-reads it.
func TestHTTP_HeaderFromKeysFileEnvWinsAndRestartRereads(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("T12_MCP_TOK", "")
	writeKeysFile(t, "T12_MCP_TOK=from-file\n")
	fake := mcptest.NewHTTP(echoTool("a", false))
	fake.Token = "from-file"
	cfg := config.MCPServer{URL: serve(t, fake), Headers: map[string]string{"Authorization": "Bearer ${T12_MCP_TOK}"}}
	c, warnings := startURL(t, cfg)
	if len(warnings) != 0 {
		t.Fatalf("file key: warnings %q", warnings)
	}
	if _, err := run(t, c, "{}"); err != nil {
		t.Fatalf("run with the file key: %v", err)
	}
	// the key is edited in the keys file; Restart dials again and reads the new value
	fake.Token = "rotated"
	writeKeysFile(t, "T12_MCP_TOK=rotated\n")
	if err := c.Restart(t.Context(), "docs"); err != nil {
		t.Fatalf("restart with the rotated key: %v", err)
	}
	// the environment wins over the file
	t.Setenv("T12_MCP_TOK", "from-env")
	fake.Token = "from-env"
	if err := c.Restart(t.Context(), "docs"); err != nil {
		t.Fatalf("restart with the env key: %v", err)
	}
}

// The initialized notification reaches the server before tools/list (the POSTs of notifications
// are not left racing the next request).
func TestHTTP_NotificationBeforeNextRequest(t *testing.T) {
	fake := mcptest.NewHTTP(echoTool("a", false))
	_, warnings := startURL(t, config.MCPServer{URL: serve(t, fake)})
	if len(warnings) != 0 {
		t.Fatalf("warnings %q", warnings)
	}
	if got := strings.Join(fake.Methods, " "); got != "initialize notifications/initialized tools/list" {
		t.Fatalf("order %q", got)
	}
}

// A server request inside the stream gets "method not found"; when the server refuses that answer
// the client's own call (same id) is not disturbed.
func TestHTTP_ServerRequestAnsweredWithoutDisturbingTheCall(t *testing.T) {
	fake := mcptest.NewHTTP(echoTool("a", false))
	fake.SSE, fake.AskBack, fake.RefuseReplies = true, true, true
	c, _ := startURL(t, config.MCPServer{URL: serve(t, fake)})
	out, err := run(t, c, `{"q":"w"}`)
	if err != nil || out != `a:{"q":"w"}` {
		t.Fatalf("run %q %v", out, err)
	}
	if got := fake.Methods[len(fake.Methods)-1]; got != "" {
		t.Fatalf("last message was %q, want the client's response to roots/list", got)
	}
}

// ---------------------------------------------------------------------------
// Failures (H3)
// ---------------------------------------------------------------------------

func TestHTTP_StatusTexts(t *testing.T) {
	t.Setenv("T9_TOKEN", "wrong-value")
	auth := mcptest.NewHTTP(echoTool("a", false))
	auth.Token = "right"
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	tests := []struct {
		name string
		cfg  config.MCPServer
		want string
	}{
		{"401", config.MCPServer{URL: serve(t, auth), Headers: map[string]string{"Authorization": "Bearer ${T9_TOKEN}"}},
			`HTTP 401: the server needs authentication; set headers.Authorization`},
		{"500", config.MCPServer{URL: serve(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "kaput", 500)
		}))}, "HTTP 500: kaput"},
		{"bad body", config.MCPServer{URL: serve(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>"))
		}))}, "bad answer: text/html"},
		{"down", config.MCPServer{URL: down.URL + "/mcp"}, "cannot reach " + down.URL + "/mcp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, warnings := startURL(t, tt.cfg)
			if len(warnings) != 1 || !strings.Contains(warnings[0], tt.want) {
				t.Fatalf("warnings %q, want %q", warnings, tt.want)
			}
			if strings.Contains(warnings[0], "wrong-value") {
				t.Fatalf("header value leaked: %q", warnings[0])
			}
			if srv := c.Servers(); srv[0].State != mcp.StateFailed || strings.Contains(srv[0].Reason, "wrong-value") {
				t.Fatalf("server %+v", srv[0])
			}
			if _, stderr, _ := c.Tools("docs"); strings.Contains(stderr, "wrong-value") {
				t.Fatalf("stderr leaked the header: %q", stderr)
			}
		})
	}
}

func TestHTTP_SessionExpiredReinitializesOnce(t *testing.T) {
	fake := mcptest.NewHTTP(echoTool("a", false))
	fake.Sessions = true
	c, _ := startURL(t, config.MCPServer{URL: serve(t, fake)})
	fake.Forget()
	out, err := run(t, c, `{"q":"z"}`)
	if err != nil || out != `a:{"q":"z"}` || fake.Inits != 2 {
		t.Fatalf("run %q %v inits %d", out, err, fake.Inits)
	}
	if srv := c.Servers(); srv[0].State != mcp.StateRunning {
		t.Fatalf("server %+v", srv[0])
	}
	// a server that forgets every session: the second attempt is the error
	always := mcptest.NewHTTP(echoTool("a", false))
	always.Sessions, always.ExpireCalls = true, true
	c2, warnings := startURL(t, config.MCPServer{URL: serve(t, always)})
	if len(warnings) != 0 {
		t.Fatalf("warnings %q", warnings)
	}
	if _, err := run(t, c2, "{}"); err == nil || !strings.Contains(err.Error(), "session expired") || always.Inits != 2 {
		t.Fatalf("err %v inits %d", err, always.Inits)
	}
}

// ---------------------------------------------------------------------------
// Security (H4)
// ---------------------------------------------------------------------------

func TestHTTP_PlainHTTPOnlyPrivate(t *testing.T) {
	tests := []struct {
		url  string
		want string // "" = allowed by the check (the dial itself may still fail)
	}{
		{"http://203.0.113.5/mcp", "use https"},
		{"https://203.0.113.5/mcp", ""},
		{"http://127.0.0.1:1/mcp", ""},
		{"http://localhost:1/mcp", ""},
		{"http://10.1.2.3:1/mcp", ""},
		{"http://192.168.1.9:1/mcp", ""},
		{"http://[::1]:1/mcp", ""},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			err := mcp.CheckURL(tt.url)
			if (tt.want == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CheckURL(%s) = %v, want %q", tt.url, err, tt.want)
			}
		})
	}
	_, warnings := startURL(t, config.MCPServer{URL: "http://203.0.113.5/mcp"})
	if len(warnings) != 1 || !strings.Contains(warnings[0], "use https") {
		t.Fatalf("warnings %q", warnings)
	}
}

func TestHTTP_RedirectToAnotherHostRefused(t *testing.T) {
	fake := mcptest.NewHTTP(echoTool("a", false))
	target := serve(t, fake)
	hop := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	}))
	_, warnings := startURL(t, config.MCPServer{URL: hop})
	if len(warnings) != 1 || !strings.Contains(warnings[0], "redirect to another host refused") || fake.Inits != 0 {
		t.Fatalf("warnings %q inits %d", warnings, fake.Inits)
	}
}

// ---------------------------------------------------------------------------
// The Python demo server over HTTP (the one the eval tasks use)
// ---------------------------------------------------------------------------

func TestHTTP_PythonDemoServer(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	script, _ := filepath.Abs(filepath.Join("..", "..", "tests", "eval", "mcp", "mcp_http.py"))
	cmd := exec.Command(py, script, strconv.Itoa(port), "--token", "t9-test", "--sse")
	cmd.Dir = t.TempDir()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/mcp"
	for i := 0; i < 50; i++ { // wait for the port
		if conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port)); err == nil {
			conn.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Setenv("T9_TOKEN", "t9-test")
	c := mcp.New(map[string]config.MCPServer{"orders": {URL: url, Trust: true, Headers: map[string]string{"Authorization": "Bearer ${T9_TOKEN}"}}}, mcp.Deps{Confirm: true})
	defer c.Close()
	if w := c.Start(context.Background()); len(w) != 0 {
		t.Fatalf("warnings %q", w)
	}
	if got := specNames(c); got != "mcp__orders__lookup mcp__orders__save" {
		t.Fatalf("specs %q", got)
	}
	out, _, err := c.Run(context.Background(), "mcp__orders__lookup", `{"order_id":"A-300"}`)
	if err != nil || !strings.Contains(out, "cancelled") {
		t.Fatalf("lookup %q %v", out, err)
	}
	if _, _, err := c.Run(context.Background(), "mcp__orders__lookup", `{"order_id":"Z"}`); err == nil || !strings.Contains(err.Error(), "unknown order") {
		t.Fatalf("isError %v", err)
	}
	c.Close()
	// without the token the server refuses
	c2 := mcp.New(map[string]config.MCPServer{"orders": {URL: url}}, mcp.Deps{})
	defer c2.Close()
	if w := c2.Start(context.Background()); len(w) != 1 || !strings.Contains(w[0], "needs authentication") {
		t.Fatalf("warnings %q", w)
	}
}
