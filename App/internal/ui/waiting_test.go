package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Waiting Messages (R-27, I-8): text sent while the agent works is shown at once
// ---------------------------------------------------------------------------

func userText(s string) agent.Event { return agent.Event{Kind: agent.EvUserText, Text: s} }

// enter types text into the input and presses Enter.
func (r *rig) enter(text string) {
	r.m.ta.SetValue(text)
	r.send(key(tea.KeyEnter, 0))
}

// screenCount counts the screen rows containing part.
func (r *rig) screenCount(part string) int {
	n := 0
	for _, row := range r.rows() {
		if strings.Contains(row, part) {
			n++
		}
	}
	return n
}

// rowIndex is the first screen row containing part, -1 if none.
func (r *rig) rowIndex(part string) int {
	for i, row := range r.rows() {
		if strings.Contains(row, part) {
			return i
		}
	}
	return -1
}

// The owner's bug: during a long answer the sent text did not appear until the Turn ended.
func TestWaiting_ShownAtOnce(t *testing.T) {
	r := newRig(t)
	r.events(started("first"), delta("a long answer that keeps streaming"))
	r.enter("second message")
	if r.screenCount("second message") != 1 {
		t.Fatalf("the sent text is not on screen before the agent takes it:\n%s", r.view())
	}
	if !strings.Contains(r.rows()[r.rowIndex("second message")], waitingMark) {
		t.Errorf("the text is not marked as waiting:\n%s", r.view())
	}
	if got := r.ag.submitted; len(got) != 1 || got[0] != "second message" {
		t.Errorf("submitted %q", got)
	}
}

func TestWaiting_TakenOnInject(t *testing.T) {
	r := newRig(t)
	r.events(started("first"), delta("answer"))
	r.enter("second")
	r.events(agent.Event{Kind: agent.EvToolStart, Call: chat.ToolCall{ID: "c1", Name: "read_file"}, Label: "a.go"})
	r.events(agent.Event{Kind: agent.EvToolEnd, Call: chat.ToolCall{ID: "c1", Name: "read_file"}, Label: "a.go", Brief: "3 lines"})
	r.events(userText("second"))
	if n := r.screenCount("second"); n != 1 {
		t.Fatalf("the text is shown %d times, want once:\n%s", n, r.view())
	}
	if strings.Contains(r.view(), waitingMark) {
		t.Errorf("the mark stayed after the agent took the text:\n%s", r.view())
	}
	if !strings.Contains(r.convText(), "second") {
		t.Errorf("the taken text is not a conversation item:\n%s", r.convText())
	}
	if r.rowIndex("read_file") > r.rowIndex("second") {
		t.Errorf("the taken text is above the tool block of the Step before it:\n%s", r.view())
	}
}

func TestWaiting_TakenOnNextTurn(t *testing.T) {
	r := newRig(t)
	r.events(started("first"), delta("answer"))
	r.enter("next")
	r.events(ended(chat.FinishStop, nil), started("next"))
	if n := r.screenCount("next"); n != 1 || strings.Contains(r.view(), waitingMark) {
		t.Fatalf("want one taken item, got %d (mark: %v):\n%s", n, strings.Contains(r.view(), waitingMark), r.view())
	}
}

func TestWaiting_SubmitOrder(t *testing.T) {
	r := newRig(t)
	r.events(started("first"), delta("answer"))
	r.enter("one")
	r.enter("two")
	r.enter("three")
	if !(r.rowIndex("one") < r.rowIndex("two") && r.rowIndex("two") < r.rowIndex("three")) {
		t.Fatalf("waiting texts out of order:\n%s", r.view())
	}
	r.events(userText("one"), userText("two"))
	if r.screenCount("one") != 1 || r.screenCount("two") != 1 || r.screenCount("three") != 1 {
		t.Fatalf("a text is lost or doubled:\n%s", r.view())
	}
	if !strings.Contains(r.rows()[r.rowIndex("three")], waitingMark) {
		t.Errorf("three is still waiting but not marked:\n%s", r.view())
	}
	if strings.Contains(r.rows()[r.rowIndex("one")], waitingMark) {
		t.Errorf("one was taken but is still marked:\n%s", r.view())
	}
}

func TestWaiting_UnknownTextStillShown(t *testing.T) {
	r := newRig(t)
	r.events(started("first"))
	r.events(userText("from elsewhere"))
	if r.screenCount("from elsewhere") != 1 {
		t.Fatalf("an injected text that was never waiting is not shown:\n%s", r.view())
	}
}

func TestWaiting_IdleSubmitNoDouble(t *testing.T) {
	r := newRig(t)
	r.enter("hello")
	r.events(started("hello"))
	if n := r.screenCount("hello"); n != 1 {
		t.Fatalf("hello shown %d times:\n%s", n, r.view())
	}
}

func TestWaiting_CommandsAreNotWaiting(t *testing.T) {
	r := newRig(t)
	r.events(started("first"))
	r.enter("/auto")
	if strings.Contains(r.view(), waitingMark) {
		t.Errorf("a command is shown as a waiting message:\n%s", r.view())
	}
}

func TestWaiting_DuringAsk(t *testing.T) {
	r := newRig(t)
	r.events(started("first"))
	out := r.ask(t.Context(), "shell", `{"command":"ls"}`)
	r.enter("no, do it differently")
	if ap := recv(t, out); ap.Allow || ap.Note != "no, do it differently" {
		t.Errorf("approval %+v", ap)
	}
	if r.screenCount("no, do it differently") != 1 {
		t.Errorf("the note text is not shown as waiting:\n%s", r.view())
	}
}

// R-11: queued text still runs after an abort, so its Waiting Message stays until the agent takes it.
func TestWaiting_KeptAcrossAbort(t *testing.T) {
	r := newRig(t)
	r.events(started("first"), delta("answer"))
	r.enter("later")
	r.send(key(tea.KeyEscape, 0))
	r.send(key(tea.KeyEscape, 0))
	r.events(ended(chat.FinishInterrupted, nil))
	if !strings.Contains(r.rows()[r.rowIndex("later")], waitingMark) {
		t.Fatalf("the waiting text lost its mark after the abort:\n%s", r.view())
	}
	r.events(started("later"))
	if r.screenCount("later") != 1 || strings.Contains(r.view(), waitingMark) {
		t.Errorf("want one taken item:\n%s", r.view())
	}
}
