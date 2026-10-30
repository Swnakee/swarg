package ui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// ---------------------------------------------------------------------------
// The menus of the app on the one engine (R-99): /skills, /mcp, /model, /think, /config
// ---------------------------------------------------------------------------

const (
	markOn  = "◉"
	markOff = "○"
)

// openMenu shows a menu in the list area, registering its spots in the host's hover.
func (m *model) openMenu(mn *menu, parent *menu) {
	mn.hv, mn.parent = &m.hv, parent
	if m.d.Scopes != nil && mn.scopes == nil && mn.onScope != nil {
		mn.scopes, mn.scope = m.d.Scopes.Scopes(), m.d.Scopes.Default()
	}
	m.pick = mn
}

// scopeOf is the save scope of a command: the given one, else the default (nil Scopes = "").
func (m *model) scopeOf(given string) string {
	if given != "" || m.d.Scopes == nil {
		return given
	}
	return m.d.Scopes.Default()
}

// saved is the footer note after a switch: persisted to the scope, this run only (no scope), or
// applied but not written (a NotSavedError carries the reason); any other error is returned.
func saved(scope, what string, err error) (string, error) {
	var ns *NotSavedError
	switch {
	case errors.As(err, &ns):
		return what + " (this run only; " + ns.Error() + ")", nil
	case err != nil:
		return "", err
	case scope == "":
		return what + " (this run)", nil
	}
	return what + " (saved to " + scope + ")", nil
}

// columns joins cells with two blanks, padding each but the last to the width of its column.
func columns(rows [][]string) []string {
	var w []int
	for _, r := range rows {
		for i, c := range r {
			for len(w) <= i {
				w = append(w, 0)
			}
			w[i] = max(w[i], lipgloss.Width(c))
		}
	}
	out := make([]string, len(rows))
	for k, r := range rows {
		var b strings.Builder
		for i, c := range r {
			b.WriteString(c)
			if i < len(r)-1 {
				b.WriteString(strings.Repeat(" ", w[i]-lipgloss.Width(c)+2))
			}
		}
		out[k] = strings.TrimRight(b.String(), " ")
	}
	return out
}

// ---------------------------------------------------------------------------
// /skills
// ---------------------------------------------------------------------------

// skillsMenu lists the Skills: Enter applies one (like /skill name), Space or the label switches
// it (saved to the scope), Tab cycles the scope.
func (m *model) skillsMenu() *menu {
	mn := newMenu("Skills", nil)
	mn.hint = "↑/↓ move   Enter apply   Space on/off   Esc close   type to filter"
	mn.empty = "no skills   Esc close"
	rebuild := func() {
		var cells [][]string
		var rows []SkillRow
		for _, s := range m.d.Skills.List() {
			mark := markOn
			if !s.Enabled {
				mark = markOff
			}
			rows = append(rows, s)
			cells = append(cells, []string{"  " + mark + " " + Clean(s.Name), s.Source, Clean(s.Description)})
		}
		items := make([]menuRow, len(rows))
		for i, t := range columns(cells) {
			items[i] = menuRow{ID: rows[i].Name, Text: t, Match: rows[i].Name + " " + rows[i].Description, Right: onOff(!rows[i].Enabled)}
		}
		mn.setItems(items)
	}
	toggle := func(id string) {
		for _, s := range m.d.Skills.List() {
			if s.Name == id {
				note, err := saved(mn.scope, "skill "+id+": "+onOff(!s.Enabled), m.d.Skills.SetEnabled(id, !s.Enabled, mn.scope))
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
	mn.onChoose = func(id string) bool {
		m.submitSkill(id, "")
		return true
	}
	rebuild()
	return mn
}

// ---------------------------------------------------------------------------
// /mcp
// ---------------------------------------------------------------------------

// mcpMenu lists the servers: Enter opens one (its tools), Space / the label switches it (saved to
// the scope), r restarts, a approves a project server.
func (m *model) mcpMenu() *menu {
	mn := newMenu("MCP servers", nil)
	mn.hint = "↑/↓ move   Enter tools   Space on/off   r restart   a approve   Esc close"
	mn.empty = "no MCP servers configured   Esc close"
	find := func(id string) (MCPServerRow, bool) {
		for _, s := range m.d.MCP.Servers() {
			if s.Name == id {
				return s, true
			}
		}
		return MCPServerRow{}, false
	}
	rebuild := func() {
		var cells [][]string
		var rows []MCPServerRow
		for _, s := range m.d.MCP.Servers() {
			state := s.State
			if !s.Enabled && state == "running" {
				state = "hidden"
			}
			rows = append(rows, s)
			cells = append(cells, []string{"  " + Clean(s.Name), state, fmt.Sprintf("%d tools", s.Tools), s.Transport, s.Source, Clean(s.Reason)})
		}
		items := make([]menuRow, len(rows))
		for i, t := range columns(cells) {
			right := onOff(!rows[i].Enabled)
			if rows[i].State == "needs approval" {
				right = "approve"
			}
			items[i] = menuRow{ID: rows[i].Name, Text: t, Match: rows[i].Name, Right: right}
		}
		mn.setItems(items)
	}
	act := func(id string, f func(MCPServerRow) (string, error)) {
		s, ok := find(id)
		if !ok {
			return
		}
		note, err := f(s)
		rebuild()
		if err != nil {
			note = "mcp " + id + ": " + err.Error()
		}
		mn.note = note
	}
	toggle := func(id string) {
		act(id, func(s MCPServerRow) (string, error) {
			return saved(mn.scope, "mcp "+id+": "+onOff(!s.Enabled), m.d.MCP.SetEnabled(id, !s.Enabled, mn.scope))
		})
	}
	approve := func(id string) {
		act(id, func(MCPServerRow) (string, error) { return "mcp " + id + ": approved and started", m.d.MCP.Approve(id) })
	}
	mn.keys["space"] = toggle
	mn.keys["r"] = func(id string) {
		act(id, func(MCPServerRow) (string, error) { return "mcp " + id + ": running", m.d.MCP.Restart(id) })
	}
	mn.keys["a"] = approve
	mn.onRight = func(id string) {
		if s, ok := find(id); ok && s.State == "needs approval" {
			approve(id)
			return
		}
		toggle(id)
	}
	mn.onScope = func(string) {}
	mn.onChoose = func(id string) bool {
		if sub := m.mcpServerMenu(id); sub != nil {
			m.openMenu(sub, mn)
		}
		return false
	}
	rebuild()
	return mn
}

// mcpServerMenu shows the tools of one server (offered or not, read-only marked) with the stderr
// tail as the footer note; nil with a notice when the server is unknown.
func (m *model) mcpServerMenu(name string) *menu {
	tools, stderr, err := m.d.MCP.Tools(name)
	if err != nil {
		m.note(err.Error())
		return nil
	}
	var cells [][]string
	for _, t := range tools {
		note := ""
		if t.ReadOnly {
			note = "read-only"
		}
		if !t.Offered {
			note = strings.TrimSpace(note + " not offered")
		}
		cells = append(cells, []string{"  " + Clean(t.Name), note, Clean(t.Description)})
	}
	items := make([]menuRow, len(tools))
	for i, t := range columns(cells) {
		items[i] = menuRow{ID: tools[i].Name, Text: t, Match: tools[i].Name, Dim: !tools[i].Offered}
	}
	mn := newMenu("MCP server "+name, items)
	mn.hint, mn.empty = "Esc back", "no tools   Esc back"
	if stderr = strings.TrimSpace(stderr); stderr != "" {
		mn.note = "stderr: " + lastLineOf(Clean(stderr))
	}
	mn.onChoose = func(string) bool { return false }
	return mn
}

func lastLineOf(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// ---------------------------------------------------------------------------
// /model, /think
// ---------------------------------------------------------------------------

// modelMenu lists the fetched rows, the Recent ones first (R-101), the one in use marked, the
// price on the right (R-102); the filter searches id and name; Enter sets the model. The optional
// save stores the choice through /config's API when called from there. The order shown is the
// order /model N counts (modelRows).
func (m *model) modelMenu(rows []ModelRow, save func(id string)) *menu {
	rows = slices.Clone(rows)
	slices.SortStableFunc(rows, func(a, b ModelRow) int {
		if a.Recent != b.Recent {
			if a.Recent {
				return -1
			}
			return 1
		}
		return 0
	})
	m.modelRows = rows
	cells := make([][]string, len(rows))
	for i, r := range rows {
		mark := " "
		if r.ID == m.modelName {
			mark = glyphAuto
		}
		cells[i] = []string{"  " + mark + " " + Clean(r.ID), Clean(r.Name)}
	}
	items := make([]menuRow, len(rows))
	for i, t := range columns(cells) {
		items[i] = menuRow{ID: rows[i].ID, Text: t, Match: rows[i].ID + " " + rows[i].Name, Right: rows[i].Price}
	}
	mn := newMenu("Models ("+glyphAuto+" = in use)", items)
	mn.onChoose = func(id string) bool {
		m.setModel(id)
		if save != nil {
			save(id)
		}
		return true
	}
	return mn
}

// thinkMenu is /think without an argument: two rows, Enter sets the mode.
func (m *model) thinkMenu() *menu {
	var items []menuRow
	for _, s := range []string{"on", "off"} {
		mark := " "
		if onOff(m.thinking) == s {
			mark = glyphAuto
		}
		items = append(items, menuRow{ID: s, Text: "  " + mark + " thinking " + s})
	}
	mn := newMenu("Thinking mode", items)
	mn.onChoose = func(id string) bool {
		m.think(id)
		return true
	}
	return mn
}

// ---------------------------------------------------------------------------
// /config
// ---------------------------------------------------------------------------

// configMenu lists the main settings: Enter or Space toggles a bool (or opens the model menu),
// Left/Right step a number; every change is saved to the scope and applied live where the app can.
func (m *model) configMenu() *menu {
	mn := newMenu("Settings", nil)
	mn.hint = "↑/↓ move   Enter toggle/choose   ←/→ step   Esc close"
	settings := func(id string) (Setting, bool) {
		for _, s := range m.d.Config.Settings() {
			if s.Key == id {
				return s, true
			}
		}
		return Setting{}, false
	}
	rebuild := func() {
		var cells [][]string
		var keys []string
		for _, s := range m.d.Config.Settings() {
			keys = append(keys, s.Key)
			cells = append(cells, []string{"  " + s.Label, fmt.Sprint(s.Value), s.Scope})
		}
		items := make([]menuRow, len(keys))
		for i, t := range columns(cells) {
			items[i] = menuRow{ID: keys[i], Text: t, Match: keys[i] + " " + cells[i][0]}
		}
		mn.setItems(items)
	}
	set := func(s Setting, v any) {
		if err := m.d.Config.Set(mn.scope, s.Key, v); err != nil {
			mn.note = err.Error()
			return
		}
		when := " (at the next start)"
		switch s.Live {
		case "auto":
			m.d.Approver.SetAuto(v == true)
			when = ""
		case "think":
			if t, ok := m.d.Agent.(thinker); ok {
				t.SetThinking(v == true)
				m.thinking = v == true
				when = ""
			}
		case "model":
			when = ""
		}
		rebuild()
		note, _ := saved(mn.scope, s.Label+" = "+fmt.Sprint(v), nil)
		mn.note = note + when
	}
	step := func(dir int) func(string) {
		return func(id string) {
			s, ok := settings(id)
			if !ok || s.Kind != "int" {
				return
			}
			v, _ := strconv.Atoi(fmt.Sprint(s.Value))
			st := max(s.Step, 1)
			v = v + dir*st
			if s.Max > s.Min {
				v = max(min(v, s.Max), s.Min)
			}
			set(s, v)
		}
	}
	choose := func(id string) bool {
		s, ok := settings(id)
		if !ok {
			return false
		}
		switch {
		case s.Kind == "bool":
			set(s, s.Value != true)
		case s.Live == "model":
			m.openMenu(m.modelMenu(m.modelRows, func(v string) { set(s, v) }), mn)
			if len(m.modelRows) == 0 {
				m.pick.note = "no model list fetched yet; /model fetches it"
			}
		}
		return false
	}
	mn.keys["left"], mn.keys["right"] = step(-1), step(1)
	mn.keys["space"] = func(id string) { choose(id) }
	mn.onChoose = choose
	mn.onScope = func(string) {}
	rebuild()
	return mn
}
