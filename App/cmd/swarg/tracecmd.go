package main

import (
	"fmt"
	"io"
	"os"

	"swarg/internal/session"
	"swarg/internal/trace"
)

// ---------------------------------------------------------------------------
// swarg trace [id]
// ---------------------------------------------------------------------------

// runTrace prints the Trace timeline of the session id (a prefix) of the project
// at cwd, or of its newest session. It returns the exit code (0 ok, 1 error).
func runTrace(dir, id string, out io.Writer) (int, error) {
	infos, err := session.List(dir)
	if err != nil {
		return 1, err
	}
	in, err := pickSession(infos, id, false)
	if err != nil {
		return 1, err
	}
	f, err := os.Open(session.TracePath(in.Path))
	if os.IsNotExist(err) {
		return 1, fmt.Errorf("session %s has no trace (saved with --trace-off?)", in.ID)
	}
	if err != nil {
		return 1, err
	}
	defer f.Close()
	if err := trace.Timeline(f, out); err != nil {
		return 1, err
	}
	return 0, nil
}
