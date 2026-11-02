package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"swarg/internal/agent"
	"swarg/internal/chat"
	"swarg/internal/ui/menu"
)

// ---------------------------------------------------------------------------
// /compact /sessions /resume /rename /context
// ---------------------------------------------------------------------------

const (
	noSessionsText = "sessions are not saved"
	waitTurnText   = "wait for the turn to end"
)

// compact is /compact: the agent runs it between Turns; while busy it is queued.
func (m *model) compact() {
	c, ok := m.d.Agent.(compacter)
	if !ok {
		m.say("compaction is not available")
		return
	}
	queued := m.working() || m.d.Agent.Busy()
	if err := c.Compact(m.ctx); err != nil {
		m.say("compact: " + err.Error())
		return
	}
	if queued {
		m.say("compaction queued")
	}
}

// sessionRows returns the saved sessions, newest first (the order of the picker).
func (m *model) sessionRows() ([]SessionRow, error) {
	rows, err := m.d.Sessions.List()
	if err != nil {
		return nil, err
	}
	rows = append([]SessionRow(nil), rows...)
	slices.SortStableFunc(rows, func(a, b SessionRow) int { return b.Time.Compare(a.Time) })
	return rows, nil
}

// openPicker is /sessions (and /resume): the saved Sessions in a picker that takes the list
// area. Keys and the mouse go to it while it is open; Esc closes it.
func (m *model) openPicker() {
	if m.d.Sessions == nil {
		m.say(noSessionsText)
		return
	}
	rows, err := m.sessionRows()
	switch {
	case err != nil:
		m.say("sessions: " + err.Error())
	case len(rows) == 0:
		m.say("no saved sessions")
	default:
		m.openMenu(newPicker(rows, m.d.Sessions.Current(), m.d.Sessions.Delete, func(o *menu.Options) {
			o.Title, o.OnChoose = "Saved sessions", m.openSession
		}), nil)
	}
}

// pickAct applies what the open menu decided after a key or mouse message: a close (back to the
// parent menu, if any), or the chosen row handed to the menu's onChoose (a true closes the menu;
// the Session picker stays open when opening fails, with the reason in its footer).
func (m *model) pickAct(a menu.Action) {
	switch a {
	case menu.ActClose:
		m.pick = m.pick.Parent()
	case menu.ActChosen:
		if m.pick.Choose(m.pick.TakeChosen()) {
			m.pick = m.pick.Parent()
		}
	}
}

// openSession switches to the saved Session id: idle only; the SessionFile being written
// becomes that Session's (SessionsAPI.Open), the agent gets its history and the list is
// rebuilt. It reports success; a failure is noted where the user looks (the picker's footer
// while it is open, else the list).
func (m *model) openSession(id string) bool {
	if m.working() || m.d.Agent.Busy() {
		m.note(waitTurnText)
		return false
	}
	ld, ok := m.d.Agent.(loader)
	if !ok {
		m.note("resume is not available")
		return false
	}
	o, err := m.d.Sessions.Open(id)
	if err != nil {
		m.note("resume: " + err.Error())
		return false
	}
	if err := ld.Load(o.History, agent.WithMaxSeq(o.MaxSeq), agent.WithModel(o.Model), agent.WithThinking(o.Think), agent.WithSpend(o.Spend), agent.WithTokens(o.Tokens)); err != nil {
		m.note("resume: " + err.Error())
		return false
	}
	m.rebuild(o.History)
	m.say("resumed session " + firstLine(Clean(o.Title)))
	return true
}

// rename is /rename <name>: the Session being written gets a Name (R-32).
func (m *model) rename(name string) {
	if m.d.Sessions == nil {
		m.say(noSessionsText)
		return
	}
	if err := m.d.Sessions.Rename(name); err != nil {
		m.say("rename: " + err.Error())
		return
	}
	m.say("session renamed to " + firstLine(Clean(name)))
}

// note shows a short message where the user looks: the picker's footer while it is open,
// else a notice in the list.
func (m *model) note(text string) {
	if m.pick != nil {
		m.pick.SetNote(text)
		return
	}
	m.say(text)
}

// showContext is /context: the last request estimate by category and the cache share.
func (m *model) showContext() {
	if !m.hasBd {
		m.say("no context data yet")
		return
	}
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	total := max(m.bd.Total(), 1)
	for _, c := range []struct {
		name string
		n    int
	}{
		{"system + tools", m.bd.System}, {"user", m.bd.User}, {"answers", m.bd.Answers},
		{"call args", m.bd.CallArgs}, {"tool results", m.bd.ResultsFull}, {"stubs", m.bd.Stubs},
	} {
		fmt.Fprintf(w, "%s\t~%d tokens\t%d%%\n", c.name, c.n, c.n*100/total)
	}
	fmt.Fprintf(w, "total\t%s\t\n", agent.FormatMeter(m.used, m.limit, m.est))
	if m.hasCache {
		fmt.Fprintf(w, "cache\t%d%%\t\n", m.cachePct)
	}
	w.Flush()
	m.say(strings.TrimRight(b.String(), "\n"))
}

// ---------------------------------------------------------------------------
// Rebuilding the list from a history
// ---------------------------------------------------------------------------

// rebuild resets the list, the call records and the scroll, then fills the list from history:
// user items, assistant items (a Summary as a dim "work summary" notice), tool items from the
// assistant ToolCalls and their results. A call without a result shows as cancelled.
func (m *model) rebuild(history []chat.Message) {
	m.resetList()
	results := map[string]chat.Message{}
	for _, msg := range history {
		if msg.Role == chat.RoleTool {
			results[msg.ToolCallID] = msg
		}
	}
	for _, msg := range history {
		switch msg.Role {
		case chat.RoleUser:
			m.cv.addUser(msg.Content)
		case chat.RoleAssistant:
			if msg.Summary {
				m.cv.addNotice("work summary\n" + Clean(msg.Content))
				continue
			}
			if strings.TrimSpace(msg.Content) != "" {
				i := m.cv.addAssistant()
				m.cv.feed(i, Clean(msg.Content))
				m.cv.endAssistant(i, msg.Interrupted)
			}
			if len(msg.ToolCalls) > 0 {
				m.step++
			}
			for _, tc := range msg.ToolCalls {
				m.rebuildCall(tc, results)
			}
		}
	}
}

// rebuildCall adds the block of one past call, finished.
func (m *model) rebuildCall(tc chat.ToolCall, results map[string]chat.Message) {
	m.nextN++
	r := callRec{n: m.nextN, step: m.step, call: tc, label: tc.Name, done: true}
	if res, ok := results[tc.ID]; ok {
		if res.Label != "" {
			r.label = res.Label
		}
		r.brief, r.output = res.Brief, capTail(res.Content, maxLiveOutput)
		if res.Failed {
			r.err = errors.New(res.Brief)
		}
	} else {
		r.brief = cancelledOnEnd
	}
	r.item = m.cv.addTool(r)
	m.appendCall(r)
}
