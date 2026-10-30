package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Model choice in the app (/model, the panel's model item; R-30)
// ---------------------------------------------------------------------------

// modelsMsg carries the Source's model list (or the error) back into Update.
type modelsMsg struct {
	rows []ModelRow
	err  error
}

// chooseModel is /model: with an id (or a number of the last list) it sets the model at once;
// without one it fetches the list asynchronously and shows it.
func (m *model) chooseModel(arg string) tea.Cmd {
	if arg != "" {
		if n, err := strconv.Atoi(arg); err == nil { // a number picks from the last list, never an id
			if n < 1 || n > len(m.modelRows) {
				m.say(fmt.Sprintf("no model %d in the list; /model shows it", n))
				return nil
			}
			arg = m.modelRows[n-1].ID
		}
		m.setModel(arg)
		return nil
	}
	if m.d.Models == nil {
		m.say("the model list is not available; use /model <id>")
		return nil
	}
	list, ctx := m.d.Models, m.ctx
	return func() tea.Msg {
		rows, err := list.List(ctx)
		return modelsMsg{rows: rows, err: err}
	}
}

// onModels opens the model menu over the fetched list (the id in use marked).
func (m *model) onModels(msg modelsMsg) {
	switch {
	case msg.err != nil:
		m.say("model list: " + msg.err.Error())
		return
	case len(msg.rows) == 0:
		m.say("the server lists no models; use /model <id>")
		return
	}
	m.openMenu(m.modelMenu(msg.rows, nil), nil)
}

// setModel asks the agent to use the model from the next request on.
func (m *model) setModel(name string) {
	ms, ok := m.d.Agent.(modelSetter)
	if !ok {
		m.say("this agent cannot change the model")
		return
	}
	if err := ms.SetModel(name); err != nil {
		m.say("model: " + err.Error())
		return
	}
	m.modelName = strings.TrimSpace(name)
	m.say("model: " + m.modelName + " (used from the next request)")
}

// thinkText is the Bottom panel item shown while the thinking mode is on.
const thinkText = "think"

// think is /think [on|off]: no argument shows the state; on or off asks the agent to switch the
// thinking mode from the next request on (R-60).
func (m *model) think(arg string) {
	if arg == "" {
		m.openMenu(m.thinkMenu(), nil)
		return
	}
	t, ok := m.d.Agent.(thinker)
	if !ok {
		m.say("this agent cannot change the thinking mode")
		return
	}
	t.SetThinking(arg == "on")
	m.thinking = arg == "on"
	m.say("thinking: " + onOff(m.thinking) + " (used from the next request)")
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
