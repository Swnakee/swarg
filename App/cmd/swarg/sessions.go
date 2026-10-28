package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/tabwriter"

	"swarg/internal/agent"
	"swarg/internal/chat"
	"swarg/internal/llm/openai"
	"swarg/internal/session"
	"swarg/internal/trace"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// Picking a session
// ---------------------------------------------------------------------------

// pickSession returns the session of infos (newest first) whose id starts with id; an empty
// id picks the newest readable one. Broken files are never picked. An unknown or ambiguous
// id is an error listing the candidates. With none true an empty id and no sessions return
// (zero, nil) instead of an error.
func pickSession(infos []session.Info, id string, none bool) (session.Info, error) {
	var ok, match []session.Info
	for _, in := range infos {
		if in.Err == "" {
			ok = append(ok, in)
		}
		// a broken file still matches by the id in its name, so resuming it reports the real error
		if id != "" && strings.HasPrefix(fileID(in), id) {
			match = append(match, in)
		}
	}
	switch {
	case id == "" && len(ok) > 0:
		return ok[0], nil
	case id == "" && none:
		return session.Info{}, nil
	case id == "":
		return session.Info{}, fmt.Errorf("no saved sessions in this project")
	case len(match) == 1:
		return match[0], nil
	case len(match) > 1:
		return session.Info{}, fmt.Errorf("session id %q is ambiguous:\n%s", id, sessionTable(match))
	case len(ok) == 0:
		return session.Info{}, fmt.Errorf("no session %q: no saved sessions in this project", id)
	}
	return session.Info{}, fmt.Errorf("no session %q; sessions of this project:\n%s", id, sessionTable(ok))
}

// fileID is the session id of in; for a broken file (no id) the one in its file name
// YYYYMMDD-HHMMSS-<id>.jsonl.
func fileID(in session.Info) string {
	if in.ID != "" {
		return in.ID
	}
	stem := strings.TrimSuffix(filepath.Base(in.Path), ".jsonl")
	return stem[strings.LastIndex(stem, "-")+1:]
}

// sessionTable renders `id  started  model  messages  title`, one line per session.
func sessionTable(infos []session.Info) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, in := range infos {
		if in.Err != "" {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", in.ID, in.Started.Format("2006-01-02 15:04"), in.Model, in.Messages, ui.Clean(in.Title))
	}
	tw.Flush()
	return strings.TrimRight(b.String(), "\n")
}

// printSessions implements --sessions.
func printSessions(infos []session.Info, out io.Writer) {
	if t := sessionTable(infos); t != "" {
		fmt.Fprintln(out, t)
		return
	}
	fmt.Fprintln(out, "no saved sessions in this project")
}

// choosePick implements -r without an id. It returns the chosen session id, or done with the
// exit code when the run ends here: empty list (0), cancelled (0), no terminal (2: the list
// goes to out and a hint to errOut), picker failure (1).
func choosePick(infos []session.Info, tty bool, pick func([]ui.PickRow, func(string) error) (string, bool, error), del func(string) error, out, errOut io.Writer) (id string, code int, done bool) {
	if len(infos) == 0 {
		fmt.Fprintln(out, "no saved sessions in this folder")
		return "", 0, true
	}
	if !tty {
		printSessions(infos, out)
		fmt.Fprintln(errOut, "swarg: no terminal to choose in: use swarg -r <id>")
		return "", 2, true
	}
	rows := make([]ui.PickRow, len(infos))
	for i, in := range infos {
		rows[i] = ui.PickRow{ID: fileID(in), Title: in.Title, Model: in.Model, Time: in.Started, Messages: in.Messages, Err: in.Err}
	}
	id, ok, err := pick(rows, del)
	switch {
	case err != nil:
		fmt.Fprintf(errOut, "swarg: %v\n", err)
		return "", 1, true
	case !ok:
		return "", 0, true
	}
	return id, 0, false
}

// ---------------------------------------------------------------------------
// Saving: the SessionFile + Trace being written, switchable (ui.SessionsAPI)
// ---------------------------------------------------------------------------

// saving is the SessionFile and Trace of one run and the ui.SessionsAPI of the project
// directory. Open switches the files being written to another saved Session (a true
// continuation, R-33): the agent keeps its Store and Trace, which forward to the current
// files under one mutex. All zero when saving is off (then only List and Open work, and Open
// only loads).
type saving struct {
	dir       string
	withTrace bool
	history   []chat.Message // the resumed Session's history, for the agent and the UI
	maxSeq    int
	model     string      // the resumed Session's last Model choice ("" for a new session)
	on        bool        // saving is on: a SessionFile is being written
	store     agent.Store // nil when saving is off
	trace     agent.Trace // nil when saving or the Trace is off

	mu    sync.Mutex
	w     *session.Writer // the current SessionFile, nil when saving is off or after close
	tw    *trace.Writer   // its Trace, nil when off
	fresh bool            // w is a new file of this run: removed when it gets no message
	prior int             // messages the current file had when it was opened
	n     int             // messages written to the current file in this run
}

var _ ui.SessionsAPI = (*saving)(nil)

// openSaving prepares the SessionFile and Trace. resume is the session to continue (zero
// Info: a new one). A problem with the files is a warning on warn and the run goes on
// without saving; a resumed session that cannot be read is an error. The files are closed
// by cl; a new session that stayed empty is removed.
func openSaving(dir string, resume session.Info, h session.Header, save, withTrace bool, keep int, cl *cleanups, warn io.Writer) (*saving, error) {
	s := &saving{dir: dir, withTrace: withTrace}
	if resume.Path != "" {
		l, err := session.Load(resume.Path)
		if err != nil {
			return nil, err
		}
		s.history, s.maxSeq, s.prior, s.model = l.Messages(), l.MaxSeq(), len(l.Entries), l.Model()
	}
	if !save {
		return s, nil
	}
	if err := session.Prune(dir, keep); err != nil {
		fmt.Fprintf(warn, "swarg: prune sessions: %v\n", err)
	}
	var (
		w   *session.Writer
		err error
	)
	if resume.Path != "" {
		w, err = session.Open(resume.Path)
	} else {
		w, err = session.Create(dir, h)
	}
	if err != nil {
		fmt.Fprintf(warn, "swarg: session not saved: %v\n", err)
		return s, nil
	}
	s.on, s.w, s.fresh = true, w, resume.Path == ""
	s.store = swStore{s}
	if withTrace {
		s.tw, s.trace = s.openTrace(w.Path(), warn), swTrace{s}
	}
	cl.Add(s.close)
	return s, nil
}

// openTrace opens the Trace of a SessionFile; a failure is a warning on warn and no Trace.
func (s *saving) openTrace(path string, warn io.Writer) *trace.Writer {
	tw, err := trace.Open(session.TracePath(path))
	if err != nil {
		fmt.Fprintf(warn, "swarg: trace not written: %v\n", err)
		return nil
	}
	return tw
}

// close closes the current files; a new file that got no message is removed with its Trace.
func (s *saving) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked()
}

func (s *saving) closeLocked() {
	if s.w == nil {
		return
	}
	s.tw.Close() // nil-safe
	s.w.Close()
	if s.fresh && s.n == 0 { // nothing was said: leave no empty file
		os.Remove(s.w.Path())
		os.Remove(session.TracePath(s.w.Path()))
	}
	s.w, s.tw = nil, nil
}

// saved reports whether the current Session has at least one message on disk.
func (s *saving) saved() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.on && s.prior+s.n > 0
}

// ---------------------------------------------------------------------------
// ui.SessionsAPI
// ---------------------------------------------------------------------------

// List is the readable saved Sessions of the project (the UI sorts them).
func (s *saving) List() ([]ui.SessionRow, error) {
	infos, err := session.List(s.dir)
	if err != nil {
		return nil, err
	}
	rows := make([]ui.SessionRow, 0, len(infos))
	for _, in := range infos {
		if in.Err == "" {
			rows = append(rows, ui.SessionRow{ID: in.ID, Title: in.Title, Model: in.Model, Time: in.Started, Messages: in.Messages})
		}
	}
	return rows, nil
}

// Current is the id of the Session being written, "" when saving is off.
func (s *saving) Current() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return ""
	}
	return s.w.ID()
}

// Open switches to the saved Session id (an id prefix is enough): the current files are
// closed (a new file without a message is removed), the chosen SessionFile and its Trace are
// opened for appending, and its history is returned for the agent. Opening the current
// Session, or any Session while saving is off, only returns the history. When the chosen
// file cannot be opened for writing the current files stay.
func (s *saving) Open(id string) (ui.Opened, error) {
	infos, err := session.List(s.dir)
	if err != nil {
		return ui.Opened{}, err
	}
	in, err := pickSession(infos, id, false)
	if err != nil {
		return ui.Opened{}, err
	}
	l, err := session.Load(in.Path)
	if err != nil {
		return ui.Opened{}, err
	}
	o := ui.Opened{History: l.Messages(), MaxSeq: l.MaxSeq(), Model: l.Model(), Title: in.Title}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil || s.w.Path() == in.Path {
		return o, nil
	}
	w, err := session.Open(in.Path)
	if err != nil {
		return ui.Opened{}, err
	}
	s.closeLocked()
	s.w, s.fresh, s.prior, s.n = w, false, len(l.Entries), 0
	if s.withTrace {
		s.tw = s.openTrace(in.Path, io.Discard) // the UI owns the screen now: no stderr
	}
	return o, nil
}

// Delete removes the saved Session id with its Trace; the Session being written is refused.
func (s *saving) Delete(id string) error {
	if cur := s.Current(); cur != "" && cur == id {
		return errors.New("the current session cannot be deleted")
	}
	return session.Delete(s.dir, id)
}

// Rename gives the Session being written a Name (R-32).
func (s *saving) Rename(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return errors.New("sessions are not saved")
	}
	if err := s.w.Name(name); err != nil {
		return err
	}
	s.fresh = false // a named Session is kept even before its first message
	return nil
}

// ---------------------------------------------------------------------------
// agent.Store and agent.Trace forwarding to the current files
// ---------------------------------------------------------------------------

// swStore is the agent's Store: every call goes to the SessionFile of the moment.
type swStore struct{ s *saving }

// with runs f on the current file's Store under the lock; no file (closed) is a no-op.
func (x swStore) with(f func(*session.Store) error) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	if x.s.w == nil {
		return nil
	}
	return f(session.NewStore(x.s.w))
}

func (x swStore) Message(m chat.Message) error {
	return x.with(func(st *session.Store) error {
		err := st.Message(m)
		if err == nil {
			x.s.n++
		}
		return err
	})
}
func (x swStore) Patch(ps []chat.Patch) error {
	return x.with(func(st *session.Store) error { return st.Patch(ps) })
}
func (x swStore) Clear() error { return x.with(func(st *session.Store) error { return st.Clear() }) }
func (x swStore) Compact(from, to int, sum chat.Message) error {
	return x.with(func(st *session.Store) error { return st.Compact(from, to, sum) })
}
func (x swStore) Repair(ms []chat.Message) error {
	return x.with(func(st *session.Store) error { return st.Repair(ms) })
}
func (x swStore) Model(name string) error {
	return x.with(func(st *session.Store) error { return st.Model(name) })
}
func (x swStore) Flush() error { return x.with(func(st *session.Store) error { return st.Flush() }) }

// swTrace is the agent's Trace: records go to the Trace of the moment (nil-safe).
type swTrace struct{ s *saving }

func (x swTrace) cur() *trace.Writer {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return x.s.tw
}

func (x swTrace) Record(kind string, v any) { x.cur().Record(kind, v) }
func (x swTrace) Err() error                { return x.cur().Err() }

// ---------------------------------------------------------------------------
// Cleanups
// ---------------------------------------------------------------------------

// cleanups runs registered functions once, newest first. Run is safe to call from the
// signal goroutine and from a defer.
type cleanups struct {
	mu  sync.Mutex
	fns []func()
}

func (c *cleanups) Add(f func()) {
	c.mu.Lock()
	c.fns = append(c.fns, f)
	c.mu.Unlock()
}

func (c *cleanups) Run() {
	c.mu.Lock()
	fns := c.fns
	c.fns = nil
	c.mu.Unlock()
	for i := len(fns) - 1; i >= 0; i-- {
		fns[i]()
	}
}

// ---------------------------------------------------------------------------
// Model choice at start (R-30)
// ---------------------------------------------------------------------------

// startModel is the model a run uses: the --model flag, else the resumed session's last
// choice, else the configured one (config file, SWARG_MODEL). A resumed session therefore
// continues with its own model even inside the sandbox, which sets SWARG_MODEL.
func startModel(flag, resumed, configured string) string {
	switch {
	case flag != "":
		return flag
	case resumed != "":
		return resumed
	}
	return configured
}

// modelsView adapts the client's model list to ui.ModelsAPI.
type modelsView struct{ c *openai.Client }

func (v modelsView) List(ctx context.Context) ([]string, error) { return v.c.Models(ctx) }
