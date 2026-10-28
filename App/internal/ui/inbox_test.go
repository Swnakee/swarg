package ui

import (
	"testing"

	"swarg/internal/agent"
)

func TestInbox_SendNeverBlocksAndKeepsOrder(t *testing.T) {
	send, events := NewInbox()
	for i := 0; i < 1000; i++ { // no reader yet: must not block
		send(agent.Event{Kind: agent.EvQueue, Queued: i})
	}
	for i := 0; i < 1000; i++ {
		if ev := <-events; ev.Queued != i {
			t.Fatalf("event %d has Queued %d", i, ev.Queued)
		}
	}
	send(agent.Event{Kind: agent.EvDelta, Text: "late"})
	if ev := <-events; ev.Text != "late" {
		t.Errorf("late event = %+v", ev)
	}
}
