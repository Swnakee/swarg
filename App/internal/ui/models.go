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

// modelsMsg carries the Provider's model list (or the error) back into Update.
type modelsMsg struct {
	ids []string
	err error
}

// chooseModel is /model: with an id (or a number of the last list) it sets the model at once;
// without one it fetches the list asynchronously and shows it.
func (m *model) chooseModel(arg string) tea.Cmd {
	if arg != "" {
		if n, err := strconv.Atoi(arg); err == nil { // a number picks from the last list, never an id
			if n < 1 || n > len(m.modelList) {
				m.say(fmt.Sprintf("no model %d in the list; /model shows it", n))
				return nil
			}
			arg = m.modelList[n-1]
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
		ids, err := list.List(ctx)
		return modelsMsg{ids: ids, err: err}
	}
}

// onModels shows the fetched list: each id numbered, the one in use marked.
func (m *model) onModels(msg modelsMsg) {
	switch {
	case msg.err != nil:
		m.say("model list: " + msg.err.Error())
		return
	case len(msg.ids) == 0:
		m.say("the server lists no models; use /model <id>")
		return
	}
	m.modelList = msg.ids
	var b strings.Builder
	b.WriteString("models (" + glyphAuto + " = in use):")
	for i, id := range msg.ids {
		mark := " "
		if id == m.modelName {
			mark = glyphAuto
		}
		fmt.Fprintf(&b, "\n  %2d %s %s", i+1, mark, id)
	}
	b.WriteString("\nchoose with /model N or /model <id>")
	m.say(b.String())
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
		m.say("thinking: " + onOff(m.thinking))
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
