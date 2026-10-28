package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// laneProvider: a Provider wrapper that proves the single-lane rule (I-1, R-18). A request is
// in flight from Stream until its done event is handed to the agent, so two requests overlap
// exactly when the agent started one before it had read the other to its end.
// ---------------------------------------------------------------------------

// isSummaryRequest tells a Compaction's request from a Turn's: no tools and the fixed prompt.
func isSummaryRequest(req chat.Request) bool {
	return len(req.Tools) == 0 && len(req.Messages) > 0 && req.Messages[0].Role == chat.RoleSystem &&
		req.Messages[0].Content == summaryPrompt
}

type laneProvider struct {
	inner Provider

	mu       sync.Mutex
	cur, max int
	log      []string      // "turn:start", "turn:end", "summary:start", "summary:end", in order
	changed  chan struct{} // closed and replaced on every note
}

// await blocks until the order log satisfies pred (or waitDeadline passes).
func (l *laneProvider) await(what string, pred func(order string) bool) error {
	end := time.After(waitDeadline)
	for {
		l.mu.Lock()
		ok, ch := pred(strings.Join(l.log, " ")), l.changed
		l.mu.Unlock()
		if ok {
			return nil
		}
		select {
		case <-ch:
		case <-end:
			return fmt.Errorf("timed out waiting for %s", what)
		}
	}
}

func (l *laneProvider) note(kind, what string, delta int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cur += delta
	l.max = max(l.max, l.cur)
	l.log = append(l.log, kind+":"+what)
	if l.changed != nil {
		close(l.changed)
	}
	l.changed = make(chan struct{})
}

func (l *laneProvider) Stream(ctx context.Context, req chat.Request) (<-chan chat.StreamEvent, error) {
	kind := "turn"
	if isSummaryRequest(req) {
		kind = "summary"
	}
	l.note(kind, "start", 1)
	ch, err := l.inner.Stream(ctx, req)
	if err != nil {
		l.note(kind, "end", -1)
		return nil, err
	}
	out := make(chan chat.StreamEvent)
	go func() {
		defer close(out)
		left := false
		leave := func() {
			if !left {
				left = true
				l.note(kind, "end", -1)
			}
		}
		defer leave()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if ev.Kind == chat.EventDone {
					leave()
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (l *laneProvider) maxInFlight() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.max
}

func (l *laneProvider) order() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.log, " ")
}

// withLane wraps the Deps' Provider in a laneProvider and stores it in *out.
func withLane(out **laneProvider) func(*Deps) {
	return func(d *Deps) {
		*out = &laneProvider{inner: d.Provider, changed: make(chan struct{})}
		d.Provider = *out
	}
}
