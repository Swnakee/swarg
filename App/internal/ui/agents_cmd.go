package ui

import (
	"fmt"

	"swarg/internal/ui/command"
	"swarg/internal/ui/menu"
)

// ---------------------------------------------------------------------------
// /agents
// ---------------------------------------------------------------------------

// agentsCmd opens the Agents menu, switches one type, or creates a new type file.
func (m *model) agentsCmd(c command.Command) {
	if m.d.Agents == nil {
		m.say("no sub-agent types available")
		return
	}
	if c.Name == "" {
		m.openMenu(m.agentsMenu(), nil)
		return
	}
	scope := m.scopeOr(c.Scope, "global") // global even when the project has its own .swarg folder (R-128)
	if c.Text == "edit" {
		m.say(m.editAgent(c.Name))
		return
	}
	if c.Text == "new" {
		path, err := m.d.Agents.New(c.Name, scope)
		if err != nil {
			m.say("agents new: " + err.Error())
			return
		}
		m.say("agent type file created: " + path + " (edit its description and body; it is re-read on the next /agents or agent run)")
		return
	}
	m.sayToggle(scope, fmt.Sprintf("agent %s: %s", c.Name, onOff(c.On)), m.d.Agents.SetEnabled(c.Name, c.On, scope))
}

// editAgent says which file holds the type; a built-in is first copied to the global folder.
func (m *model) editAgent(name string) string {
	path, created, err := m.d.Agents.Edit(name)
	switch {
	case err != nil:
		return "agents edit: " + err.Error()
	case created:
		return "agent " + name + ": built-in copied to " + path + " - edit it; the file replaces the built-in (re-read on the next /agents or agent run)"
	}
	return "agent " + name + ": edit " + path + " (re-read on the next /agents or agent run)"
}

// agentsMenu lists the sub-agent types: Space or the label switches one (saved to the scope), Tab
// cycles the scope, Enter shows the type's file in the footer and keeps the menu open.
func (m *model) agentsMenu() *menu.Menu {
	var mn *menu.Menu
	o := menuOpts("Agents", nil)
	o.Hint = "↑/↓ move   Space on/off   e edit   Tab scope   Esc close   type to filter"
	o.Empty = "no agent types   Esc close"
	rebuild := func() {
		var cells [][]string
		rows := append([]AgentRow(nil), m.d.Agents.List()...) // a copy: the Path is filled in below
		for i := range rows {
			if rows[i].Path == "" {
				rows[i].Path = "(built-in)"
			}
		}
		for _, a := range rows {
			cells = append(cells, []string{"  " + pickMark(a.Enabled, markOn, markOff) + " " + Clean(a.Name), a.Source, Clean(a.Description), Clean(a.Path)})
		}
		mn.SetRows(columnRows(cells, func(i int) menu.Row {
			return menu.Row{ID: rows[i].Name, Match: rows[i].Name + " " + rows[i].Description, Right: onOff(!rows[i].Enabled)}
		}))
	}
	toggle := func(id string) {
		for _, a := range m.d.Agents.List() {
			if a.Name == id {
				toggleNote(mn, rebuild, "agent "+id+": "+onOff(!a.Enabled), m.d.Agents.SetEnabled(id, !a.Enabled, mn.Scope()))
				return
			}
		}
	}
	o.Keys["space"], o.OnRight = toggle, toggle
	o.OnScope = func(string) {}
	o.Keys["e"] = func(id string) {
		if id != "" {
			mn.SetNote(m.editAgent(id))
			rebuild()
		}
	}
	o.OnChoose = func(id string) bool {
		for _, a := range m.d.Agents.List() {
			if a.Name == id {
				where := a.Path
				if where == "" {
					where = "built-in"
				}
				mn.SetNote(id + ": " + where)
			}
		}
		return false
	}
	mn = menu.New(o)
	rebuild()
	if m.d.Scopes != nil {
		mn.SetScopes(m.d.Scopes.Scopes(), "global") // not the project default (R-128)
	}
	return mn
}
