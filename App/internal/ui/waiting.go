package ui

import (
	"charm.land/lipgloss/v2"

	"swarg/internal/ui/mdstream"
)

// ---------------------------------------------------------------------------
// Waiting Messages: texts sent to the agent that it has not taken yet (R-27, I-8)
// ---------------------------------------------------------------------------

// waitingMark follows a Waiting Message on screen.
const waitingMark = "(waiting)"

// waitingStyle is dim text without a background: the message is not part of the history yet.
var waitingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.Dim))

// addWaiting notes a text sent to the agent; it is shown until the agent takes it.
func (m *model) addWaiting(text string) { m.waiting = append(m.waiting, text) }

// takeWaiting drops the text the agent just took (EvTurnStarted, EvUserText). The agent takes
// texts in submit order, so it is the first one; the first equal one is dropped in any case.
// A text that was never waiting (sent by someone else) drops nothing.
func (m *model) takeWaiting(text string) {
	for i, w := range m.waiting {
		if w == text {
			m.waiting = append(m.waiting[:i:i], m.waiting[i+1:]...)
			return
		}
	}
}

// waitingRows renders the Waiting Messages, oldest first: the user glyph, the text and the mark,
// wrapped to the width.
func (m *model) waitingRows() []string {
	var rows []string
	for _, t := range m.waiting {
		for _, r := range wrapText(glyphUser+" "+Clean(t)+"  "+waitingMark, m.width) {
			rows = append(rows, waitingStyle.Render(r))
		}
	}
	return rows
}
