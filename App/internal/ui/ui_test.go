package ui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"swarg/internal/agent"
	"swarg/internal/chat"
	"swarg/internal/prompt"
)

// ---------------------------------------------------------------------------
// Fakes and helpers
// ---------------------------------------------------------------------------

type fakeAgent struct {
	submitted   []string
	interrupts  int
	aborts      int
	busy        bool
	clearErr    error
	clears      int
	backgrounds int
	bgResult    bool
}

func (f *fakeAgent) Start(context.Context) {}
func (f *fakeAgent) Submit(s string)       { f.submitted = append(f.submitted, s) }
func (f *fakeAgent) Interrupt()            { f.interrupts++ }
func (f *fakeAgent) Abort()                { f.aborts++ }
func (f *fakeAgent) Clear() error          { f.clears++; return f.clearErr }
func (f *fakeAgent) Busy() bool            { return f.busy }
func (f *fakeAgent) Wait()                 {}
func (f *fakeAgent) Background() bool      { f.backgrounds++; return f.bgResult }

type fakeBlocks struct {
	infos []prompt.BlockInfo
}

func (f *fakeBlocks) List() []prompt.BlockInfo { return f.infos }
func (f *fakeBlocks) SetEnabled(name string, on bool) error {
	for i := range f.infos {
		if f.infos[i].Name == name {
			f.infos[i].Enabled = on
			return nil
		}
	}
	return errors.New("unknown block " + name + "; known: base, rules")
}

type rig struct {
	m   *model
	now time.Time
	ag  *fakeAgent
	bl  *fakeBlocks
	t   *testing.T
}

func newRig(t *testing.T) *rig {
	ag := &fakeAgent{}
	bl := &fakeBlocks{infos: []prompt.BlockInfo{
		{Name: "base", Source: prompt.SourceBase, Enabled: true, Tokens: 120},
		{Name: "rules", Source: prompt.SourceProject, Tokens: 40},
	}}
	ev := make(chan agent.Event)
	close(ev) // the reader cmd returns at once in tests
	d := Deps{Agent: ag, Blocks: bl, Events: ev, Model: "qwen", Project: "swarg", Limit: 16000}
	r := &rig{ag: ag, bl: bl, t: t, now: time.Unix(1000, 0)}
	m := newModel(context.Background(), d, func() time.Time { return r.now })
	m.tick = func() tea.Cmd { return nil }
	m.bgTick = func() tea.Cmd { return nil }
	r.m = m
	r.send(tea.WindowSizeMsg{Width: 80, Height: 40})
	return r
}

// result is what executing the cmd returned by Update would do. Nothing is printed to a
// scrollback in full screen: raws and cleared must stay empty, they are kept to assert that.
type result struct {
	raws    []string // tea.Raw output
	cleared bool     // tea.ClearScreen
	quit    bool
	clip    string // tea.SetClipboard text
	clipped bool
}

// send runs Update and walks the returned command tree (Batch is a slice of Cmd).
func (r *rig) send(msg tea.Msg) result {
	_, cmd := r.m.Update(msg)
	var res result
	r.walk(cmd, &res)
	return res
}

func (r *rig) walk(cmd tea.Cmd, res *result) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); ok {
		res.quit = true
		return
	}
	if raw, ok := msg.(tea.RawMsg); ok {
		res.raws = append(res.raws, fmt.Sprint(raw.Msg))
		return
	}
	v := reflect.ValueOf(msg)
	switch {
	case !v.IsValid():
	case v.Kind() == reflect.String && v.Type().Name() == "setClipboardMsg":
		res.clip, res.clipped = v.String(), true
	case v.Type().Name() == "clearScreenMsg":
		res.cleared = true
	case v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(cmd):
		for i := 0; i < v.Len(); i++ {
			r.walk(v.Index(i).Interface().(tea.Cmd), res)
		}
	}
}

func (r *rig) events(evs ...agent.Event) result { return r.send(eventsMsg(evs)) }

func (r *rig) view() string { return ansi.Strip(r.m.View().Content) }

// rows are the screen rows of the current view, colours stripped.
func (r *rig) rows() []string { return strings.Split(r.view(), "\n") }

// convText is all text of the conversation list, ignoring the scroll position.
func (r *rig) convText() string {
	return strings.Join(plainRows(r.m.cv.rows(0, r.m.cv.total())), "\n")
}

func key(code rune, mod tea.KeyMod) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Mod: mod} }

func delta(s string) agent.Event { return agent.Event{Kind: agent.EvDelta, Text: s} }

func started(text string) agent.Event {
	return agent.Event{Kind: agent.EvTurnStarted, Text: text}
}

func ended(f chat.FinishReason, err error) agent.Event {
	return agent.Event{Kind: agent.EvTurnEnded, Finish: f, Err: err}
}

// wantLine checks that the conversation list contains part (the result is kept for the old call sites).
func (r *rig) wantLine(_ result, part string) {
	r.t.Helper()
	if text := r.convText(); !strings.Contains(text, part) {
		r.t.Errorf("list is %q, want containing %q", text, part)
	}
}

// ---------------------------------------------------------------------------
// Keys
// ---------------------------------------------------------------------------

func TestModel_KeyHandling(t *testing.T) {
	t.Run("enter submits trimmed text", func(t *testing.T) {
		r := newRig(t)
		r.m.ta.SetValue("  hello  ")
		r.send(key(tea.KeyEnter, 0))
		if !reflect.DeepEqual(r.ag.submitted, []string{"hello"}) || r.m.ta.Value() != "" {
			t.Errorf("submitted %q, input %q", r.ag.submitted, r.m.ta.Value())
		}
	})
	t.Run("enter on empty input does nothing", func(t *testing.T) {
		r := newRig(t)
		r.m.ta.SetValue("  \n ")
		r.send(key(tea.KeyEnter, 0))
		if len(r.ag.submitted) != 0 {
			t.Errorf("submitted %q", r.ag.submitted)
		}
	})
	t.Run("ctrl+j and shift+enter insert a newline", func(t *testing.T) {
		for _, k := range []tea.KeyPressMsg{key('j', tea.ModCtrl), key(tea.KeyEnter, tea.ModShift)} {
			r := newRig(t)
			r.m.ta.SetValue("a")
			r.send(k)
			r.send(tea.KeyPressMsg{Code: 'b', Text: "b"})
			if r.m.ta.Value() != "a\nb" || len(r.ag.submitted) != 0 {
				t.Errorf("%s: input %q submitted %q", k.String(), r.m.ta.Value(), r.ag.submitted)
			}
		}
	})
	t.Run("esc interrupts, then aborts, and stays out of the textarea", func(t *testing.T) {
		r := newRig(t)
		r.events(started("q"))
		r.m.ta.SetValue("keep")
		r.send(key(tea.KeyEscape, 0))
		if r.ag.interrupts != 1 || r.ag.aborts != 0 || r.m.ta.Value() != "keep" {
			t.Errorf("interrupts %d aborts %d, input %q", r.ag.interrupts, r.ag.aborts, r.m.ta.Value())
		}
		if v := r.view(); !strings.Contains(v, "Stopping… (esc again to abort)") {
			t.Errorf("no stopping hint:\n%s", v)
		}
		r.send(key(tea.KeyEscape, 0))
		r.send(key(tea.KeyEscape, 0))
		if r.ag.interrupts != 1 || r.ag.aborts != 2 {
			t.Errorf("interrupts %d aborts %d", r.ag.interrupts, r.ag.aborts)
		}
		r.events(ended(chat.FinishInterrupted, nil), started("again"))
		r.send(key(tea.KeyEscape, 0))
		if r.ag.interrupts != 2 {
			t.Errorf("counter not reset after the turn: interrupts %d", r.ag.interrupts)
		}
	})
	t.Run("esc when idle does nothing", func(t *testing.T) {
		r := newRig(t)
		r.send(key(tea.KeyEscape, 0))
		if r.ag.interrupts != 0 || r.ag.aborts != 0 {
			t.Errorf("interrupts %d aborts %d", r.ag.interrupts, r.ag.aborts)
		}
	})
	t.Run("ctrl+c clears non-empty input", func(t *testing.T) {
		r := newRig(t)
		r.m.ta.SetValue("junk")
		if res := r.send(key('c', tea.ModCtrl)); res.quit || r.m.ta.Value() != "" {
			t.Errorf("quit %v, input %q", res.quit, r.m.ta.Value())
		}
	})
	t.Run("ctrl+c on empty idle input quits", func(t *testing.T) {
		r := newRig(t)
		if res := r.send(key('c', tea.ModCtrl)); !res.quit || r.ag.interrupts != 0 {
			t.Errorf("quit %v interrupts %d", res.quit, r.ag.interrupts)
		}
	})
	t.Run("ctrl+c busy interrupts, quits after the drain, twice forces", func(t *testing.T) {
		r := newRig(t)
		r.events(started("q"))
		if res := r.send(key('c', tea.ModCtrl)); res.quit || r.ag.interrupts != 1 {
			t.Fatalf("quit %v interrupts %d", res.quit, r.ag.interrupts)
		}
		if v := r.view(); !strings.Contains(v, "finishing the current answer… (ctrl+c again to force)") {
			t.Errorf("no hint in view:\n%s", v)
		}
		if res := r.events(ended(chat.FinishInterrupted, nil)); !res.quit {
			t.Error("did not quit when the agent became idle")
		}
		r = newRig(t)
		r.events(started("q"))
		r.send(key('c', tea.ModCtrl))
		if res := r.send(key('c', tea.ModCtrl)); !res.quit {
			t.Error("second ctrl+c did not force the quit")
		}
	})
	t.Run("paste with newlines is inserted without submitting", func(t *testing.T) {
		r := newRig(t)
		r.send(tea.PasteMsg{Content: "one\ntwo"})
		if r.m.ta.Value() != "one\ntwo" || len(r.ag.submitted) != 0 {
			t.Errorf("input %q submitted %q", r.m.ta.Value(), r.ag.submitted)
		}
	})
}

// ---------------------------------------------------------------------------
// Events to view
// ---------------------------------------------------------------------------

func TestModel_AgentEventsToView(t *testing.T) {
	r := newRig(t)
	r.events(
		started("hi"),
		agent.Event{Kind: agent.EvProgress, Done: 14, Total: 18},
		agent.Event{Kind: agent.EvMeter, Used: 5600, Limit: 16000},
		agent.Event{Kind: agent.EvQueue, Queued: 1},
	)
	v := r.view()
	for _, want := range []string{"Thinking…", "reading prompt 14/18", "esc to stop", "qwen │ 35% · 5.6k/16k"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	r.events(delta("partial answer"))
	if v := r.view(); strings.Contains(v, "reading prompt") || !strings.Contains(v, "↓ ~4 tokens") {
		t.Errorf("progress should hide once text flows:\n%s", v)
	}
	r.events(agent.Event{Kind: agent.EvDraining})
	if v := r.view(); !strings.Contains(v, "Stopping…") || strings.Contains(v, "esc to stop") {
		t.Errorf("draining view:\n%s", v)
	}
	r.events(agent.Event{Kind: agent.EvQueue, Queued: 0})
	if v := r.view(); strings.Contains(v, "queued") {
		t.Errorf("queued 0 must be hidden:\n%s", v)
	}
}

// ---------------------------------------------------------------------------
// Turn end
// ---------------------------------------------------------------------------

func TestModel_TurnEndNotices(t *testing.T) {
	tests := []struct {
		name string
		ev   agent.Event
		want string
	}{
		{"truncated", ended(chat.FinishLength, nil), "answer cut off"},
		{"interrupted", ended(chat.FinishInterrupted, nil), "interrupted"},
		{"error", ended(chat.FinishError, errors.New("boom")), "error: boom"},
		{"warning", agent.Event{Kind: agent.EvWarning, Text: "empty answer"}, "empty answer"},
	}
	for _, tc := range tests {
		r := newRig(t)
		r.events(started("q"))
		r.wantLine(r.events(tc.ev), tc.want)
	}
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func TestModel_ClearBusyShowsHint(t *testing.T) {
	r := newRig(t)
	r.ag.clearErr = agent.ErrBusy
	r.events(started("keep me"))
	r.m.ta.SetValue("/clear")
	r.wantLine(r.send(key(tea.KeyEnter, 0)), "wait for the answer to finish (esc to stop it)")
	if len(r.ag.submitted) != 0 {
		t.Error("a command must never reach the model")
	}
	if !strings.Contains(r.convText(), "keep me") {
		t.Error("a busy /clear must not touch the list")
	}
	r.ag.clearErr = nil
	r.m.ta.SetValue("/clear")
	res := r.send(key(tea.KeyEnter, 0))
	if len(res.raws) != 0 || res.cleared || r.ag.clears != 2 || r.m.cv.count() != 0 {
		t.Errorf("raws %q cleared %v clears %d items %d", res.raws, res.cleared, r.ag.clears, r.m.cv.count())
	}
}

func TestModel_BlocksCommandOutput(t *testing.T) {
	r := newRig(t)
	r.m.ta.SetValue("/blocks")
	res := r.send(key(tea.KeyEnter, 0))
	for _, want := range []string{"base", "base", "on", "~120 tokens", "rules", "project", "off", "~40 tokens"} {
		r.wantLine(res, want)
	}
	r.m.ta.SetValue("/block on rules")
	r.send(key(tea.KeyEnter, 0))
	if !r.bl.infos[1].Enabled {
		t.Error("/block on rules did not enable the block")
	}
}

func TestModel_UnknownBlockShowsError(t *testing.T) {
	r := newRig(t)
	r.m.ta.SetValue("/block on nope")
	r.wantLine(r.send(key(tea.KeyEnter, 0)), "known: base, rules")
}

func TestModel_ExitAndUnknownCommand(t *testing.T) {
	r := newRig(t)
	r.m.ta.SetValue("/bogus")
	r.wantLine(r.send(key(tea.KeyEnter, 0)), "unknown command /bogus")
	r.m.ta.SetValue("/exit")
	if res := r.send(key(tea.KeyEnter, 0)); !res.quit {
		t.Error("/exit while idle must quit")
	}
	r = newRig(t)
	r.events(started("q"))
	r.m.ta.SetValue("/exit")
	if res := r.send(key(tea.KeyEnter, 0)); res.quit || r.ag.interrupts != 1 || !r.m.quitting {
		t.Errorf("/exit while busy: quit %v interrupts %d", res.quit, r.ag.interrupts)
	}
}

func TestReadEvents_CoalescesPendingEvents(t *testing.T) {
	ch := make(chan agent.Event, 3)
	ch <- delta("a")
	ch <- delta("b")
	ch <- delta("c")
	got, _ := readEvents(context.Background(), ch)().(eventsMsg)
	if len(got) != 3 {
		t.Errorf("got %d events in one batch, want 3", len(got))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if msg := readEvents(ctx, make(chan agent.Event))(); msg != nil {
		t.Errorf("cancelled reader returned %v", msg)
	}
}

func TestModel_TickAdvancesElapsed(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	r.send(tickMsg(time.Unix(1012, 0)))
	if v := r.view(); !strings.Contains(v, "(12s") {
		t.Errorf("elapsed missing:\n%s", v)
	}
}
