package agent

import (
	"context"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// Provider streams one chat completion. Errors come two ways: Stream returns
// (nil, err) when the request could not start (connection refused, non-2xx),
// or the stream ends with EventDone{FinishError, Err}. Cancelling ctx
// disconnects: the agent does it on shutdown and on Abort, never on the first Interrupt.
type Provider interface {
	Stream(ctx context.Context, req chat.Request) (<-chan chat.StreamEvent, error)
}
