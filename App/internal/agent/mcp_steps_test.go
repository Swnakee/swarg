package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/config"
	"swarg/internal/mcp"
	"swarg/internal/mcp/mcptest"
	"swarg/internal/prompt"
)

// ---------------------------------------------------------------------------
// Steps for mcp.feature: the real mcp.Client over the in-process fake server.
// ---------------------------------------------------------------------------

type mcpWorld struct {
	name     string
	fake     *mcptest.Server
	http     *mcptest.HTTPServer // set for a server reached by URL (the real HTTP transport is used)
	cfg      config.MCPServer
	client   *mcp.Client
	warnings []string
}

func (w *world) mcpTool(name string) *mcptest.Tool {
	for i := range w.mc.fake.Tools {
		if w.mc.fake.Tools[i].Name == name {
			return &w.mc.fake.Tools[i]
		}
	}
	return nil
}

// plugMCP builds and starts the client (like main) and returns the `mcp` block; nil without a server.
func (w *world) plugMCP() []prompt.Block {
	if w.mc == nil {
		return nil
	}
	deps := mcp.Deps{Confirm: true, Dial: w.mc.fake.Dial}
	if w.mc.http != nil {
		deps.Dial = nil // the real transports, picked by url
	}
	w.mc.client = mcp.New(map[string]config.MCPServer{w.mc.name: w.mc.cfg}, deps)
	w.tb.Cleanup(w.mc.client.Close)
	w.mc.warnings = w.mc.client.Start(context.Background())
	return []prompt.Block{{Name: "mcp", Source: prompt.SourceTool, Order: 1600, Text: w.mc.client.Instructions()}}
}

func registerMCPSteps(sc *godog.ScenarioContext, w *world) {
	server := func(name string, tools ...string) error {
		w.real = true
		w.mc = &mcpWorld{name: name, fake: mcptest.New(), cfg: config.MCPServer{Command: "fake"}}
		for _, t := range tools {
			w.mc.fake.Tools = append(w.mc.fake.Tools, mcptest.Tool{Name: t, Description: "the " + t + " tool"})
		}
		return nil
	}
	sc.Step(`^an MCP Server "([^"]*)" offering the tool "([^"]*)"$`, func(name, a string) error { return server(name, a) })
	sc.Step(`^an MCP Server "([^"]*)" offering the tools "([^"]*)" and "([^"]*)"$`, func(name, a, b string) error { return server(name, a, b) })
	sc.Step(`^the tool "([^"]*)" of the MCP Server answers "([^"]*)"$`, func(tool, text string) error {
		w.mcpTool(tool).Handler = func(json.RawMessage) (string, bool) { return text, false }
		return nil
	})
	sc.Step(`^the tool "([^"]*)" of the MCP Server answers with an error "([^"]*)"$`, func(tool, text string) error {
		w.mcpTool(tool).Handler = func(json.RawMessage) (string, bool) { return text, true }
		return nil
	})
	sc.Step(`^the tool "([^"]*)" of the MCP Server is marked read-only$`, func(tool string) error {
		w.mcpTool(tool).ReadOnly = true
		return nil
	})
	sc.Step(`^the tool "([^"]*)" of the MCP Server is described in (\d+) characters$`, func(tool string, n int) error {
		w.mcpTool(tool).Description = strings.Repeat("d", n)
		return nil
	})
	sc.Step(`^the MCP Server is trusted$`, func() error { w.mc.cfg.Trust = true; return nil })
	sc.Step(`^the MCP Server offers only the tool "([^"]*)" to the model$`, func(tool string) error {
		w.mc.cfg.Tools = []string{tool}
		return nil
	})
	byURL := func(name string, tools ...string) error {
		server(name, tools...)
		w.mc.http = &mcptest.HTTPServer{Server: w.mc.fake, Sessions: true}
		srv := httptest.NewServer(w.mc.http)
		w.tb.Cleanup(srv.Close)
		w.mc.cfg = config.MCPServer{URL: srv.URL + "/mcp"}
		return nil
	}
	sc.Step(`^an MCP Server "([^"]*)" reached by URL offering the tool "([^"]*)"$`, func(name, a string) error { return byURL(name, a) })
	sc.Step(`^an MCP Server "([^"]*)" reached by URL offering the tools "([^"]*)" and "([^"]*)"$`, func(name, a, b string) error { return byURL(name, a, b) })
	sc.Step(`^the MCP Server streams its answers$`, func() error { w.mc.http.SSE = true; return nil })
	sc.Step(`^the MCP Server needs the bearer token "([^"]*)"$`, func(tok string) error { w.mc.http.Token = tok; return nil })
	sc.Step(`^the environment variable "([^"]*)" holds "([^"]*)"$`, func(k, v string) error {
		w.tb.Cleanup(func() { os.Unsetenv(k) })
		return os.Setenv(k, v)
	})
	sc.Step(`^the MCP Server is configured with the header "([^"]*)" set to "([^"]*)"$`, func(k, v string) error {
		w.mc.cfg.Headers = map[string]string{k: v}
		return nil
	})
	sc.Step(`^nobody answers at the MCP Server's URL$`, func() error {
		u, _ := url.Parse(w.mc.cfg.URL)
		l, err := net.Listen("tcp", "127.0.0.1:0") // a port that was free a moment ago
		if err != nil {
			return err
		}
		u.Host = l.Addr().String()
		l.Close()
		w.mc.cfg.URL = u.String()
		return nil
	})
	sc.Step(`^the MCP Server's URL is plain http to a public host$`, func() error { w.mc.cfg.URL = "http://203.0.113.5/mcp"; return nil })
	sc.Step(`^the MCP Server forgets the session$`, func() error {
		w.act(func() error { w.mc.http.Forget(); return nil })
		return nil
	})
	sc.Step(`^the MCP Server "([^"]*)" is reported failed because of "([^"]*)"$`, func(name, reason string) error {
		if err := w.settle(); err != nil {
			return err
		}
		for _, s := range w.mc.client.Servers() {
			if s.Name == name && s.State == mcp.StateFailed && strings.Contains(s.Reason, reason) {
				return nil
			}
		}
		return fmt.Errorf("servers %+v, want %s failed because of %q", w.mc.client.Servers(), name, reason)
	})
	sc.Step(`^the reported state of the MCP Server "([^"]*)" does not mention "([^"]*)"$`, func(name, secret string) error {
		if err := w.settle(); err != nil {
			return err
		}
		_, stderr, err := w.mc.client.Tools(name)
		if err != nil {
			return err
		}
		all := strings.Join(w.mc.warnings, "\n") + stderr
		for _, s := range w.mc.client.Servers() {
			all += s.Reason
		}
		if strings.Contains(all, secret) {
			return fmt.Errorf("the state mentions %q: %q", secret, all)
		}
		return nil
	})
	sc.Step(`^the MCP Server refuses to start$`, func() error { w.mc.fake.FailInit = true; return nil })
	sc.Step(`^the MCP Server speaks protocol version "([^"]*)"$`, func(v string) error { w.mc.fake.Version = v; return nil })
	sc.Step(`^the MCP Server lists its tools one per page$`, func() error { w.mc.fake.PageSize = 1; return nil })
	sc.Step(`^the MCP Server never answers a call$`, func() error {
		w.mc.fake.Silent = true
		w.mc.cfg.TimeoutSec = 1
		return nil
	})
	sc.Step(`^the MCP Server exits$`, func() error {
		w.act(func() error { w.mc.fake.Crash(); return nil })
		return nil
	})
	sc.Step(`^the user restarts the MCP Server "([^"]*)"$`, func(name string) error {
		w.act(func() error {
			err := w.mc.client.Restart(context.Background(), name)
			w.set.SetText("mcp", w.mc.client.Instructions())
			return err
		})
		return nil
	})
	sc.Step(`^the user switches the MCP Server "([^"]*)" off$`, func(name string) error {
		w.act(func() error {
			if err := w.mc.client.SetEnabled(name, false); err != nil {
				return err
			}
			return w.set.SetText("mcp", w.mc.client.Instructions())
		})
		return nil
	})

	sc.Step(`^the request to the Provider offers the Tools "([^"]*)"$`, func(list string) error {
		if err := w.settle(); err != nil {
			return err
		}
		req, err := w.request(0)
		if err != nil {
			return err
		}
		offered := map[string]bool{}
		for _, t := range offeredNames(req) {
			offered[t] = true
		}
		for _, want := range strings.Split(list, ",") {
			if want = strings.TrimSpace(want); !offered[want] {
				return fmt.Errorf("the request offers %v, want %s", offeredNames(req), want)
			}
		}
		return nil
	})
	sc.Step(`^the Tool "([^"]*)" is offered with a description of (\d+) characters$`, func(name string, n int) error {
		if err := w.settle(); err != nil {
			return err
		}
		req, err := w.request(0)
		if err != nil {
			return err
		}
		for _, t := range req["tools"].([]any) {
			fn := t.(map[string]any)["function"].(map[string]any)
			if fn["name"] == name {
				if got := len([]rune(fn["description"].(string))); got != n {
					return fmt.Errorf("description of %s has %d characters, want %d", name, got, n)
				}
				return nil
			}
		}
		return fmt.Errorf("%s is not offered", name)
	})
	sc.Step(`^a start warning names the MCP Server "([^"]*)"$`, func(name string) error {
		if err := w.settle(); err != nil {
			return err
		}
		for _, s := range w.mc.warnings {
			if strings.Contains(s, `"`+name+`"`) {
				return nil
			}
		}
		return fmt.Errorf("start warnings = %q, want one naming %q", w.mc.warnings, name)
	})
	sc.Step(`^the MCP Server "([^"]*)" is reported (running|failed|crashed|off)$`, func(name, state string) error {
		if err := w.settle(); err != nil {
			return err
		}
		for _, s := range w.mc.client.Servers() {
			if s.Name == name {
				if string(s.State) != state {
					return fmt.Errorf("server %s is %s (%s), want %s", name, s.State, s.Reason, state)
				}
				return nil
			}
		}
		return errors.New("unknown server " + name)
	})
}

// offeredNames lists the function names a wire request offers.
func offeredNames(req map[string]any) []string {
	var out []string
	offered, _ := req["tools"].([]any)
	for _, t := range offered {
		if m, ok := t.(map[string]any); ok {
			if fn, ok := m["function"].(map[string]any); ok {
				out = append(out, fmt.Sprint(fn["name"]))
			}
		}
	}
	return out
}
