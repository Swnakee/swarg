package ui

import (
	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Model source choice in the app (/server, the panel's provider item; R-100)
// ---------------------------------------------------------------------------

// serversMsg carries the configured Sources with their states (or the error) back into Update.
type serversMsg struct {
	rows []ServerRow
	err  error
}

// chooseServer is /server: with a name it switches at once; without one it probes the Sources
// asynchronously and shows the menu.
func (m *model) chooseServer(arg string) tea.Cmd {
	if m.d.Servers == nil {
		m.say("the model sources are not available here")
		return nil
	}
	if arg != "" {
		m.switchServer(arg)
		return nil
	}
	srv, ctx := m.d.Servers, m.ctx
	return func() tea.Msg {
		rows, err := srv.Servers(ctx)
		return serversMsg{rows: rows, err: err}
	}
}

// onServers opens the source menu over the probed rows.
func (m *model) onServers(msg serversMsg) {
	switch {
	case msg.err != nil:
		m.say("sources: " + msg.err.Error())
		return
	case len(msg.rows) == 0:
		m.say("no model sources are configured")
		return
	}
	m.openMenu(m.serverMenu(msg.rows), nil)
}

// serverMenu lists the Sources: `name  type  address  state`, the active one marked; Enter switches.
func (m *model) serverMenu(rows []ServerRow) *menu {
	cells := make([][]string, len(rows))
	for i, r := range rows {
		mark := " "
		if r.Active {
			mark = glyphAuto
		}
		cells[i] = []string{"  " + mark + " " + Clean(r.Name), r.Type, Clean(r.Address), Clean(r.State)}
	}
	items := make([]menuRow, len(rows))
	for i, t := range columns(cells) {
		items[i] = menuRow{ID: rows[i].Name, Text: t, Match: rows[i].Name}
	}
	mn := newMenu("Model sources ("+glyphAuto+" = in use)", items)
	mn.onChoose = func(id string) bool {
		m.switchServer(id)
		return true
	}
	return mn
}

// switchServer makes name the Source from the next request on and reports it (with the one-time
// cloud notice when the adapter returns one, R-104).
func (m *model) switchServer(name string) {
	notice, err := m.d.Servers.Switch(name)
	if err != nil {
		m.say("server: " + err.Error())
		return
	}
	m.say("server: " + name + " (used from the next request)")
	if notice != "" {
		m.say(notice)
	}
}
