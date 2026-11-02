package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Replay
// ---------------------------------------------------------------------------

// Entry is one message of the replayed history with its seq.
type Entry struct {
	Seq int
	Msg chat.Message // Msg.Seq equals Seq
}

// Dangling is a tool call of an assistant message that has no result.
type Dangling struct {
	Seq  int // seq of the assistant message
	Call chat.ToolCall
}

// Loaded is the result of replaying a SessionFile.
type Loaded struct {
	Header   Header
	Name     string // the Session's Name (/rename), "" when it was never named
	Entries  []Entry
	Dangling []Dangling // calls without results; the caller decides how to repair

	goodSize int64 // size of the file up to the last complete record
	maxSeq   int
	model    string // the last Model choice line; "" = none
	provider string // the last provider line; "" = none
	thinking bool   // the last thinking line; false = none or off
	spend    map[string]float64
	tokens   Tokens
	profile  string // the last prompt profile line; "" = none (R-118)
}

// MaxSeq is the highest seq the file ever used (also by cleared or compacted messages); the
// agent continues its numbering above it.
func (l Loaded) MaxSeq() int { return l.maxSeq }

// Model is the model the Session uses at its end: the last Model choice line, else the header's
// model (files written before model lines existed have only the header).
func (l Loaded) Model() string {
	if l.model != "" {
		return l.model
	}
	return l.Header.Model
}

// Provider is the Source the Session runs on at its end: the last provider line, else the header's
// provider; "" when the file has neither (written before T11 or on the configured Source).
func (l Loaded) Provider() string {
	if l.provider != "" {
		return l.provider
	}
	return l.Header.Provider
}

// Spend is the cost in USD of the Session so far, per model, summed over the spend lines (R-111);
// nil for a file with none (written before T13 or on a free server). The caller may keep the map.
// Tokens is what the Session's requests used (R-131): the sums of the usage lines.
type Tokens struct{ In, Cached, Out int }

// Tokens returns the summed usage lines; zero for a file without any.
func (l Loaded) Tokens() Tokens { return l.tokens }

func (l Loaded) Spend() map[string]float64 { return l.spend }

// Thinking is the thinking mode at the end of the Session; a file with no thinking line is off.
func (l Loaded) Thinking() bool { return l.thinking }

// Profile is the prompt profile the user chose in this Session (the last `prompt` line); "" when
// the file has none (the config decides, R-116).
func (l Loaded) Profile() string { return l.profile }

// Messages returns the replayed history (each message carries its Seq).
func (l Loaded) Messages() []chat.Message {
	ms := make([]chat.Message, len(l.Entries))
	for i, e := range l.Entries {
		ms[i] = e.Msg
	}
	return ms
}

// Load replays the SessionFile exactly: msg appends, patch applies stubs and
// shrunk args, clear empties, compact replaces a range by its summary, repair
// appends, a model line changes the model. An unterminated last line (a torn write) is skipped; a bad line
// anywhere else is an error naming the line.
func Load(path string) (Loaded, error) {
	f, err := os.Open(path)
	if err != nil {
		return Loaded{}, fmt.Errorf("session: %w", err)
	}
	defer f.Close()
	rd := bufio.NewReaderSize(f, 1<<16)
	var l Loaded
	for n := 1; ; n++ {
		line, err := rd.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return Loaded{}, fmt.Errorf("session: %s: %w", path, err)
		}
		if len(line) == 0 {
			break
		}
		if line[len(line)-1] != '\n' { // torn last line
			break
		}
		l.goodSize += int64(len(line))
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r record
		if err := json.Unmarshal(line, &r); err != nil {
			return Loaded{}, fmt.Errorf("session: %s: line %d: %w", path, n, err)
		}
		if n == 1 {
			if r.V == 0 {
				return Loaded{}, fmt.Errorf("session: %s: no header", path)
			}
			if r.V != Version {
				return Loaded{}, fmt.Errorf("session: %s: unsupported version %d", path, r.V)
			}
			l.Header = Header{V: r.V, ID: r.ID, Cwd: r.Cwd, Model: r.Model, Started: r.Started, Provider: r.Provider}
			continue
		}
		if err := l.apply(r); err != nil {
			return Loaded{}, fmt.Errorf("session: %s: line %d: %w", path, n, err)
		}
	}
	if l.Header.V == 0 {
		return Loaded{}, fmt.Errorf("session: %s: no header", path)
	}
	l.findDangling()
	return l, nil
}

func (l *Loaded) apply(r record) error {
	switch {
	case r.Msg != nil:
		l.add(*r.Msg)
	case r.Patch != nil:
		for _, p := range r.Patch {
			i := l.index(p.Seq)
			if i < 0 {
				return fmt.Errorf("patch: no message with seq %d", p.Seq)
			}
			e := &l.Entries[i]
			if p.Stub != "" {
				e.Msg.Stub = p.Stub
			}
			for j, c := range e.Msg.ToolCalls {
				if a, ok := p.Args[c.ID]; ok {
					e.Msg.ToolCalls[j].Args = a
				}
			}
		}
	case r.Clear:
		l.Entries = nil
	case r.Compact != nil:
		c := r.Compact
		i, j := l.index(c.FromSeq), l.index(c.ToSeq)
		if i < 0 || j < 0 || i > j {
			return fmt.Errorf("compact: bad range seq %d..%d", c.FromSeq, c.ToSeq)
		}
		tail := append([]Entry{{Seq: c.Summary.Seq, Msg: fromDTO(c.Summary)}}, l.Entries[j+1:]...)
		l.Entries = append(l.Entries[:i], tail...)
		l.maxSeq = max(l.maxSeq, c.Summary.Seq)
	case r.Repair != nil:
		for _, m := range r.Repair {
			l.add(m)
		}
	case r.Name != "":
		l.Name = r.Name // the last Name wins; the history is untouched (I-11)
	case r.Spend != nil:
		if l.spend == nil {
			l.spend = map[string]float64{}
		}
		if r.Spend.Model != "" && r.Spend.USD > 0 {
			l.spend[r.Spend.Model] += r.Spend.USD
		}
	case r.Usage != nil:
		l.tokens.In += max(r.Usage.In, 0)
		l.tokens.Cached += max(r.Usage.Cached, 0)
		l.tokens.Out += max(r.Usage.Out, 0)
	case r.Think != nil:
		l.thinking = *r.Think
	case r.Prompt != "":
		l.profile = r.Prompt
	case r.Model != "": // a Model choice (only the header line has V set)
		l.model = r.Model
	case r.Provider != "": // a Source switch (R-101)
		l.provider = r.Provider
	}
	return nil
}

func (l *Loaded) add(m msgRecord) {
	l.Entries = append(l.Entries, Entry{Seq: m.Seq, Msg: fromDTO(m)})
	l.maxSeq = max(l.maxSeq, m.Seq)
}

func (l *Loaded) index(seq int) int {
	return slices.IndexFunc(l.Entries, func(e Entry) bool { return e.Seq == seq })
}

// findDangling lists the calls that have no tool result anywhere after them.
func (l *Loaded) findDangling() {
	answered := map[string]bool{}
	for _, e := range l.Entries {
		if e.Msg.Role == chat.RoleTool {
			answered[e.Msg.ToolCallID] = true
		}
	}
	for _, e := range l.Entries {
		for _, c := range e.Msg.ToolCalls {
			if !answered[c.ID] {
				l.Dangling = append(l.Dangling, Dangling{Seq: e.Seq, Call: c})
			}
		}
	}
}
