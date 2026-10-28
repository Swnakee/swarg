// Package openai is a streaming client for OpenAI-compatible chat completion APIs.
package openai

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ---------------------------------------------------------------------------
// Server-Sent Events
// ---------------------------------------------------------------------------

// sseEvent is one unit of an SSE stream: a comment line or a dispatched data event.
type sseEvent struct{ Comment, Data string }

// readSSE parses an SSE stream and calls fn for every comment line and for every
// data event (dispatched at a blank line or at EOF). fn's error aborts reading.
func readSSE(r io.Reader, fn func(sseEvent) error) error {
	br := bufio.NewReader(r)
	var (
		lines   []string
		pending bool // a data line was seen since the last dispatch
	)
	dispatch := func() error {
		if !pending {
			return nil
		}
		ev := sseEvent{Data: strings.Join(lines, "\n")}
		lines, pending = nil, false
		return fn(ev)
	}
	handle := func(line string) error {
		switch {
		case line == "":
			return dispatch()
		case strings.HasPrefix(line, ":"):
			return fn(sseEvent{Comment: fieldValue(line, ":")})
		case strings.HasPrefix(line, "data:"):
			lines = append(lines, fieldValue(line, "data:"))
			pending = true
		}
		return nil
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("sse: %w", err)
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line != "" || err == nil {
			if herr := handle(line); herr != nil {
				return herr
			}
		}
		if err == io.EOF {
			return dispatch()
		}
	}
}

// fieldValue returns the text after prefix with one leading space removed.
func fieldValue(line, prefix string) string {
	return strings.TrimPrefix(strings.TrimPrefix(line, prefix), " ")
}
