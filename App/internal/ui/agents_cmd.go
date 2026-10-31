package ui

import "fmt"

// ---------------------------------------------------------------------------
// /agents
// ---------------------------------------------------------------------------

// agentsCmd opens the Agents menu, switches one type, or creates a new type file.
func (m *model) agentsCmd(c Command) {
	if m.d.Agents == nil {
		m.say("no sub-agent types available")
		return
	}
	if c.Name == "" {
		m.openMenu(m.agentsMenu(), nil)
		return
	}
	scope := m.agentScope(c.Scope)
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
	note, err := saved(scope, fmt.Sprintf("agent %s: %s", c.Name, onOff(c.On)), m.d.Agents.SetEnabled(c.Name, c.On, scope))
	if err != nil {
		m.say(err.Error())
		return
	}
	m.say(note + " (from the next request)")
}

// agentScope is the save scope of /agents: the given one, else global even when the project has
// its own .swarg folder (R-128); nil Scopes = "" (this run only).
func (m *model) agentScope(given string) string {
	if given != "" || m.d.Scopes == nil {
		return given
	}
	return "global"
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
func (m *model) agentsMenu() *menu {
	mn := newMenu("Agents", nil)
	mn.hint = "↑/↓ move   Space on/off   e edit   Tab scope   Esc close   type to filter"
	mn.empty = "no agent types   Esc close"
	rebuild := func() {
		var cells [][]string
		rows := append([]AgentRow(nil), m.d.Agents.List()...) // a copy: the Path is filled in below
		for i := range rows {
			if rows[i].Path == "" {
				rows[i].Path = "(built-in)"
			}
		}
		for _, a := range rows {
			mark := markOn
			if !a.Enabled {
				mark = markOff
			}
			cells = append(cells, []string{"  " + mark + " " + Clean(a.Name), a.Source, Clean(a.Description), Clean(a.Path)})
		}
		items := make([]menuRow, len(rows))
		for i, t := range columns(cells) {
			items[i] = menuRow{ID: rows[i].Name, Text: t, Match: rows[i].Name + " " + rows[i].Description, Right: onOff(!rows[i].Enabled)}
		}
		mn.setItems(items)
	}
	toggle := func(id string) {
		for _, a := range m.d.Agents.List() {
			if a.Name == id {
				note, err := saved(mn.scope, "agent "+id+": "+onOff(!a.Enabled), m.d.Agents.SetEnabled(id, !a.Enabled, mn.scope))
				rebuild()
				if err != nil {
					note = err.Error()
				}
				mn.note = note
				return
			}
		}
	}
	mn.keys["space"], mn.onRight = toggle, toggle
	mn.onScope = func(string) {}
	mn.keys["e"] = func(id string) {
		if id != "" {
			mn.note = m.editAgent(id)
			rebuild()
		}
	}
	mn.onChoose = func(id string) bool {
		for _, a := range m.d.Agents.List() {
			if a.Name == id {
				where := a.Path
				if where == "" {
					where = "built-in"
				}
				mn.note = id + ": " + where
			}
		}
		return false
	}
	rebuild()
	if m.d.Scopes != nil {
		mn.scopes, mn.scope = m.d.Scopes.Scopes(), "global" // not the project default (R-128)
	}
	return mn
}
