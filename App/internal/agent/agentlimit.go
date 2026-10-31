package agent

import (
	"context"
	"sync"
)

// ---------------------------------------------------------------------------
// Slots: how many agents run at once, overall and per Source (R-122)
// ---------------------------------------------------------------------------

// maxAgents is the largest number of agents that run at once (owner decision, T13).
const maxAgents = 4

// slots hands out the slots; an agent that finds none waits, holding nothing else.
type slots struct {
	global chan struct{}
	mu     sync.Mutex
	by     map[string]chan struct{}
}

func newSlots(n int) *slots {
	if n < 1 || n > maxAgents {
		n = maxAgents
	}
	return &slots{global: make(chan struct{}, n), by: map[string]chan struct{}{}}
}

// take waits for a slot of the Source (n at once, at least 1) and then a global one. onWait is
// called once before the first wait. It returns false when ctx ends first.
func (s *slots) take(ctx context.Context, source string, n int, onWait func()) (release func(), ok bool) {
	s.mu.Lock()
	src := s.by[source]
	if src == nil {
		src = make(chan struct{}, max(n, 1))
		s.by[source] = src
	}
	s.mu.Unlock()
	waited := false
	acquire := func(ch chan struct{}) bool {
		select {
		case ch <- struct{}{}:
			return true
		default:
		}
		if !waited && onWait != nil {
			waited = true
			onWait()
		}
		select {
		case ch <- struct{}{}:
			return true
		case <-ctx.Done():
			return false
		}
	}
	if !acquire(src) {
		return nil, false
	}
	if !acquire(s.global) {
		<-src
		return nil, false
	}
	return func() { <-s.global; <-src }, true
}
