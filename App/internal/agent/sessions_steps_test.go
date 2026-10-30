package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/cucumber/godog"

	"swarg/internal/chat"
	"swarg/internal/session"
)

// ---------------------------------------------------------------------------
// Steps for sessions.feature: the real session.Writer / session.Load in a temp dir, wired as
// Deps.Store through a lazy proxy (a resumed Session reopens its file after the build).
// ---------------------------------------------------------------------------

// sessionWorld is the SessionFile side of a scenario; nil in scenarios that do not mention it.
type sessionWorld struct {
	on      bool // the Session is saved
	broken  bool // the writer is closed, so every write fails
	writer  *session.Writer
	path    string         // the saved or resumed file
	saved   []chat.Message // what the saved file replays to
	before  int            // entries of the saved file before the resume
	during  []chat.Message // the history right after the resume was requested
	loadErr error
	nameErr error          // result of the last naming (session_name.feature)
	listed  []session.Info // result of the last listing (session_name.feature)
}

// sessions returns the scenario's SessionFile world.
func (w *world) sessions() *sessionWorld {
	if w.ses == nil {
		w.ses = &sessionWorld{}
	}
	return w.ses
}

func (w *world) sessionsDir() string { return filepath.Join(w.root, "sessions") }

// proxyStore forwards to the scenario's current writer; without one it does nothing.
type proxyStore struct{ s *sessionWorld }

func (p proxyStore) cur() Store {
	if p.s.writer == nil {
		return noStore{}
	}
	return session.NewStore(p.s.writer)
}

func (p proxyStore) Message(m chat.Message) error { return p.cur().Message(m) }
func (p proxyStore) Patch(ps []chat.Patch) error  { return p.cur().Patch(ps) }
func (p proxyStore) Clear() error                 { return p.cur().Clear() }
func (p proxyStore) Compact(f, t int, s chat.Message) error {
	return p.cur().Compact(f, t, s)
}
func (p proxyStore) Repair(ms []chat.Message) error { return p.cur().Repair(ms) }
func (p proxyStore) Flush() error                   { return p.cur().Flush() }
func (p proxyStore) Thinking(on bool) error         { return p.cur().Thinking(on) }
func (p proxyStore) Model(name string) error        { return p.cur().Model(name) }
func (p proxyStore) Provider(name string) error     { return p.cur().Provider(name) }

type noStore struct{}

func (noStore) Message(chat.Message) error           { return nil }
func (noStore) Patch([]chat.Patch) error             { return nil }
func (noStore) Clear() error                         { return nil }
func (noStore) Compact(int, int, chat.Message) error { return nil }
func (noStore) Thinking(bool) error                  { return nil }
func (noStore) Repair([]chat.Message) error          { return nil }
func (noStore) Flush() error                         { return nil }
func (noStore) Model(string) error                   { return nil }
func (noStore) Provider(string) error                { return nil }

// plugSessions wires the SessionFile into the Deps; called by begin.
func (w *world) plugSessions(d *Deps) error {
	s := w.ses
	if s == nil || (!s.on && s.path == "") {
		return nil
	}
	if s.on {
		wr, err := session.Create(w.sessionsDir(), session.Header{Model: "m", Cwd: w.cwd()})
		if err != nil {
			return err
		}
		w.tb.Cleanup(func() { wr.Close() })
		if s.broken {
			wr.Close()
		}
		s.writer, s.path = wr, wr.Path()
	}
	d.Store = proxyStore{s}
	return nil
}

// ---------------------------------------------------------------------------
// Saved sessions
// ---------------------------------------------------------------------------

// twoTurns is the history of the saved Session: two finished Turns, the second with a ToolCall.
func twoTurns() []chat.Message {
	return []chat.Message{
		{Role: chat.RoleUser, Content: "first", TurnStart: true},
		{Role: chat.RoleAssistant, Content: "one"},
		{Role: chat.RoleUser, Content: "second", TurnStart: true},
		{Role: chat.RoleAssistant, ToolCalls: []chat.ToolCall{{ID: "c1", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		{Role: chat.RoleTool, ToolCallID: "c1", Name: "read_file", Content: "the text of a.txt", Brief: "1 line", Label: "read a.txt"},
		{Role: chat.RoleAssistant, Content: "two"},
	}
}

// saveSession writes msgs as a SessionFile (a stub patch on the tool result) and remembers it.
func (w *world) saveSession(msgs []chat.Message, tail string) error {
	s := w.sessions()
	wr, err := session.Create(w.sessionsDir(), session.Header{Model: "m", Cwd: w.cwd()})
	if err != nil {
		return err
	}
	for i, m := range msgs {
		m.Seq = i + 1
		if _, err := wr.Message(m); err != nil {
			return err
		}
		if m.Role == chat.RoleTool {
			if err := wr.Patch([]chat.Patch{{Seq: m.Seq, Stub: "[read_file a.txt: S]"}}); err != nil {
				return err
			}
		}
	}
	if err := wr.Close(); err != nil {
		return err
	}
	s.path = wr.Path()
	if tail != "" {
		f, err := os.OpenFile(s.path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, err = f.WriteString(tail)
		f.Close()
		if err != nil {
			return err
		}
	}
	if l, err := session.Load(s.path); err == nil {
		s.saved, s.before = l.Messages(), len(l.Entries)
	}
	return nil
}

// resume loads the saved file into the Agent the way main does: replay, reopen, Load.
func (w *world) resume() error {
	if err := w.begin(); err != nil {
		return err
	}
	s := w.sessions()
	l, err := session.Load(s.path)
	if err != nil {
		s.loadErr = err
		return nil
	}
	wr, err := session.Open(s.path)
	if err != nil {
		return err
	}
	w.tb.Cleanup(func() { wr.Close() })
	s.writer = wr
	if err := w.ag.Load(l.Messages(), WithMaxSeq(l.MaxSeq()), WithModel(l.Model()), WithThinking(l.Thinking())); err != nil {
		return err
	}
	s.during = w.ag.History()
	return nil
}

// replayed settles the Agent and replays the current SessionFile.
func (w *world) replayed() ([]chat.Message, error) {
	if err := w.settle(); err != nil {
		return nil, err
	}
	s := w.sessions()
	if s.path == "" {
		return nil, errors.New("the scenario has no SessionFile")
	}
	l, err := session.Load(s.path)
	return l.Messages(), err
}

func sameMessages(a, b []chat.Message) bool {
	return (len(a) == 0 && len(b) == 0) || reflect.DeepEqual(a, b)
}

// ---------------------------------------------------------------------------
// Step definitions
// ---------------------------------------------------------------------------

func registerSessionSteps(sc *godog.ScenarioContext, w *world) {
	registerSessionGivens(sc, w)
	sc.Step(`^the user resumes it$`, w.resume)
	registerSessionThens(sc, w)
}

func registerSessionGivens(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the Session is saved in a SessionFile$`, func() error { w.sessions().on = true; return nil })
	sc.Step(`^a SessionFile that cannot be written$`, func() error {
		s := w.sessions()
		s.on, s.broken = true, true
		return nil
	})
	sc.Step(`^saving is switched off$`, func() error { return nil })
	sc.Step(`^a saved Session with two finished Turns$`, func() error { return w.saveSession(twoTurns(), "") })
	sc.Step(`^a saved Session whose last line was cut in the middle$`, func() error {
		return w.saveSession(twoTurns(), `{"msg":{"seq":7,"role":"user","content":"cut in the mid`)
	})
	sc.Step(`^a saved Session whose last assistant Message has a ToolCall without a ToolResult$`, func() error {
		return w.saveSession([]chat.Message{
			{Role: chat.RoleUser, Content: "go", TurnStart: true},
			{Role: chat.RoleAssistant, ToolCalls: []chat.ToolCall{{ID: "c1", Name: "shell", Args: `{"command":"ls"}`}}},
		}, "")
	})
	sc.Step(`^a saved Session whose second line is not valid JSON$`, func() error {
		if err := w.saveSession(twoTurns(), ""); err != nil {
			return err
		}
		b, err := os.ReadFile(w.ses.path)
		if err != nil {
			return err
		}
		lines := strings.SplitAfter(string(b), "\n")
		lines[1] = "this is not json\n"
		return os.WriteFile(w.ses.path, []byte(strings.Join(lines, "")), 0o600)
	})
	sc.Step(`^the user resumed it$`, func() error { w.act(w.resume); return nil })
}

func registerSessionThens(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^the SessionFile holds the user Message "([^"]*)" followed by the assistant Message "([^"]*)"$`, func(user, ans string) error {
		got, err := w.replayed()
		if err != nil {
			return err
		}
		if len(got) != 2 || got[0].Role != chat.RoleUser || got[0].Content != user || got[1].Role != chat.RoleAssistant || got[1].Content != ans {
			return fmt.Errorf("the SessionFile holds %+v", got)
		}
		return nil
	})
	sc.Step(`^replaying the SessionFile gives the Session history$`, func() error {
		got, err := w.replayed()
		if err != nil {
			return err
		}
		if want := w.ag.History(); !sameMessages(got, want) {
			return fmt.Errorf("replay differs from the history\nfile: %+v\nlive: %+v", got, want)
		}
		return nil
	})
	sc.Step(`^the Session history equals the saved one$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		if got, want := w.ag.History(), w.ses.saved; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("history %+v, want the saved %+v", got, want)
		}
		return nil
	})
	sc.Step(`^the request starts with the saved Messages followed by the user Message "([^"]*)"$`, func(text string) error {
		if err := w.settle(); err != nil {
			return err
		}
		msgs, err := w.requestMessages(0)
		if err != nil {
			return err
		}
		saved := w.ses.saved
		if len(msgs) != len(saved)+2 { // system prompt, saved, the new user Message
			return fmt.Errorf("request has %d Messages, want %d", len(msgs), len(saved)+2)
		}
		for i, m := range saved {
			want := m.Content
			if m.Stub != "" {
				want = m.Stub // the request carries the Stub of a stubbed ToolResult
			}
			if got := msgs[i+1]; got["role"] != string(m.Role) || got["content"] != want && !(want == "" && got["content"] == nil) {
				return fmt.Errorf("request Message %d = %v, want %s %q", i+1, got, m.Role, want)
			}
		}
		if last := msgs[len(msgs)-1]; last["role"] != "user" || last["content"] != text {
			return fmt.Errorf("last request Message = %v", last)
		}
		return nil
	})
	sc.Step(`^the SessionFile ends with the ToolResult "([^"]*)"$`, func(text string) error {
		got, err := w.replayed()
		if err != nil {
			return err
		}
		if n := len(got); n == 0 || got[n-1].Role != chat.RoleTool || got[n-1].Content != text {
			return fmt.Errorf("the SessionFile ends with %+v", got)
		}
		return nil
	})
	sc.Step(`^the Turn ends first and the Session history then equals the saved one$`, func() error {
		if len(w.ses.during) == 0 || w.ses.during[0].Content != "go" {
			return fmt.Errorf("the history changed while the Turn ran: %+v", w.ses.during)
		}
		if err := w.settle(); err != nil {
			return err
		}
		if n := countKind(w.snapshotEvents(), EvTurnEnded); n != 1 {
			return fmt.Errorf("%d Turns ended, want the running one", n)
		}
		if got := w.ag.History(); !reflect.DeepEqual(got, w.ses.saved) {
			return fmt.Errorf("history %+v, want the saved %+v", got, w.ses.saved)
		}
		return nil
	})
	sc.Step(`^the SessionFile gains no Message$`, func() error {
		got, err := w.replayed()
		if err != nil {
			return err
		}
		if len(got) != w.ses.before {
			return fmt.Errorf("the file has %d Messages, had %d", len(got), w.ses.before)
		}
		return nil
	})
	sc.Step(`^the resume fails naming line (\d+) and the Session history is empty$`, func(line int) error {
		if err := w.settle(); err != nil {
			return err
		}
		if err := w.ses.loadErr; err == nil || !strings.Contains(err.Error(), fmt.Sprintf("line %d", line)) {
			return fmt.Errorf("resume error = %v, want one naming line %d", err, line)
		}
		return w.wantHistory()
	})
	sc.Step(`^no SessionFile exists$`, func() error {
		if err := w.settle(); err != nil {
			return err
		}
		files, _ := filepath.Glob(filepath.Join(w.sessionsDir(), "*"))
		if len(files) != 0 {
			return fmt.Errorf("SessionFiles exist: %v", files)
		}
		return nil
	})
}
