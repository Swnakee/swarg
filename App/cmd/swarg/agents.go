package main

import (
	"fmt"
	"path/filepath"

	"swarg/internal/agent"
	"swarg/internal/agents"
	"swarg/internal/config"
	"swarg/internal/prompt"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// Sub-agents in the app (R-120..R-123): the types, their Sources and prompts, /agents
// ---------------------------------------------------------------------------

// subagentsBlock is the prompt layer that carries the sub-agent policy (prompt package).
const subagentsBlock = "subagents"

// agentHost joins the agent types to the runtime: which types are offered, and how a run gets
// its Source and its system prompt.
type agentHost struct {
	set     *agents.Set
	cfg     *config.Config
	ps      *prompt.Set
	srcs    *sources
	runner  toolRunner
	profile func() string // the Session's prompt profile in force
	model   func() string // the Session's model
}

// Types are the enabled types, none under the compact profile: compact never offers or
// describes the `agent` Tool (R-123, I-40).
func (h *agentHost) Types() []agent.AgentType {
	reloadAgents(h.set, h.ps)
	if h.profile() == string(prompt.Compact) {
		return nil
	}
	var out []agent.AgentType
	for _, t := range h.set.Enabled() {
		out = append(out, agent.AgentType{Name: t.Name, Description: t.Description, Tools: t.Tools, MaxSteps: t.MaxSteps,
			Server: t.Server, Model: t.Model, Profile: t.Profile, Body: t.Body})
	}
	return out
}

// reloadAgents re-reads the agent folders when a file changed and rewrites the `subagents` prompt
// layer, so an edited type applies without a restart (R-129).
func reloadAgents(set *agents.Set, ps *prompt.Set) {
	if changed, _ := set.Reload(); changed {
		_ = ps.SetText(subagentsBlock, set.Block())
	}
}

// inherits says whether a type field leaves the choice to the Session.
func inherits(v string) bool { return v == "" || v == "inherit" }

// Open resolves the Source of a run (the type's server and model, else the Session's) and builds
// its system prompt: the type's profile, the Tool lines it may use, its body.
func (h *agentHost) Open(t agent.AgentType) (agent.AgentRun, error) {
	server := h.srcs.Current()
	if !inherits(t.Server) {
		server = t.Server
	}
	p, err := h.cfg.Ready(server)
	if err != nil {
		return agent.AgentRun{}, err
	}
	model := h.model()
	switch {
	case !inherits(t.Model):
		model = t.Model
	case server != h.srcs.Current():
		model = h.srcs.recent.First(server, p.Model)
	}
	profile := prompt.Compact
	if !inherits(t.Profile) {
		if pr, err := prompt.ParseProfile(t.Profile); err == nil {
			profile = pr
		}
	}
	allowed := func(name string) bool {
		known, ro, _ := h.runner.Kind(name)
		return known && t.Allows(name, ro)
	}
	return agent.AgentRun{Source: h.srcs.source(server, model), System: h.ps.BuildAgent(profile, model, t.Body, allowed)}, nil
}

// Slots is how many agent requests the Source takes at once (providers.<p>.max_parallel).
func (h *agentHost) Slots(source string) int { return h.cfg.Providers[source].Parallel() }

// Deps is the agent.Deps.Agents value.
func (h *agentHost) Deps() agent.AgentDeps {
	return agent.AgentDeps{Types: h.Types, Open: h.Open, Slots: h.Slots, Max: h.cfg.Agents.MaxParallel}
}

// agentsView adapts *agents.Set to ui.AgentsAPI; a switch rewrites the `subagents` prompt layer
// and writes `agents.disabled` (every type that is off) into the scope's file (R-98).
type agentsView struct {
	set *agents.Set
	ps  *prompt.Set
	sc  scopes
	cwd string
	dir string // <config dir>/agents
}

func (v agentsView) List() []ui.AgentRow {
	reloadAgents(v.set, v.ps)
	var rows []ui.AgentRow
	for _, a := range v.set.List() {
		rows = append(rows, ui.AgentRow{Name: a.Name, Description: a.Description, Source: a.Source, Path: a.Path, Enabled: a.Enabled})
	}
	return rows
}

func (v agentsView) SetEnabled(name string, on bool, scope string) error {
	if err := v.set.SetEnabled(name, on); err != nil {
		return err
	}
	if err := v.ps.SetText(subagentsBlock, v.set.Block()); err != nil {
		return err
	}
	off := []string{}
	for _, a := range v.set.List() {
		if !a.Enabled {
			off = append(off, a.Name)
		}
	}
	return notSaved(v.sc.set(scope, "agents.disabled", off))
}

// Edit returns the file that holds the type. A built-in has none, so its full definition is
// copied to the global folder first (never over an existing file); the copy then replaces the
// built-in on the next reload (R-129).
func (v agentsView) Edit(name string) (string, bool, error) {
	reloadAgents(v.set, v.ps)
	t, ok := v.set.Get(name)
	if !ok {
		return "", false, fmt.Errorf("unknown agent type %q", name)
	}
	if t.Source != agents.SourceBuiltin {
		return t.Path, false, nil
	}
	path, created, err := agents.ExportBuiltin(v.dir, name)
	if err == nil {
		reloadAgents(v.set, v.ps)
	}
	return path, created, err
}

// New writes a template type file in the scope's folder (global by default, R-128); it is re-read
// on the next /agents or agent run.
func (v agentsView) New(name, scope string) (string, error) {
	switch scope {
	case "project":
		return agents.NewFile(filepath.Join(v.cwd, filepath.FromSlash(agents.ProjectDir)), name)
	case "global", "":
		return agents.NewFile(v.dir, name)
	}
	return "", fmt.Errorf("unknown scope %q (global or project)", scope)
}

var _ ui.AgentsAPI = agentsView{}
