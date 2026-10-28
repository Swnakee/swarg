package ui

import (
	"sync"

	"swarg/internal/agent"
)

// ---------------------------------------------------------------------------
// Inbox: an unbounded event queue
// ---------------------------------------------------------------------------

// NewInbox returns the pair main wires between the agent and the UI: send goes to
// agent.Deps.OnEvent and never blocks; events goes to Deps.Events. Order is kept.
// The forwarding goroutine lives as long as the process.
func NewInbox() (send func(agent.Event), events <-chan agent.Event) {
	var (
		mu   sync.Mutex
		q    []agent.Event
		wake = make(chan struct{}, 1)
		out  = make(chan agent.Event)
	)
	go func() {
		for {
			mu.Lock()
			if len(q) == 0 {
				mu.Unlock()
				<-wake
				continue
			}
			ev := q[0]
			q = q[1:]
			mu.Unlock()
			out <- ev
		}
	}()
	return func(ev agent.Event) {
		mu.Lock()
		q = append(q, ev)
		mu.Unlock()
		select {
		case wake <- struct{}{}:
		default:
		}
	}, out
}
