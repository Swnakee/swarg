package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// richAgent is the fake agent with the optional parts (compacter, loader).
type richAgent struct {
	*fakeAgent
	compacts   int
	compactErr error
	loaded     [][]chat.Message
	loadOpts   int // options passed to the last Load
	loadErr    error
}

func (f *richAgent) Compact(context.Context) error { f.compacts++; return f.compactErr }
func (f *richAgent) Load(h []chat.Message, opts ...agent.LoadOption) error {
	f.loaded = append(f.loaded, h)
	f.loadOpts = len(opts)
	return f.loadErr
}

type fakeSessions struct {
	rows      []SessionRow
	listErr   error
	hist      map[string][]chat.Message
	loadErr   error
	loads     []string // ids passed to Open
	deleted   []string
	deleteErr error
	renamed   []string
	renameErr error
	current   string
}

func (f *fakeSessions) List() ([]SessionRow, error) { return f.rows, f.listErr }
func (f *fakeSessions) Open(id string) (Opened, error) {
	f.loads = append(f.loads, id)
	if f.loadErr != nil {
		return Opened{}, f.loadErr
	}
	title := id
	for _, r := range f.rows {
		if r.ID == id {
			title = r.Title
		}
	}
	h := f.hist[id]
	f.current = id
	return Opened{History: h, MaxSeq: len(h), Model: "m", Title: title}, nil
}
func (f *fakeSessions) Delete(id string) error {
	f.deleted = append(f.deleted, id)
	return f.deleteErr
}
func (f *fakeSessions) Rename(name string) error {
	f.renamed = append(f.renamed, name)
	return f.renameErr
}
func (f *fakeSessions) Current() string { return f.current }

func newSessRig(t *testing.T) (*rig, *richAgent, *fakeSessions) {
	r := newRig(t)
	ag := &richAgent{fakeAgent: r.ag}
	ss := &fakeSessions{hist: map[string][]chat.Message{}}
	r.m.d.Agent, r.m.d.Sessions = ag, ss
	return r, ag, ss
}

func (r *rig) cmd(line string) result {
	r.m.ta.SetValue(line)
	return r.send(key(tea.KeyEnter, 0))
}

func sampleHistory() []chat.Message {
	return []chat.Message{
		{Role: chat.RoleSystem, Content: "sys"},
		{Role: chat.RoleUser, Content: "read a.go"},
		{Role: chat.RoleAssistant, Content: "sure", ToolCalls: []chat.ToolCall{
			{ID: "c1", Name: "read_file", Args: `{"path":"a.go"}`},
			{ID: "c2", Name: "shell", Args: `{"command":"false"}`},
		}},
		{Role: chat.RoleTool, ToolCallID: "c1", Name: "read_file", Content: "1: package a", Brief: "read 1 line", Label: "a.go"},
		{Role: chat.RoleTool, ToolCallID: "c2", Name: "shell", Content: "boom", Brief: "exit 1", Label: "false", Failed: true},
		{Role: chat.RoleAssistant, Content: "earlier work was done", Summary: true},
		{Role: chat.RoleAssistant, Content: "all done"},
	}
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

func TestParseCommand_SessionCommands(t *testing.T) {
	for line, want := range map[string]Command{
		"/compact": {Kind: KindCompact}, "/sessions": {Kind: KindSessions},
		"/context": {Kind: KindContext}, " /resume ": {Kind: KindSessions},
		"/rename  api   work ": {Kind: KindRename, Name: "api work"},
	} {
		if got, err := ParseCommand(line); err != nil || got != want {
			t.Errorf("ParseCommand(%q) = %+v, %v; want %+v", line, got, err, want)
		}
	}
	for line, part := range map[string]string{
		"/resume 1": "usage: /resume", "/compact x": "usage: /compact", "/sessions 1": "usage: /sessions",
		"/context x": "usage: /context", "/rename": "usage: /rename <name>", "/zzz": "unknown command /zzz (see /help)",
	} {
		if _, err := ParseCommand(line); err == nil || !strings.Contains(err.Error(), part) {
			t.Errorf("ParseCommand(%q) error = %v, want containing %q", line, err, part)
		}
	}
}

// ---------------------------------------------------------------------------
// /compact
// ---------------------------------------------------------------------------

func TestCompactCommand_CallsAgent(t *testing.T) {
	r, ag, _ := newSessRig(t)
	r.cmd("/compact")
	if ag.compacts != 1 || strings.Contains(r.convText(), "queued") {
		t.Errorf("compacts %d, list %q", ag.compacts, r.convText())
	}
}

func TestCompactCommand_BusyIsQueued(t *testing.T) {
	r, ag, _ := newSessRig(t)
	r.events(started("q"))
	r.cmd("/compact")
	if ag.compacts != 1 {
		t.Errorf("compacts %d", ag.compacts)
	}
	r.wantLine(result{}, "compaction queued")
}

func TestCompactCommand_ErrorAndMissingPart(t *testing.T) {
	r, ag, _ := newSessRig(t)
	ag.compactErr = errors.New("stopped")
	r.cmd("/compact")
	r.wantLine(result{}, "compact: stopped")
	r2 := newRig(t) // the plain fake agent has no Compact
	r2.cmd("/compact")
	r2.wantLine(result{}, "compaction is not available")
}

// ---------------------------------------------------------------------------
// /sessions
// ---------------------------------------------------------------------------

func TestSessionsCommand_OpensThePickerNewestFirst(t *testing.T) {
	r, _, ss := newSessRig(t)
	t1 := time.Date(2026, 10, 26, 9, 0, 0, 0, time.Local)
	t2 := time.Date(2026, 10, 27, 13, 5, 0, 0, time.Local)
	ss.rows = []SessionRow{{ID: "a", Title: "old one", Model: "m1", Time: t1, Messages: 3}, {ID: "b", Title: "new one", Model: "m2", Time: t2, Messages: 5}}
	r.cmd("/sessions")
	if r.m.pick == nil {
		t.Fatal("/sessions did not open the picker")
	}
	text := r.view()
	i1, i2 := strings.Index(text, "2026-10-27 13:05  m2     5 msgs  new one"), strings.Index(text, "2026-10-26 09:00  m1     3 msgs  old one")
	if i1 < 0 || i2 < 0 || i1 > i2 {
		t.Errorf("picker:\n%s", text)
	}
	if !strings.Contains(text, "Saved sessions") {
		t.Errorf("picker header missing:\n%s", text)
	}
	r.send(key(tea.KeyEscape, 0))
	if r.m.pick != nil {
		t.Error("Esc did not close the picker")
	}
	r.cmd("/resume")
	if r.m.pick == nil {
		t.Error("a bare /resume did not open the picker")
	}
}

func TestSessionsCommand_EmptyErrorAndNil(t *testing.T) {
	r, _, ss := newSessRig(t)
	r.cmd("/sessions")
	r.wantLine(result{}, "no saved sessions")
	ss.listErr = errors.New("disk")
	r.cmd("/sessions")
	r.wantLine(result{}, "sessions: disk")
	if r.m.pick != nil {
		t.Error("the picker opened with nothing to show")
	}
	r2 := newRig(t)
	r2.cmd("/sessions")
	r2.wantLine(result{}, "sessions are not saved")
	r2.cmd("/resume")
	r2.wantLine(result{}, "sessions are not saved")
	r2.cmd("/rename x")
	r2.wantLine(result{}, "sessions are not saved")
}

// ---------------------------------------------------------------------------
// Opening a session from the picker rebuilds the list
// ---------------------------------------------------------------------------

// openNth opens the picker and chooses its n-th row (0 = the newest).
func (r *rig) openNth(n int) {
	r.cmd("/sessions")
	for range n {
		r.send(key(tea.KeyDown, 0))
	}
	r.send(key(tea.KeyEnter, 0))
}

func TestOpenSession_RebuildsList(t *testing.T) {
	r, ag, ss := newSessRig(t)
	ss.rows = []SessionRow{
		{ID: "old", Title: "t-old", Time: time.Unix(100, 0)},
		{ID: "new", Title: "t-new", Time: time.Unix(200, 0)},
	}
	ss.hist["old"] = sampleHistory()
	r.events(started("stale"), ended(chat.FinishStop, nil))
	r.openNth(1) // the older one
	if len(ss.loads) != 1 || ss.loads[0] != "old" || len(ag.loaded) != 1 || len(ag.loaded[0]) != 7 {
		t.Fatalf("loads %v, agent loads %d", ss.loads, len(ag.loaded))
	}
	if ag.loadOpts != 3 {
		t.Errorf("Load got %d options, want WithMaxSeq, WithModel and WithThinking", ag.loadOpts)
	}
	text := r.convText()
	for _, want := range []string{"read a.go", "sure", "read_file(a.go)  #1", "read 1 line", "shell(false)  #2",
		"error: exit 1", "work summary", "earlier work was done", "all done", "resumed session t-old"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "stale") {
		t.Errorf("old list kept:\n%s", text)
	}
	if len(r.m.calls) != 2 || !r.m.calls[1].done || r.m.calls[1].err == nil || r.m.calls[0].output != "1: package a" {
		t.Errorf("calls = %+v", r.m.calls)
	}
	r.cmd("/tool 1") // numbering works on loaded blocks
	r.wantLine(result{}, "1: package a")
}

func TestOpenSession_AgentBusyRefused(t *testing.T) {
	r, ag, ss := newSessRig(t)
	ss.rows = []SessionRow{{ID: "a", Title: "t"}}
	ag.busy = true // the agent says busy though no Turn shows in the UI
	r.openNth(0)
	if len(ss.loads) != 0 || len(ag.loaded) != 0 || r.m.pick == nil || !strings.Contains(r.view(), waitTurnText) {
		t.Errorf("opened while the agent is busy: %v, picker open %v", ss.loads, r.m.pick != nil)
	}
}

func TestOpenSession_AgentLoadFailureKeepsTheList(t *testing.T) {
	r, ag, ss := newSessRig(t)
	ss.rows = []SessionRow{{ID: "a", Title: "t"}}
	ss.hist["a"] = []chat.Message{{Role: chat.RoleUser, Content: "kept?"}}
	ag.loadErr = errors.New("agent: stopped")
	r.openNth(0)
	if r.m.pick == nil || !strings.Contains(r.view(), "resume: agent: stopped") {
		t.Errorf("the reason is not in the picker footer:\n%s", r.view())
	}
	if strings.Contains(r.convText(), "kept?") {
		t.Error("list rebuilt although the agent refused the Load")
	}
}

func TestRebuild_CallWithoutResultIsCancelled(t *testing.T) {
	r := newRig(t)
	r.m.rebuild([]chat.Message{
		{Role: chat.RoleUser, Content: "q"},
		{Role: chat.RoleAssistant, ToolCalls: []chat.ToolCall{{ID: "c1", Name: "shell", Args: `{}`}}},
	})
	r.wantLine(result{}, "cancelled")
	if len(r.m.runN) != 0 || !r.m.calls[0].done {
		t.Errorf("call left running: %+v", r.m.calls)
	}
}

func TestDeps_HistoryPrefillsList(t *testing.T) {
	r := newRig(t)
	d := r.m.d
	d.History = sampleHistory()
	m := newModel(context.Background(), d, time.Now)
	m.resize(80, 40)
	r2 := &rig{m: m, t: t, ag: r.ag}
	for _, want := range []string{"read a.go", "read_file(a.go)  #1", "work summary", "all done"} {
		r2.wantLine(result{}, want)
	}
	if m.nextN != 2 {
		t.Errorf("nextN = %d", m.nextN)
	}
}

// ---------------------------------------------------------------------------
// /context
// ---------------------------------------------------------------------------

func TestContextCommand_ShowsBreakdownAndCache(t *testing.T) {
	r := newRig(t)
	r.cmd("/context")
	r.wantLine(result{}, "no context data yet")
	r.events(agent.Event{Kind: agent.EvMeter, Used: 1000, Limit: 16000, CachePct: 80, HasCache: true,
		Breakdown: agent.Breakdown{System: 400, User: 100, Answers: 100, CallArgs: 50, ResultsFull: 300, Stubs: 50}})
	r.cmd("/context")
	for _, want := range []string{"system + tools", "~400 tokens", "40%", "tool results", "~300 tokens", "30%", "stubs", "cache", "80%"} {
		r.wantLine(result{}, want)
	}
}
