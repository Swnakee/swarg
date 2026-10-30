package main

import (
	"context"
	"encoding/json"
	"path/filepath"

	"swarg/internal/agent"
	"swarg/internal/chat"
	"swarg/internal/mcp"
	"swarg/internal/tools"
)

// ---------------------------------------------------------------------------
// Tool adapter
// ---------------------------------------------------------------------------

// toolRunner adapts *tools.Set, bound to one project environment, to agent.ToolRunner; names
// with the mcp__ prefix go to the MCP client (nil = no MCP), everything else to the Set.
type toolRunner struct {
	set *tools.Set
	env tools.Env
	mcp *mcp.Client
}

var (
	_ agent.ToolRunner  = toolRunner{}
	_ agent.ToolStubber = toolRunner{}
)

func (r toolRunner) Specs() []chat.ToolSpec {
	specs := r.set.Specs()
	if r.mcp != nil {
		specs = append(specs, r.mcp.Specs()...) // read on every request: /mcp changes show at the next Step
	}
	return specs
}

func (r toolRunner) Kind(name string) (known, readOnly, confirm bool) {
	if r.mcp != nil && mcp.Owns(name) {
		return r.mcp.Kind(name)
	}
	return r.set.Kind(name)
}

// Detachable: only a shell command can move to the background.
func (r toolRunner) Detachable(name string) bool { return name == "shell" }

func (r toolRunner) Label(call chat.ToolCall) string {
	if r.mcp != nil && mcp.Owns(call.Name) {
		return r.mcp.Label(call.Name, call.Args)
	}
	return r.set.Label(call.Name, json.RawMessage(call.Args))
}

// Run binds the per-call hooks to a copy of the shared env (Jobs stays shared).
func (r toolRunner) Run(ctx context.Context, call chat.ToolCall, hooks agent.ToolHooks) (output, brief string, err error) {
	if r.mcp != nil && mcp.Owns(call.Name) {
		return r.mcp.Run(ctx, call.Name, call.Args)
	}
	env := r.env
	env.Progress, env.Detach = hooks.Progress, hooks.Detach
	res, err := r.set.Run(ctx, env, call.Name, json.RawMessage(call.Args))
	return res.Output, res.Brief, err
}

// Stub forwards to the tool's history policy.
func (r toolRunner) Stub(call chat.ToolCall, output, brief string) string {
	if r.mcp != nil && mcp.Owns(call.Name) {
		return r.mcp.Stub(call.Name)
	}
	return r.set.Stub(call.Name, json.RawMessage(call.Args), output, brief)
}

// Touched is the project-relative path a file tool changed; an absolute path inside the
// project is made relative so reads and edits of one file compare equal.
func (r toolRunner) Touched(call chat.ToolCall) string {
	if mcp.Owns(call.Name) {
		return ""
	}
	p := r.set.Touched(call.Name, json.RawMessage(call.Args))
	if p == "" || !filepath.IsAbs(filepath.FromSlash(p)) {
		return p
	}
	rel, err := tools.Rel(r.env.Dir, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(rel)
}
