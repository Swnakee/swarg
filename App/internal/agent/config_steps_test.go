package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/config"
	"swarg/internal/mcp"
	"swarg/internal/mcp/mcptest"
)

// ---------------------------------------------------------------------------
// Steps for config.feature: the real config package (layers, guard, approvals) and the real
// mcp.Client over the in-process fake server.
// ---------------------------------------------------------------------------

type configWorld struct {
	dir, cwd string // the user config dir and the project
	cfg      config.Config
	layers   config.Layers
	err      error
	loaded   bool
	client   *mcp.Client
}

func (w *world) cfgWorld() *configWorld {
	if w.cf == nil {
		w.cf = &configWorld{dir: w.tb.TempDir(), cwd: w.tb.TempDir()}
	}
	return w.cf
}

func (c *configWorld) global() string { return filepath.Join(c.dir, "config.json") }
func (c *configWorld) project() string {
	return filepath.Join(c.cwd, filepath.FromSlash(config.ProjectConfig))
}

// load runs the layered load once (Given steps that need the result call it too).
func (c *configWorld) load() error {
	if c.loaded {
		return nil
	}
	c.cfg, c.layers, c.err = config.LoadLayered(c.global(), false, c.cwd)
	c.loaded = true
	return c.err
}

// approvals is the store of the user config dir.
func (c *configWorld) approvals() (*config.Approvals, error) { return config.LoadApprovals(c.dir) }

func registerConfigSteps(sc *godog.ScenarioContext, w *world) {
	set := func(project bool, key string, v any) error {
		c := w.cfgWorld()
		if project {
			return config.Set(c.project(), key, v, false) // written like a hand-edited file: the guard is the loader's job
		}
		return config.Set(c.global(), key, v, false)
	}
	sc.Step(`^the global config sets the max steps to (\d+)$`, func(n int) error { return set(false, "tools.max_steps", n) })
	sc.Step(`^the project config sets the max steps to (\d+)$`, func(n int) error { return set(true, "tools.max_steps", n) })
	sc.Step(`^the global config defines the MCP Server "([^"]*)"$`, func(n string) error { return set(false, "mcp.servers."+n+".command", "fake") })
	sc.Step(`^the project config defines the MCP Server "([^"]*)"$`, func(n string) error { return set(true, "mcp.servers."+n+".command", "fake") })
	sc.Step(`^the project config defines the MCP Server "([^"]*)" as trusted$`, func(n string) error {
		if err := set(true, "mcp.servers."+n+".command", "fake"); err != nil {
			return err
		}
		return set(true, "mcp.servers."+n+".trust", true)
	})
	sc.Step(`^the project config switches auto-approve on$`, func() error { return set(true, "tools.auto_approve", true) })
	sc.Step(`^the project has no config of its own$`, func() error { w.cfgWorld(); return nil })
	sc.Step(`^the project has a config of its own$`, func() error { return set(true, "tools.max_steps", 20) })
	sc.Step(`^the project config changes the arguments of the MCP Server "([^"]*)"$`, func(n string) error {
		w.cfgWorld().loaded = false
		return set(true, "mcp.servers."+n+".args", []string{"v2"})
	})
	sc.Step(`^the user approved the MCP Server "([^"]*)" for this project$`, func(n string) error {
		c := w.cfgWorld()
		if err := c.load(); err != nil {
			return err
		}
		a, err := c.approvals()
		if err != nil {
			return err
		}
		return a.Approve(c.cwd, n, c.cfg.MCP.Servers[n])
	})

	sc.Step(`^swarg loads the configuration$`, func() error { w.cfgWorld().load(); return nil })
	sc.Step(`^the user switches the Skill "([^"]*)" off in the project scope$`, func(name string) error {
		return config.Set(w.cfgWorld().project(), "skills.disabled", []string{name}, true)
	})
	sc.Step(`^the MCP Servers start$`, func() error {
		c := w.cfgWorld()
		if err := c.load(); err != nil {
			return err
		}
		a, err := c.approvals()
		if err != nil {
			return err
		}
		fake := mcptest.New(mcptest.Tool{Name: "a", Description: "the a tool"})
		c.client = mcp.New(c.cfg.MCP.Servers, mcp.Deps{Confirm: true, Dial: fake.Dial, Project: c.layers.ProjectServers,
			Approved: func(n string) bool { return a.Approved(c.cwd, n, c.cfg.MCP.Servers[n]) }})
		w.tb.Cleanup(c.client.Close)
		c.client.Start(context.Background())
		return nil
	})

	sc.Step(`^the effective max steps are (\d+)$`, func(n int) error {
		c := w.cfgWorld()
		if c.err != nil {
			return c.err
		}
		if c.cfg.Tools.MaxSteps != n {
			return fmt.Errorf("max steps %d, want %d", c.cfg.Tools.MaxSteps, n)
		}
		return nil
	})
	sc.Step(`^the configured MCP Servers are "([^"]*)"$`, func(list string) error {
		c := w.cfgWorld()
		if c.err != nil {
			return c.err
		}
		var names []string
		for n := range c.cfg.MCP.Servers {
			names = append(names, n)
		}
		sort.Strings(names)
		if got := strings.Join(names, ", "); got != list {
			return fmt.Errorf("servers %q, want %q", got, list)
		}
		return nil
	})
	warned := func(key string) error {
		for _, s := range w.cfgWorld().layers.Warnings {
			if strings.Contains(s, key) {
				return nil
			}
		}
		return fmt.Errorf("warnings %q name no %q", w.cfgWorld().layers.Warnings, key)
	}
	sc.Step(`^auto-approve stays off and a warning names "([^"]*)"$`, func(key string) error {
		c := w.cfgWorld()
		if c.err != nil {
			return c.err
		}
		if c.cfg.Tools.AutoApprove {
			return errors.New("auto-approve is on")
		}
		return warned(key)
	})
	sc.Step(`^the MCP Server "([^"]*)" is loaded untrusted and a warning names "([^"]*)"$`, func(n, key string) error {
		c := w.cfgWorld()
		if c.err != nil {
			return c.err
		}
		if c.cfg.MCP.Servers[n].Trust {
			return fmt.Errorf("server %s is trusted", n)
		}
		return warned(key)
	})
	sc.Step(`^the project config lists the Skill "([^"]*)" as disabled$`, func(name string) error {
		data, err := os.ReadFile(w.cfgWorld().project())
		if err != nil {
			return err
		}
		if !strings.Contains(string(data), `"`+name+`"`) {
			return fmt.Errorf("project file:\n%s", data)
		}
		c := w.cfgWorld()
		c.loaded = false
		if err := c.load(); err != nil {
			return err
		}
		if len(c.cfg.Skills.Disabled) != 1 || c.cfg.Skills.Disabled[0] != name {
			return fmt.Errorf("disabled %v", c.cfg.Skills.Disabled)
		}
		return nil
	})
	sc.Step(`^the MCP Server "([^"]*)" starts as "([^"]*)"$`, func(n, state string) error {
		for _, s := range w.cfgWorld().client.Servers() {
			if s.Name == n {
				if string(s.State) != state {
					return fmt.Errorf("server %s is %s (%s), want %s", n, s.State, s.Reason, state)
				}
				return nil
			}
		}
		return errors.New("unknown server " + n)
	})
}
