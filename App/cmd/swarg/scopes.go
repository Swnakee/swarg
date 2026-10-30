package main

import (
	"fmt"
	"os"
	"path/filepath"

	"swarg/internal/config"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// Save scopes and /config (R-98): the global file and the project's .swarg/config.json
// ---------------------------------------------------------------------------

// scopes knows the two config files of this run.
type scopes struct {
	global  string // <config dir>/config.json
	project string // <cwd>/.swarg/config.json
}

func newScopes(dir, cwd string) scopes {
	return scopes{global: filepath.Join(dir, "config.json"), project: filepath.Join(cwd, filepath.FromSlash(config.ProjectConfig))}
}

func (s scopes) Scopes() []string { return []string{"project", "global"} }

// Default is project when the project has a .swarg folder, else global.
func (s scopes) Default() string {
	if st, err := os.Stat(filepath.Dir(s.project)); err == nil && st.IsDir() {
		return "project"
	}
	return "global"
}

// set writes one key into the scope's file ("" = nothing is written).
func (s scopes) set(scope, key string, value any) error {
	switch scope {
	case "":
		return nil
	case "global":
		return config.Set(s.global, key, value, false)
	case "project":
		return config.Set(s.project, key, value, true)
	}
	return fmt.Errorf("unknown scope %q (global or project)", scope)
}

// configView adapts the loaded config and the scopes to ui.ConfigAPI: the main settings as rows,
// a change written to the scope's file and re-read so the rows show the effective values.
type configView struct {
	sc       scopes
	cwd      string
	cfg      *config.Config
	layers   *config.Layers
	override func(*config.Config) // the command-line overrides (--model, --base-url), re-applied after a reload
}

func (v *configView) Settings() []ui.Setting {
	p := "providers." + v.cfg.Provider + "."
	rows := []ui.Setting{
		{Key: p + "model", Label: "model", Kind: "string", Value: v.cfg.Active().Model, Live: "model"},
		{Key: p + "thinking", Label: "thinking", Kind: "bool", Value: v.cfg.Active().Thinking, Live: "think"},
		{Key: "tools.auto_approve", Label: "auto-approve tools", Kind: "bool", Value: v.cfg.Tools.AutoApprove, Live: "auto"},
		{Key: "tools.max_steps", Label: "max steps per turn", Kind: "int", Value: v.cfg.Tools.MaxSteps, Min: 1, Max: 200, Step: 5},
		{Key: "web.confirm", Label: "confirm web calls", Kind: "bool", Value: v.cfg.Web.Confirm},
		{Key: "mcp.confirm", Label: "confirm MCP calls", Kind: "bool", Value: v.cfg.MCP.Confirm},
		{Key: "session.save", Label: "save sessions", Kind: "bool", Value: v.cfg.Session.Save},
		{Key: "context.auto_compact", Label: "auto-compact at %", Kind: "int", Value: v.cfg.Context.AutoCompact, Min: 50, Max: 95, Step: 5},
		{Key: "tasks.max_depth", Label: "task nesting depth", Kind: "int", Value: v.cfg.Tasks.MaxDepth, Min: 0, Max: 8, Step: 1},
	}
	for i := range rows {
		rows[i].Scope = "global"
		if v.layers.FromProject(rows[i].Key) {
			rows[i].Scope = "project"
		}
	}
	return rows
}

// Set writes the key and reloads the layered config (the file must still load).
func (v *configView) Set(scope, key string, value any) error {
	if err := v.sc.set(scope, key, value); err != nil {
		return err
	}
	cfg, layers, err := config.LoadLayered(v.sc.global, false, v.cwd)
	if err != nil {
		return err
	}
	if v.override != nil {
		v.override(&cfg)
	}
	*v.cfg, *v.layers = cfg, layers
	return nil
}
