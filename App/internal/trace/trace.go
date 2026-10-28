// Package trace writes the debug Trace of a Session: one JSONL record per
// event, flushed as it is written, and renders a trace file as a timeline.
package trace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Limits
// ---------------------------------------------------------------------------

const (
	// MaxString is the longest string field stored whole; longer ones are cut.
	MaxString = 256 << 10
	// MaxFile is the file size after which large string fields are replaced by their size.
	MaxFile = 20 << 20
	// smallString is the longest string kept whole once the file passed MaxFile.
	smallString = 512
)

// ---------------------------------------------------------------------------
// Writer
// ---------------------------------------------------------------------------

// envelope is one line of the file; D is the payload given to Record.
type envelope struct {
	T    string          `json:"t"`
	Ms   int64           `json:"ms"`
	Kind string          `json:"kind"`
	D    json.RawMessage `json:"d,omitempty"`
}

// Writer appends records to a trace file. A nil *Writer does nothing, so the
// agent can hold one unconditionally. Safe for concurrent use. A write error
// is kept (Err) and turns every later Record into a no-op: the Trace never
// fails a Turn.
type Writer struct {
	mu        sync.Mutex
	f         *os.File
	start     time.Time
	now       func() time.Time
	size      int64
	err       error
	maxString int
	maxFile   int64
}

// Open creates (or appends to) the trace file at path: parent dir 0700, file 0600.
func Open(path string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("trace: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("trace: %w", err)
	}
	var size int64
	if fi, err := f.Stat(); err == nil {
		size = fi.Size()
	}
	return &Writer{f: f, start: time.Now(), now: time.Now, size: size, maxString: MaxString, maxFile: MaxFile}, nil
}

// Record appends one record of the given kind; v is marshalled as its payload.
// It never panics and never blocks the caller on an earlier failure.
func (w *Writer) Record(kind string, v any) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil || w.f == nil {
		return
	}
	now := w.now()
	e := envelope{T: now.Format(time.RFC3339Nano), Ms: now.Sub(w.start).Milliseconds(), Kind: kind}
	e.D = w.payload(v)
	line, err := encodeLine(e)
	if err == nil {
		_, err = w.f.Write(line) // one write per record, flushed to the OS at once
	}
	if err != nil {
		w.err = fmt.Errorf("trace: %w", err)
		return
	}
	w.size += int64(len(line))
}

// Flush syncs the file to stable storage.
func (w *Writer) Flush() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return w.err
	}
	if w.f == nil {
		return nil
	}
	if err := w.f.Sync(); err != nil {
		w.err = fmt.Errorf("trace: %w", err)
	}
	return w.err
}

// Err returns the first write error, if any (the caller shows one Warning).
func (w *Writer) Err() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// Close syncs and closes the file; later Record calls are no-ops.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return w.err
	}
	serr := w.f.Sync()
	cerr := w.f.Close()
	w.f = nil
	if w.err == nil {
		if serr != nil {
			w.err = fmt.Errorf("trace: %w", serr)
		} else if cerr != nil {
			w.err = fmt.Errorf("trace: %w", cerr)
		}
	}
	return w.err
}

// ---------------------------------------------------------------------------
// Payload and size caps
// ---------------------------------------------------------------------------

// payload marshals v and applies the string caps. The caller holds w.mu.
func (w *Writer) payload(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	raw, err := marshal(v)
	if err != nil {
		raw, _ = marshal(map[string]string{"marshal_error": err.Error()})
		return raw
	}
	over := w.size > w.maxFile
	if !over && len(raw) <= w.maxString {
		return raw // nothing can exceed the cap
	}
	var tree any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&tree); err != nil {
		return raw
	}
	out, err := marshal(w.capStrings(tree, over))
	if err != nil {
		return raw
	}
	return out
}

func (w *Writer) capStrings(v any, over bool) any {
	switch x := v.(type) {
	case string:
		switch {
		case over && len(x) > smallString:
			return fmt.Sprintf("[omitted %d bytes: trace file is over %d MB]", len(x), w.maxFile>>20)
		case len(x) > w.maxString:
			return cut(x, w.maxString) + fmt.Sprintf("\n[cut at %d of %d bytes]", w.maxString, len(x))
		}
	case []any:
		for i := range x {
			x[i] = w.capStrings(x[i], over)
		}
	case map[string]any:
		for k := range x {
			x[k] = w.capStrings(x[k], over)
		}
	}
	return v
}

// cut returns the first n bytes of s, backed off to a rune boundary.
func cut(s string, n int) string {
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// marshal is encodeLine without the trailing newline.
func marshal(v any) ([]byte, error) {
	b, err := encodeLine(v)
	return bytes.TrimSuffix(b, []byte("\n")), err
}

// encodeLine marshals v as one JSON line with no HTML escaping.
func encodeLine(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// short returns s on one line cut to n runes with an ellipsis.
func short(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}
