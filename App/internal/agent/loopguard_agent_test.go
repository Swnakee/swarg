package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// The loop guard inside a Turn
// ---------------------------------------------------------------------------

// sayLoop streams the owner's list item by item until the request is cancelled.
func sayLoop(c *call, first int) (sent int) {
	for i := first; i < first+80; i++ {
		select {
		case <-c.ctx.Done():
			return sent
		case c.ch <- chat.StreamEvent{Kind: chat.EventDelta, Text: fmt.Sprintf("%d. **Сборка и тестирование** — могу собирать и тестировать проекты.\n\n", i)}:
			sent++
		}
	}
	return sent
}

func TestLoopGuard_CancelsTheRequestAndStoresTheCutAnswer(t *testing.T) {
	h := newHealth(nil)
	r := startRig(t, func(d *Deps) { d.Health = h.check })
	r.a.Submit("привет ты кто что умеешь")
	c := r.p.next(t)
	c.say("Вот что я умею:\n\n")
	sent := sayLoop(c, 67)
	if sent >= 80 {
		t.Fatal("the request was never cancelled")
	}
	wait(t, r.a)
	if c.ctx.Err() == nil {
		t.Error("the request context was not cancelled")
	}

	hist := r.a.History()
	last := hist[len(hist)-1]
	if last.Role != chat.RoleAssistant || !last.Truncated || strings.Count(last.Content, "Сборка и тестирование") != 1 {
		t.Errorf("stored answer = %+v, want one copy of the item and Truncated", last)
	}
	if !strings.HasPrefix(last.Content, "Вот что я умею:") {
		t.Errorf("stored answer lost its start: %q", last.Content)
	}
	if w := r.rec.texts(EvWarning); len(w) != 1 || w[0] != loopWarning {
		t.Errorf("warnings = %q, want %q", w, loopWarning)
	}
	end := r.rec.of(EvTurnEnded)
	if len(end) != 1 || end[0].Finish != chat.FinishStop || end[0].Err != nil {
		t.Errorf("TurnEnded = %+v", end)
	}
	select { // the request was cut like an Abort, so the server is checked afterwards
	case <-h.calls:
	case <-time.After(deadline):
		t.Error("no health check after the guard cut the request")
	}
}

func TestLoopGuard_NextTurnWorks(t *testing.T) {
	r := startRig(t)
	r.a.Submit("a")
	sayLoop(r.p.next(t), 1)
	wait(t, r.a)
	r.a.Submit("b")
	r.p.next(t).reply("fine")
	wait(t, r.a)
	h := r.a.History()
	if last := h[len(h)-1]; last.Content != "fine" || last.Truncated {
		t.Errorf("next answer = %+v", last)
	}
}

func TestLoopGuard_LegitStreamedListIsNotStopped(t *testing.T) {
	r := startRig(t)
	r.a.Submit("list")
	c := r.p.next(t)
	for i := 1; i <= 60; i++ {
		c.say(fmt.Sprintf("%d. Пункт %c%c — своё описание номер %d\n\n", i, rune('a'+i%26), rune('A'+i/3%26), i*i))
	}
	c.end(chat.FinishStop, nil)
	wait(t, r.a)
	for _, w := range r.rec.texts(EvWarning) {
		if w == loopWarning {
			t.Errorf("a legit list was reported as a loop")
		}
	}
	if h := r.a.History(); h[len(h)-1].Truncated {
		t.Error("a legit list was cut")
	}
}

func TestLoopGuard_RepeatingStepAfterToolsEndsTheTurn(t *testing.T) {
	r := toolRig(t, newFakeTools())
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolRead, ""))
	sayLoop(r.p.next(t), 1)
	wait(t, r.a)
	assertHistoryValid(t, r.a.History())
	if w := r.rec.texts(EvWarning); len(w) != 1 || w[0] != loopWarning {
		t.Errorf("warnings = %q", w)
	}
}

func TestLoopGuard_ToolCallsOfAShortStepAreUnaffected(t *testing.T) {
	ft := newFakeTools()
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("Читаю файл.\n", toolCall(1, toolRead, ""), toolCall(2, toolRead, ""), toolCall(3, toolRead, ""), toolCall(4, toolRead, ""))
	r.p.next(t).reply("done")
	wait(t, r.a)
	if got := len(ft.events()); got != 8 {
		t.Errorf("tool log = %v, want all four calls run", ft.events())
	}
	if w := r.rec.texts(EvWarning); len(w) != 0 {
		t.Errorf("warnings = %q", w)
	}
}

func TestLoopGuard_InterruptStillWinsOverTheGuard(t *testing.T) {
	r := startRig(t)
	r.a.Submit("go")
	c := r.p.next(t)
	c.say("start\n")
	r.a.Interrupt()
	r.rec.until(t, EvDraining, 1)
	sayLoopDrain(c)
	c.end(chat.FinishStop, nil)
	wait(t, r.a)
	if w := r.rec.texts(EvWarning); len(w) != 0 {
		t.Errorf("warnings = %q, a drained stream must not feed the guard", w)
	}
}

func sayLoopDrain(c *call) {
	for i := 0; i < 10; i++ {
		c.say(fmt.Sprintf("%d. **Сборка и тестирование** — могу собирать и тестировать проекты.\n\n", i))
	}
}
