package main

import (
	"context"

	"swarg/internal/mcp"
	"swarg/internal/prompt"
	"swarg/internal/skills"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// UI adapters for Skills and MCP (ui imports neither package)
// ---------------------------------------------------------------------------

// skillsView adapts *skills.Set to ui.SkillsAPI; a switch rewrites the `skills` prompt block and
// writes `skills.disabled` (every Skill that is off) into the scope's file (R-98).
type skillsView struct {
	set *skills.Set
	ps  *prompt.Set
	sc  scopes
}

func (v skillsView) List() []ui.SkillRow {
	var rows []ui.SkillRow
	for _, s := range v.set.List() {
		rows = append(rows, ui.SkillRow{Name: s.Name, Description: s.Description, Source: s.Source, Enabled: s.Enabled})
	}
	return rows
}

func (v skillsView) SetEnabled(name string, on bool, scope string) error {
	if err := v.set.SetEnabled(name, on); err != nil {
		return err
	}
	if err := v.ps.SetText("skills", v.set.Block()); err != nil {
		return err
	}
	off := []string{}
	for _, s := range v.set.List() {
		if !s.Enabled {
			off = append(off, s.Name)
		}
	}
	return notSaved(v.sc.set(scope, "skills.disabled", off))
}

// notSaved marks a failed write of a switch that already applied (the UI shows both).
func notSaved(err error) error {
	if err != nil {
		return &ui.NotSavedError{Err: err}
	}
	return nil
}

// mcpView adapts *mcp.Client to ui.MCPAPI; a switch or restart rewrites the `mcp` prompt block; a
// switch also writes `mcp.servers.<name>.disabled` into the scope's file (R-98).
type mcpView struct {
	c  *mcp.Client
	ps *prompt.Set
	sc scopes
}

func (v mcpView) Servers() []ui.MCPServerRow {
	var rows []ui.MCPServerRow
	for _, s := range v.c.Servers() {
		rows = append(rows, ui.MCPServerRow{Name: s.Name, State: string(s.State), Reason: s.Reason, Tools: s.Tools, Enabled: s.Enabled, Source: s.Source, Transport: s.Transport})
	}
	return rows
}

func (v mcpView) Tools(server string) ([]ui.MCPToolRow, string, error) {
	list, stderr, err := v.c.Tools(server)
	var rows []ui.MCPToolRow
	for _, t := range list {
		rows = append(rows, ui.MCPToolRow{Name: t.Name, Description: t.Description, ReadOnly: t.ReadOnly, Offered: t.Offered})
	}
	return rows, stderr, err
}

func (v mcpView) SetEnabled(server string, on bool, scope string) error {
	if err := v.c.SetEnabled(server, on); err != nil {
		return err
	}
	if err := v.ps.SetText("mcp", v.c.Instructions()); err != nil {
		return err
	}
	return notSaved(v.sc.set(scope, "mcp.servers."+server+".disabled", !on))
}

func (v mcpView) Approve(server string) error {
	err := v.c.Approve(context.Background(), server)
	v.ps.SetText("mcp", v.c.Instructions())
	return err
}

func (v mcpView) Restart(server string) error {
	err := v.c.Restart(context.Background(), server)
	v.ps.SetText("mcp", v.c.Instructions())
	return err
}
