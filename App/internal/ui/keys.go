package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/agent"
)

// ---------------------------------------------------------------------------
// Key handling (runs before the textarea sees anything)
// ---------------------------------------------------------------------------

const (
	askDelay  = 300 * time.Millisecond                                                      // y/n/a are dropped this long after a prompt appears (typed ahead)
	askHint   = "clear the input (Ctrl+U) to answer y/n/a; Enter sends the text and denies" // shown under a prompt while the input is not empty
	forceHint = "finishing the current answer… (ctrl+c again to force)"
)

// handleKey handles the keys the model owns. handled=false means "give it to the textarea".
func (m *model) handleKey(msg tea.KeyPressMsg) (cmd tea.Cmd, handled bool) {
	if m.pick != nil && !(m.quitting && msg.String() == "ctrl+c") { // the open picker takes every key (Esc and Ctrl+C close it); a second Ctrl+C of a pending quit still quits
		m.pickAct(m.pick.key(msg))
		return nil, true
	}
	if m.ask != nil {
		if cmd, ok := m.askKey(msg); ok {
			return cmd, true
		}
	}
	if m.suggesting() { // Tab completes the selected suggestion, Up/Down move the selection
		switch msg.String() {
		case "tab":
			_, sel := m.current()
			m.completeSuggestion(sel)
			return nil, true
		case "up":
			m.moveSuggestion(-1)
			return nil, true
		case "down":
			m.moveSuggestion(1)
			return nil, true
		}
	}
	switch msg.String() {
	case "enter":
		text := strings.TrimSpace(m.ta.Value())
		if text == "" {
			return nil, true
		}
		if m.keyWarning(text) {
			return nil, true
		}
		m.ta.Reset()
		return m.submit(text), true
	case "shift+enter", "ctrl+j":
		m.ta.InsertString("\n")
		return nil, true
	case "ctrl+o":
		m.toggleLast()
		return nil, true
	case "pgup", "pgdown", "home", "end", "ctrl+home", "ctrl+end":
		return nil, m.scrollKey(msg.String())
	case "ctrl+b":
		return nil, m.ctrlB()
	case "esc":
		if m.sel.active { // Esc first drops a selection
			m.clearSel()
			return nil, true
		}
		m.escape()
		return nil, true
	case "ctrl+c":
		if m.quitting {
			return tea.Quit, true
		}
		if m.ta.Value() != "" {
			m.ta.Reset()
			return nil, true
		}
		return m.requestQuit(), true
	}
	return nil, false
}

// askKey handles the keys of a pending confirmation. y, n and a act only on an empty
// input and after askDelay, so text typed ahead can never answer by accident; within the
// delay they are dropped rather than typed (a y in the input would leave the prompt waiting
// for ever while every further y lands in the input: the user looks stuck). Other keys start a
// note. Enter with text denies, sends the text to the model first, and passes the text as the note.
func (m *model) askKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	early := m.clock().Sub(m.askSince) < askDelay
	switch {
	case early && m.ta.Value() == "" && (key == "y" || key == "n" || key == "a"):
		return nil, true
	case key == "enter":
		text := strings.TrimSpace(m.ta.Value())
		if text == "" {
			return nil, true
		}
		m.ta.Reset()
		cmd := m.submit(text)
		m.denyAsk(text)
		return cmd, true
	case m.ta.Value() == "" && !early:
		switch key {
		case "y":
			m.answer(agent.Approval{Allow: true})
		case "a":
			m.d.Approver.SetAuto(true)
			m.answer(agent.Approval{Allow: true})
		case "n":
			m.answer(agent.Approval{})
		default:
			return nil, false
		}
		return nil, true
	}
	return nil, false
}

// answer replies to the pending confirmation.
func (m *model) answer(ap agent.Approval) {
	m.ask.reply <- ap
	m.ask = nil
}

// denyAsk denies the pending confirmation, if any.
func (m *model) denyAsk(note string) {
	if m.ask != nil {
		m.answer(agent.Approval{Note: note})
	}
}

// escape is Esc: the first press of a Turn interrupts, every later one aborts. It also
// denies a pending confirmation. Idle, it does nothing.
func (m *model) escape() {
	if !m.working() {
		return
	}
	m.denyAsk("")
	if m.escCount++; m.escCount == 1 {
		m.stopping = true
		m.d.Agent.Interrupt()
		return
	}
	m.d.Agent.Abort()
}

// requestQuit quits at once when idle; when busy it interrupts and quits after the
// drain (TurnEnded), so the model server is never left with a cut connection.
func (m *model) requestQuit() tea.Cmd {
	if !m.working() && !m.d.Agent.Busy() {
		return tea.Quit
	}
	m.quitting = true
	m.d.Agent.Interrupt()
	return nil
}
