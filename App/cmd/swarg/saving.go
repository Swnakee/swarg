package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"swarg/internal/agent"
	"swarg/internal/chat"
	"swarg/internal/session"
	"swarg/internal/trace"
	"swarg/internal/ui"
)

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
	thinking  bool               // the resumed Session's last thinking mode
	model     string             // the resumed Session's last Model choice ("" for a new session)
	provider  string             // the resumed Session's Source ("" = the configured one)
	spend     map[string]float64 // the resumed Session's cost per model (R-111)
	tokens    agent.Tokens       // the resumed Session's token usage (R-131)
	profile   string             // the resumed Session's prompt profile choice ("" = none, R-118)
	onProfile func(string)       // called with the prompt profile of a Session the app switches to ("" = none)
	on        bool               // saving is on: a SessionFile is being written
	store     agent.Store        // nil when saving is off
	trace     agent.Trace        // nil when saving or the Trace is off

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
		s.history, s.maxSeq, s.prior, s.model, s.thinking, s.provider, s.spend = l.Messages(), l.MaxSeq(), len(l.Entries), l.Model(), l.Thinking(), l.Provider(), l.Spend()
		s.tokens = tokensOf(l)
		s.profile = l.Profile()
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
	o := ui.Opened{History: l.Messages(), MaxSeq: l.MaxSeq(), Model: l.Model(), Think: l.Thinking(), Spend: l.Spend(), Tokens: tokensOf(l), Title: in.Title}
	s.mu.Lock()
	defer s.mu.Unlock()
	restore := func() { // the profile choice of the opened Session (R-118)
		if s.onProfile != nil {
			s.onProfile(l.Profile())
		}
	}
	if s.w == nil || s.w.Path() == in.Path {
		restore()
		return o, nil
	}
	w, err := session.Open(in.Path)
	if err != nil {
		return ui.Opened{}, err
	}
	defer restore()
	s.closeLocked()
	s.w, s.fresh, s.prior, s.n = w, false, len(l.Entries), 0
	if s.withTrace {
		s.tw = s.openTrace(in.Path, io.Discard) // the UI owns the screen now: no stderr
	}
	return o, nil
}

// RecordProfile writes a prompt profile choice to the current SessionFile (no file: nothing).
func (s *saving) RecordProfile(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return nil
	}
	return s.w.Profile(name)
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
func (x swStore) Provider(name string) error {
	return x.with(func(st *session.Store) error { return st.Provider(name) })
}
func (x swStore) Thinking(on bool) error {
	return x.with(func(st *session.Store) error { return st.Thinking(on) })
}
func (x swStore) Spend(model string, usd float64) error {
	return x.with(func(st *session.Store) error { return st.Spend(model, usd) })
}
func (x swStore) Usage(in, cached, out int) error {
	return x.with(func(st *session.Store) error { return st.Usage(in, cached, out) })
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
