package agent

import "swarg/internal/chat"

// plain clears the history-only bookkeeping fields (Seq, TurnStart, Injected, Brief, Label,
// Failed) so a test can compare the conversation itself.
func plain(h []chat.Message) []chat.Message {
	out := append([]chat.Message(nil), h...)
	for i := range out {
		out[i].Seq, out[i].TurnStart, out[i].Injected = 0, false, false
		out[i].Brief, out[i].Label, out[i].Failed = "", "", false
	}
	return out
}
