package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Deleting a session
// ---------------------------------------------------------------------------

// Delete removes the SessionFile of dir whose id is id (the id shown by List and the picker),
// together with its Trace. The id must identify exactly one file. A file whose Trace is
// already gone is still deleted. Nothing outside dir is touched.
func Delete(dir, id string) error {
	if id == "" {
		return errors.New("session: no id to delete")
	}
	paths, err := files(dir)
	if err != nil {
		return err
	}
	var match []string
	for _, p := range paths {
		if idFromName(p) == id {
			match = append(match, p)
		}
	}
	switch {
	case len(match) == 0:
		return fmt.Errorf("session: no session %q to delete", id)
	case len(match) > 1:
		return fmt.Errorf("session: id %q matches %d sessions", id, len(match))
	}
	return removeWithTrace(match[0])
}

// removeWithTrace deletes the SessionFile p and its Trace; a file that is already gone is fine.
func removeWithTrace(p string) error {
	var errs []error
	for _, f := range []string{p, TracePath(p)} {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// idFromName is the id embedded in a SessionFile name YYYYMMDD-HHMMSS-<id>.jsonl. It equals
// the Header id for a healthy file and is still defined for a broken one.
func idFromName(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	return stem[strings.LastIndex(stem, "-")+1:]
}
