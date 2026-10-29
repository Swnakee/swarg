package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// The Progress line is the last row of the content (R-28)
// ---------------------------------------------------------------------------

func TestProgress_UnderLastItem(t *testing.T) {
	r := newRig(t)
	r.size(80, 30)
	r.events(started("question"), delta("short answer"))
	rows := r.rows()
	ans := r.rowIndex("short answer")
	if ans < 0 || !strings.Contains(rows[ans+1], "Thinking…") {
		t.Fatalf("the Progress line is not right under the newest item:\n%s", r.view())
	}
	if strings.TrimSpace(rows[ans+2]) != "" {
		t.Errorf("the row under the Progress line is not empty space:\n%s", r.view())
	}
}

func TestProgress_BelowWaiting(t *testing.T) {
	r := newRig(t)
	r.events(started("question"), delta("answer"))
	r.enter("later")
	w, p := r.rowIndex("later"), r.rowIndex("Thinking…")
	if w < 0 || p != w+1 {
		t.Fatalf("waiting row %d, Progress line %d; want the Progress line right under it:\n%s", w, p, r.view())
	}
}

func TestProgress_GoneWhenIdle(t *testing.T) {
	r := newRig(t)
	r.events(started("question"), delta("answer"), ended(chat.FinishStop, nil))
	if strings.Contains(r.view(), "Thinking…") {
		t.Errorf("the Progress line stayed after the Turn:\n%s", r.view())
	}
}

func TestProgress_FollowsLongList(t *testing.T) {
	r := newRig(t)
	r.size(80, 20)
	r.fillRows(100)
	r.events(started("q"), delta("newest"))
	if got := r.lastListRow(); !strings.Contains(got, "Thinking…") {
		t.Fatalf("last list row %q, want the Progress line", got)
	}
	if got := strings.TrimSpace(r.rows()[r.m.listHeight()-2]); got != "newest" {
		t.Errorf("row above the Progress line %q, want the newest item", got)
	}
}

func TestProgress_NotFollowingKeepsAnchor(t *testing.T) {
	r := newRig(t)
	r.size(80, 20)
	r.fillRows(100)
	r.events(started("q"))
	r.send(wheel(tea.MouseWheelUp))
	r.send(wheel(tea.MouseWheelUp))
	top := r.firstListRow()
	r.events(delta("more text"))
	r.enter("later")
	if got := r.firstListRow(); got != top {
		t.Errorf("the view moved while reading: top %q, was %q", got, top)
	}
	if strings.Contains(r.view(), "Thinking…") {
		t.Errorf("scrolled up, the footer must stay below the window:\n%s", r.view())
	}
	r.send(key(tea.KeyEnd, tea.ModCtrl))
	if got := r.lastListRow(); !strings.Contains(got, "Thinking…") {
		t.Errorf("at the bottom the last row is %q, want the Progress line", got)
	}
}

func TestProgress_ScrolledToBottomShowsFooter(t *testing.T) {
	r := newRig(t)
	r.size(80, 20)
	r.fillRows(100)
	r.events(started("q"), delta("newest"))
	r.send(wheel(tea.MouseWheelUp))
	for i := 0; i < 10; i++ {
		r.send(wheel(tea.MouseWheelDown))
	}
	if !r.m.sc.follow {
		t.Fatal("wheel down to the end of the content must follow again")
	}
	if got := r.lastListRow(); !strings.Contains(got, "Thinking…") {
		t.Errorf("last row %q", got)
	}
}

func TestProgress_ClickOnFooterDoesNothing(t *testing.T) {
	r := newRig(t)
	r.events(started("question"))
	r.enter("later")
	for _, part := range []string{"later", "Thinking…"} {
		y := r.rowIndex(part)
		res := r.click(3, y)
		if res.clipped || r.m.sel.active {
			t.Errorf("click on the %q footer row selected or copied", part)
		}
		if _, _, ok := r.m.listPoint(3, y); ok {
			t.Errorf("the %q footer row maps to a list cell", part)
		}
	}
}

func TestProgress_AskReplacesProgressLine(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	r.ask(t.Context(), "shell", `{"command":"ls"}`)
	if strings.Contains(r.view(), "Thinking…") {
		t.Errorf("the Progress line is shown beside the ask prompt:\n%s", r.view())
	}
}

func TestProgress_FrameInvariants(t *testing.T) {
	for _, sz := range [][2]int{{20, 5}, {20, 8}, {40, 6}, {80, 24}, {200, 60}} {
		r := newRig(t)
		r.size(sz[0], sz[1])
		r.events(started("q"), delta(strings.Repeat("word ", 50)))
		r.enter(strings.Repeat("waiting text ", 20))
		r.enter("two")
		wantFrame(t, r, sz[0], sz[1])
		r.fillRows(40)
		wantFrame(t, r, sz[0], sz[1])
	}
}

// The ticking Progress line and Waiting Messages are not list rows: they never count as new rows.
func TestProgress_NoNewRowsMarkerFromFooter(t *testing.T) {
	r := newRig(t)
	r.size(80, 20)
	r.fillRows(100)
	r.events(started("q"))
	r.send(wheel(tea.MouseWheelUp))
	r.enter("later")
	r.send(tickMsg(r.now.Add(time.Second)))
	if strings.Contains(r.view(), "new rows") {
		t.Errorf("footer rows counted as new:\n%s", r.view())
	}
}

// ---------------------------------------------------------------------------
// The counter and the live speed count every streamed chunk (R-61)
// ---------------------------------------------------------------------------

func activity(s string) agent.Event { return agent.Event{Kind: agent.EvActivity, Text: s} }

// tokensShown parses "~N tokens" of the Progress line.
func (r *rig) tokensShown() int {
	r.t.Helper()
	i := r.rowIndex("tokens")
	if i < 0 {
		r.t.Fatalf("no token counter:\n%s", r.view())
	}
	var n int
	for _, f := range strings.Fields(r.rows()[i]) {
		if _, err := fmt.Sscanf(f, "~%d", &n); err == nil {
			return n
		}
	}
	r.t.Fatalf("no ~N on the Progress line: %q", r.rows()[i])
	return 0
}

func TestProgress_CountsReasoningAndToolArgs(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), activity(strings.Repeat("thinking hard ", 30)))
	if n := r.tokensShown(); n < 20 {
		t.Errorf("counter %d after 420 chars of reasoning, want it to grow", n)
	}
	r.now = r.now.Add(2 * time.Second)
	r.events(activity(`{"path":"a.txt","content":"`), activity(strings.Repeat("x", 200)))
	if !strings.Contains(r.view(), "tok/s") {
		t.Errorf("no live speed after streamed tool arguments:\n%s", r.view())
	}
	if strings.Contains(r.convText(), "thinking hard") {
		t.Errorf("activity text leaked into the conversation:\n%s", r.convText())
	}
}

func TestProgress_CounterResetsPerTurn(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), activity(strings.Repeat("word ", 50)), ended(chat.FinishStop, nil))
	r.events(started("next"))
	if n := r.tokensShown(); n != 0 {
		t.Errorf("counter %d at the start of the next Turn, want 0", n)
	}
}
