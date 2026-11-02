package ui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"swarg/internal/ui/menu"

	"charm.land/lipgloss/v2"
)

// ---------------------------------------------------------------------------
// The menus of the app on the one engine (R-99): /skills, /mcp, /model, /think, /config
// ---------------------------------------------------------------------------

const (
	markOn  = "◉"
	markOff = "○"
)

// menuOpts starts the options of a menu with the host's text cleaner and colour fitting.
func menuOpts(title string, rows []menu.Row) menu.Options {
	return menu.Options{Title: title, Rows: rows, Keys: map[string]func(string){}, Clean: Clean, Fit: fitProfile}
}

// openMenu shows a menu in the list area, registering its spots in the host's hover.
func (m *model) openMenu(mn *menu.Menu, parent *menu.Menu) {
	mn.Attach(&m.hv)
	mn.SetParent(parent)
	if m.d.Scopes != nil && mn.ScopeAware() {
		mn.SetScopes(m.d.Scopes.Scopes(), m.d.Scopes.Default())
	}
	m.pick = mn
}

// scopeOr is the save scope of a command: the given one, else def, else the scopes' default
// (nil Scopes = "").
func (m *model) scopeOr(given, def string) string {
	switch {
	case given != "" || m.d.Scopes == nil:
		return given
	case def != "":
		return def
	}
	return m.d.Scopes.Default()
}

// sayToggle says the outcome of a /skills, /mcp or /agents switch: the saved note, or the error.
func (m *model) sayToggle(scope, what string, err error) {
	note, err := saved(scope, what, err)
	if err != nil {
		m.say(err.Error())
		return
	}
	m.say(note + " (from the next request)")
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

// columnRows lays cells out with columns() and builds the menu rows; row gives the row i without its Text.
func columnRows(cells [][]string, row func(i int) menu.Row) []menu.Row {
	items := make([]menu.Row, len(cells))
	for i, t := range columns(cells) {
		items[i] = row(i)
		items[i].Text = t
	}
	return items
}

// pickMark is on when yes, else off (the in-use and enabled marks of the rows).
func pickMark(yes bool, on, off string) string {
	if yes {
		return on
	}
	return off
}

// toggleNote shows the outcome of a switch in the footer of mn after a rebuild: the saved note, or the error.
func toggleNote(mn *menu.Menu, rebuild func(), what string, err error) {
	note, err := saved(mn.Scope(), what, err)
	rebuild()
	if err != nil {
		note = err.Error()
	}
	mn.SetNote(note)
}

// ---------------------------------------------------------------------------
// /skills
// ---------------------------------------------------------------------------

// skillsMenu lists the Skills: Enter applies one (like /skill name), Space or the label switches
// it (saved to the scope), Tab cycles the scope.
func (m *model) skillsMenu() *menu.Menu {
	var mn *menu.Menu
	o := menuOpts("Skills", nil)
	o.Hint = "↑/↓ move   Enter apply   Space on/off   Esc close   type to filter"
	o.Empty = "no skills   Esc close"
	rebuild := func() {
		var cells [][]string
		var rows []SkillRow
		for _, s := range m.d.Skills.List() {
			rows = append(rows, s)
			cells = append(cells, []string{"  " + pickMark(s.Enabled, markOn, markOff) + " " + Clean(s.Name), s.Source, Clean(s.Description)})
		}
		mn.SetRows(columnRows(cells, func(i int) menu.Row {
			return menu.Row{ID: rows[i].Name, Match: rows[i].Name + " " + rows[i].Description, Right: onOff(!rows[i].Enabled)}
		}))
	}
	toggle := func(id string) {
		for _, s := range m.d.Skills.List() {
			if s.Name == id {
				toggleNote(mn, rebuild, "skill "+id+": "+onOff(!s.Enabled), m.d.Skills.SetEnabled(id, !s.Enabled, mn.Scope()))
				return
			}
		}
	}
	o.Keys["space"], o.OnRight = toggle, toggle
	o.OnScope = func(string) {}
	o.OnChoose = func(id string) bool {
		m.submitSkill(id, "")
		return true
	}
	mn = menu.New(o)
	rebuild()
	return mn
}

// ---------------------------------------------------------------------------
// /mcp
// ---------------------------------------------------------------------------

// mcpMenu lists the servers: Enter opens one (its tools), Space / the label switches it (saved to
// the scope), r restarts, a approves a project server.
func (m *model) mcpMenu() *menu.Menu {
	var mn *menu.Menu
	o := menuOpts("MCP servers", nil)
	o.Hint = "↑/↓ move   Enter tools   Space on/off   r restart   a approve   Esc close"
	o.Empty = "no MCP servers configured   Esc close"
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
		mn.SetRows(columnRows(cells, func(i int) menu.Row {
			right := onOff(!rows[i].Enabled)
			if rows[i].State == "needs approval" {
				right = "approve"
			}
			return menu.Row{ID: rows[i].Name, Match: rows[i].Name, Right: right}
		}))
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
		mn.SetNote(note)
	}
	toggle := func(id string) {
		act(id, func(s MCPServerRow) (string, error) {
			return saved(mn.Scope(), "mcp "+id+": "+onOff(!s.Enabled), m.d.MCP.SetEnabled(id, !s.Enabled, mn.Scope()))
		})
	}
	approve := func(id string) {
		act(id, func(MCPServerRow) (string, error) { return "mcp " + id + ": approved and started", m.d.MCP.Approve(id) })
	}
	o.Keys["space"] = toggle
	o.Keys["r"] = func(id string) {
		act(id, func(MCPServerRow) (string, error) { return "mcp " + id + ": running", m.d.MCP.Restart(id) })
	}
	o.Keys["a"] = approve
	o.OnRight = func(id string) {
		if s, ok := find(id); ok && s.State == "needs approval" {
			approve(id)
			return
		}
		toggle(id)
	}
	o.OnScope = func(string) {}
	o.OnChoose = func(id string) bool {
		if sub := m.mcpServerMenu(id); sub != nil {
			m.openMenu(sub, mn)
		}
		return false
	}
	mn = menu.New(o)
	rebuild()
	return mn
}

// mcpServerMenu shows the tools of one server (offered or not, read-only marked) with the stderr
// tail as the footer note; nil with a notice when the server is unknown.
func (m *model) mcpServerMenu(name string) *menu.Menu {
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
	items := columnRows(cells, func(i int) menu.Row {
		return menu.Row{ID: tools[i].Name, Match: tools[i].Name, Dim: !tools[i].Offered}
	})
	o := menuOpts("MCP server "+name, items)
	o.Hint, o.Empty = "Esc back", "no tools   Esc back"
	o.OnChoose = func(string) bool { return false }
	mn := menu.New(o)
	if stderr = strings.TrimSpace(stderr); stderr != "" {
		mn.SetNote("stderr: " + lastLineOf(Clean(stderr)))
	}
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
func (m *model) modelMenu(rows []ModelRow, save func(id string)) *menu.Menu {
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
		cells[i] = []string{"  " + pickMark(r.ID == m.modelName, glyphAuto, " ") + " " + Clean(r.ID), Clean(r.Name)}
	}
	items := columnRows(cells, func(i int) menu.Row {
		return menu.Row{ID: rows[i].ID, Match: rows[i].ID + " " + rows[i].Name, Right: rows[i].Price}
	})
	o := menuOpts("Models ("+glyphAuto+" = in use)", items)
	o.OnChoose = func(id string) bool {
		m.setModel(id)
		if save != nil {
			save(id)
		}
		return true
	}
	return menu.New(o)
}

// thinkMenu is /think without an argument: two rows, Enter sets the mode.
func (m *model) thinkMenu() *menu.Menu {
	var items []menu.Row
	for _, s := range []string{"on", "off"} {
		items = append(items, menu.Row{ID: s, Text: "  " + pickMark(onOff(m.thinking) == s, glyphAuto, " ") + " thinking " + s})
	}
	o := menuOpts("Thinking mode", items)
	o.OnChoose = func(id string) bool {
		m.think(id)
		return true
	}
	return menu.New(o)
}

// ---------------------------------------------------------------------------
// /config
// ---------------------------------------------------------------------------

// configMenu lists the main settings: Enter or Space toggles a bool (or opens the model menu),
// Left/Right step a number; every change is saved to the scope and applied live where the app can.
func (m *model) configMenu() *menu.Menu {
	var mn *menu.Menu
	o := menuOpts("Settings", nil)
	o.Hint = "↑/↓ move   Enter toggle/choose   ←/→ step   Esc close"
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
		if m.d.Keys != nil { // the API keys live in their own menu
			keys = append(keys, keysRowID)
			cells = append(cells, []string{"  API keys", "open (/keys)", "global"})
		}
		mn.SetRows(columnRows(cells, func(i int) menu.Row {
			return menu.Row{ID: keys[i], Match: keys[i] + " " + cells[i][0]}
		}))
	}
	set := func(s Setting, v any) {
		if err := m.d.Config.Set(mn.Scope(), s.Key, v); err != nil {
			mn.SetNote(err.Error())
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
		case "profile": // the row of the prompt profile (R-117)
			if m.d.Prompt != nil {
				if err := m.d.Prompt.Choose(fmt.Sprint(v), ""); err != nil {
					mn.SetNote(err.Error())
					return
				}
				when = ""
			}
		}
		rebuild()
		note, _ := saved(mn.Scope(), s.Label+" = "+fmt.Sprint(v), nil)
		mn.SetNote(note + when)
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
		if id == keysRowID {
			m.openMenu(m.keysMenu(), mn)
			return false
		}
		s, ok := settings(id)
		if !ok {
			return false
		}
		switch {
		case s.Kind == "bool":
			set(s, s.Value != true)
		case s.Live == "profile" && m.d.Prompt != nil: // Enter steps to the next profile
			set(s, nextProfile(m.d.Prompt.Profiles(), fmt.Sprint(s.Value)))
		case s.Live == "model":
			m.openMenu(m.modelMenu(m.modelRows, func(v string) { set(s, v) }), mn)
			if len(m.modelRows) == 0 {
				m.pick.SetNote("no model list fetched yet; /model fetches it")
			}
		}
		return false
	}
	o.Keys["left"], o.Keys["right"] = step(-1), step(1)
	o.Keys["space"] = func(id string) { choose(id) }
	o.OnChoose = choose
	o.OnScope = func(string) {}
	mn = menu.New(o)
	rebuild()
	return mn
}

// nextProfile is the profile after cur in the list (the first one after the last).
func nextProfile(rows []PromptRow, cur string) string {
	if len(rows) == 0 {
		return cur
	}
	for i, r := range rows {
		if r.Name == cur {
			return rows[(i+1)%len(rows)].Name
		}
	}
	return rows[0].Name
}
