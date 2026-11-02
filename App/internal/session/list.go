package session

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Listing and pruning
// ---------------------------------------------------------------------------

const (
	titleRunes = 60
	pruneGrace = time.Hour // files modified more recently may be in use
)

// Info describes one SessionFile for /sessions and --resume.
type Info struct {
	ID       string
	Path     string
	Started  time.Time
	Model    string
	Title    string // what lists show: the Name, else the first user message; 60 runes
	Messages int
	Err      string // set when the file could not be replayed
}

// List returns the SessionFiles of dir, newest first. A missing dir is an
// empty list; a broken file is listed with Err set.
func List(dir string) ([]Info, error) {
	paths, err := files(dir)
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, p := range paths {
		in := Info{Path: p}
		l, err := Load(p)
		if err != nil {
			in.Err = err.Error() // Started stays zero: broken files sort last
		} else {
			in.ID, in.Started, in.Model = l.Header.ID, l.Header.Started, l.Model()
			in.Messages = len(l.Entries)
			in.Title = title(l.Name, l.Entries)
		}
		out = append(out, in)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out, nil
}

// Prune deletes the oldest SessionFiles of dir beyond keep, together with
// their traces. keep <= 0 keeps everything; files modified in the last hour are
// never deleted.
func Prune(dir string, keep int) error {
	if keep <= 0 {
		return nil
	}
	paths, err := files(dir) // sorted by name = by start time, ascending
	if err != nil {
		return err
	}
	var errs []error
	for i := 0; i < len(paths)-keep; i++ {
		fi, err := os.Stat(paths[i])
		if err != nil || time.Since(fi.ModTime()) < pruneGrace {
			continue
		}
		if err := removeWithTrace(paths[i]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// files lists the *.jsonl files of dir by name ascending; a missing dir is empty.
func files(dir string) ([]string, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, de := range des {
		n := de.Name()
		if de.IsDir() || !strings.HasSuffix(n, ".jsonl") || strings.HasSuffix(n, ".trace.jsonl") {
			continue
		}
		out = append(out, filepath.Join(dir, n))
	}
	sort.Strings(out)
	return out, nil
}

// title is what lists show for a Session: its Name when it has one, else its first user
// message; one line, cut to titleRunes.
func title(name string, es []Entry) string {
	if name != "" {
		return cutTitle(name)
	}
	for _, e := range es {
		if e.Msg.Role == chat.RoleUser {
			return cutTitle(e.Msg.Content)
		}
	}
	return ""
}

func cutTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > titleRunes {
		s = string([]rune(s)[:titleRunes])
	}
	return s
}
