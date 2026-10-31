package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Writer
// ---------------------------------------------------------------------------

// ErrClosed is returned by a Writer method called after Close.
var ErrClosed = errors.New("session: writer is closed")

// ErrBlankName is returned by Name for a Name without any visible character.
var ErrBlankName = errors.New("session: the name is blank")

// Patch changes a stored message after the fact (a stub batch or an argument
// shrink). Stub is the short text sent to the model instead of the content;
// Args maps a tool call id of that message to its shrunk raw JSON arguments.
type Patch = chat.Patch

// Writer appends records to one SessionFile. Every record is a single write
// to a file opened with O_APPEND. Safe for concurrent use.
type Writer struct {
	mu     sync.Mutex
	f      *os.File
	path   string
	id     string
	seq    int // last seq used
	closed bool
}

// Create makes dir (0700) and a new SessionFile (0600) named
// YYYYMMDD-HHMMSS-<id6>.jsonl, and writes the header. A zero Started and an
// empty ID in h are filled in.
func Create(dir string, h Header) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	if h.Started.IsZero() {
		h.Started = time.Now()
	}
	h.V = Version
	for range 5 {
		if h.ID == "" {
			var b [3]byte
			if _, err := rand.Read(b[:]); err != nil {
				return nil, fmt.Errorf("session: %w", err)
			}
			h.ID = hex.EncodeToString(b[:])
		}
		path := filepath.Join(dir, h.Started.Format("20060102-150405")+"-"+h.ID+".jsonl")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o600)
		if errors.Is(err, os.ErrExist) {
			h.ID = ""
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("session: %w", err)
		}
		w := &Writer{f: f, path: path, id: h.ID}
		line, err := encodeLine(record{V: h.V, ID: h.ID, Cwd: h.Cwd, Model: h.Model, Started: h.Started, Provider: h.Provider})
		if err == nil {
			_, err = f.Write(line)
		}
		if err != nil {
			f.Close()
			os.Remove(path)
			return nil, fmt.Errorf("session: %w", err)
		}
		return w, nil
	}
	return nil, errors.New("session: cannot find a free file name")
}

// Open continues an existing SessionFile (resume): a torn last line is cut off
// first, and new records continue the file's seq numbering.
func Open(path string) (*Writer, error) {
	l, err := load(path)
	if err != nil {
		return nil, err
	}
	if err := os.Truncate(path, l.goodSize); err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	return &Writer{f: f, path: path, id: l.Header.ID, seq: l.maxSeq}, nil
}

// Path is the SessionFile path.
func (w *Writer) Path() string { return w.path }

// ID is the short id of the session (the 6 hex in the file name).
func (w *Writer) ID() string { return w.id }

// nextSeq is the seq a message gets: the one it carries (the agent numbers its history) when
// that is above everything written so far, else the next free one. Caller holds w.mu.
func (w *Writer) nextSeq(m chat.Message) int {
	if m.Seq > w.seq {
		return m.Seq
	}
	return w.seq + 1
}

// Message appends a message and returns the seq it got: the message's own Seq when it is
// above every seq written so far, else the next one (monotonic per file, starting at 1).
func (w *Writer) Message(m chat.Message) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	seq := w.nextSeq(m)
	if err := w.write(record{Msg: ptr(toDTO(seq, m))}); err != nil {
		return 0, err
	}
	w.seq = seq
	return seq, nil
}

// Patch records stub / shrunk-argument changes of stored messages.
func (w *Writer) Patch(ps []Patch) error {
	if len(ps) == 0 {
		return nil
	}
	r := record{}
	for _, p := range ps {
		r.Patch = append(r.Patch, patchRecord{Seq: p.Seq, Stub: p.Stub, Args: p.Args})
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.write(r)
}

// Name records that the Session was given a Name (R-32). The Name is one line: surrounding
// blanks are dropped and inner runs of blanks (newlines included) become one space; a blank
// Name is an error and writes nothing. The history is not touched (I-11).
func (w *Writer) Name(name string) error {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return ErrBlankName
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.write(record{Name: name})
}

// Clear records that the history was emptied.
func (w *Writer) Clear() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.write(record{Clear: true})
}

// Compact records that the messages from fromSeq to toSeq (inclusive, by
// position in the history) were replaced by summary; it returns the summary's seq.
func (w *Writer) Compact(fromSeq, toSeq int, summary chat.Message) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	seq := w.nextSeq(summary)
	c := &compactRecord{FromSeq: fromSeq, ToSeq: toSeq, Summary: toDTO(seq, summary)}
	if err := w.write(record{Compact: c}); err != nil {
		return 0, err
	}
	w.seq = seq
	return seq, nil
}

// Repair appends synthetic messages (cancelled results of dangling calls) and
// returns their seqs.
func (w *Writer) Repair(ms []chat.Message) ([]int, error) {
	if len(ms) == 0 {
		return nil, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	seqs := make([]int, len(ms))
	r := record{}
	last := w.seq
	for i, m := range ms {
		if m.Seq > last {
			last = m.Seq
		} else {
			last++
		}
		seqs[i] = last
		r.Repair = append(r.Repair, toDTO(last, m))
	}
	if err := w.write(r); err != nil {
		return nil, err
	}
	w.seq = last
	return seqs, nil
}

// Model records a Model choice: later requests of the Session use name (R-30). A blank name
// is an error and writes nothing.
func (w *Writer) Model(name string) error {
	if name == "" {
		return errors.New("session: blank model")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.write(record{Model: name})
}

// Provider records a Source switch (R-101): the Session runs on name from here on. A blank name
// is an error and writes nothing.
func (w *Writer) Provider(name string) error {
	if name == "" {
		return errors.New("session: blank provider")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.write(record{Provider: name})
}

// Thinking records a thinking mode switch; the last one wins on replay.
func (w *Writer) Thinking(on bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.write(record{Think: &on})
}

// Spend records the cost in USD of one response of model (R-111); replay sums the lines per model.
// A blank model or a cost that is not positive writes nothing.
func (w *Writer) Spend(model string, usd float64) error {
	if model == "" || !(usd > 0) {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.write(record{Spend: &spendRecord{Model: model, USD: usd}})
}

// Profile records a prompt profile choice (R-118); the last one wins on replay. A blank name is an
// error and writes nothing.
func (w *Writer) Profile(name string) error {
	if name == "" {
		return errors.New("session: blank prompt profile")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.write(record{Prompt: name})
}

// Flush forces the file to stable storage.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrClosed
	}
	if err := w.f.Sync(); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return nil
}

// Close syncs and closes the file. Later calls return ErrClosed.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrClosed
	}
	w.closed = true
	serr := w.f.Sync()
	if err := w.f.Close(); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	if serr != nil {
		return fmt.Errorf("session: %w", serr)
	}
	return nil
}

// write appends one record as one write. The caller holds w.mu.
func (w *Writer) write(r record) error {
	if w.closed {
		return ErrClosed
	}
	line, err := encodeLine(r)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	if _, err := w.f.Write(line); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return nil
}

func ptr[T any](v T) *T { return &v }
