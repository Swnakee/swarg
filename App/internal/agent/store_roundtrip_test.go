package agent

import (
	"fmt"
	"reflect"
	"testing"

	"swarg/internal/chat"
	"swarg/internal/session"
)

// ---------------------------------------------------------------------------
// I-6: the SessionFile replayed by session.Load equals the live history (real Writer, real files)
// ---------------------------------------------------------------------------

func newSessionStore(t *testing.T) (*session.Writer, func(*Deps)) {
	t.Helper()
	w, err := session.Create(t.TempDir(), session.Header{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w, func(d *Deps) { d.Store = session.NewStore(w) }
}

// sameAsFile fails unless the file replays to exactly the live history.
func sameAsFile(t *testing.T, w *session.Writer, a *Agent, when string) session.Loaded {
	t.Helper()
	l, err := session.Load(w.Path())
	if err != nil {
		t.Fatalf("%s: %v", when, err)
	}
	if got, want := l.Messages(), a.History(); !(len(got) == 0 && len(want) == 0) && !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: the replayed file differs from the live history\nfile: %+v\nlive: %+v", when, got, want)
	}
	return l
}

func TestStore_SessionFileReplaysToTheLiveHistoryAtEveryTurnEnd(t *testing.T) {
	w, plug := newSessionStore(t)
	et := newEconTools()
	for _, id := range []string{"c1", "c2", "c3", "c4"} {
		et.out[id] = big(500)
	}
	var r *rig
	et.onRun = func(id string) {
		if id == "c4" {
			r.a.Submit("one more thing") // injected mid-Turn: the inject batch
		}
	}
	r = econRig(t, et, plug, func(d *Deps) { d.Approve = nil })

	// Turn 1: stale read, a big write (arguments shrink), an edit, an injected message.
	bigWrite := toolCall(3, "write_file", fmt.Sprintf(`{"path":"w.go","content":%q}`, big(3000)))
	script(t, r, "go", [][]chat.ToolCall{
		{readCall(1, "a.go"), readCall(2, "a.go")},
		{bigWrite},
		{shellCall(4)},
	}, "done")
	// The injected text got its own answer step? script ends the Turn at "done".
	sameAsFile(t, w, r.a, "after turn 1")
	if h := r.a.History(); !hasStub(h) || !hasInjected(h) || !hasShrunk(h) {
		t.Fatalf("the scripted Turn must exercise stubs, inject and shrink: %+v", h)
	}

	// Turn 2: an error result.
	et.fail["c6"] = true
	script(t, r, "next", [][]chat.ToolCall{{readCall(6, "b.go")}}, "second answer")
	sameAsFile(t, w, r.a, "after turn 2")

	// Turn 3: the loop guard cuts the answer.
	r.a.Submit("loop")
	sayLoop(r.p.next(t), 1)
	wait(t, r.a)
	if last := r.a.History(); !last[len(last)-1].Truncated {
		t.Fatalf("loop guard did not cut: %+v", last[len(last)-1])
	}
	sameAsFile(t, w, r.a, "after the loop guard cut")

	// Clear, then a new life; Seq keeps counting.
	if err := r.a.Clear(); err != nil {
		t.Fatal(err)
	}
	sameAsFile(t, w, r.a, "after Clear")
	nextTurn(t, r, "fresh start")
	l := sameAsFile(t, w, r.a, "after the Turn that follows Clear")
	if len(l.Entries) != 2 || l.Entries[0].Seq <= 10 {
		t.Errorf("entries after Clear: %+v", l.Entries)
	}
	if len(l.Dangling) != 0 {
		t.Errorf("dangling %+v", l.Dangling)
	}
}

func TestStore_InterruptedTurnReplays(t *testing.T) {
	w, plug := newSessionStore(t)
	r := startRig(t, plug)
	r.a.Submit("q")
	c := r.p.next(t)
	c.say("partial")
	r.rec.until(t, EvDelta, 1)
	r.a.Interrupt()
	c.reply(" more")
	wait(t, r.a)
	sameAsFile(t, w, r.a, "after an interrupted Turn")
}

func TestLoad_ResumeFromTheFileEqualsTheSavedHistoryAndCarriesOn(t *testing.T) {
	w, plug := newSessionStore(t)
	et := newEconTools()
	et.out["c1"] = big(500)
	r := econRig(t, et, plug, func(d *Deps) { d.Approve = nil })
	script(t, r, "go", [][]chat.ToolCall{{readCall(1, "a.go")}}, "done")
	saved := r.a.History()
	sameAsFile(t, w, r.a, "saved")

	// A fresh process: reopen the file, load it, keep working.
	l, err := session.Load(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	w2, err := session.Open(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w2.Close() })
	r2 := econRig(t, newEconTools(), func(d *Deps) { d.Store = session.NewStore(w2); d.Approve = nil })
	if err := r2.a.Load(l.Messages(), WithMaxSeq(l.MaxSeq())); err != nil {
		t.Fatal(err)
	}
	wait(t, r2.a)
	if got := r2.a.History(); !reflect.DeepEqual(got, saved) {
		t.Fatalf("resumed history differs\n got %+v\nwant %+v", got, saved)
	}
	script(t, r2, "carry on", [][]chat.ToolCall{{readCall(9, "b.go")}}, "again")
	sameAsFile(t, w2, r2.a, "after the resumed Turn")
	l2, _ := session.Load(w.Path())
	if len(l2.Entries) <= len(l.Entries) {
		t.Error("the resumed Turn was not appended to the file")
	}
}

func TestLoad_DanglingCallIsRepairedInFileAndHistory(t *testing.T) {
	// A file whose last assistant message has a call without a result (the process died).
	w, plug := newSessionStore(t)
	_ = plug
	msgs := []chat.Message{
		{Role: chat.RoleUser, Content: "go", Seq: 1, TurnStart: true},
		{Role: chat.RoleAssistant, Seq: 2, ToolCalls: []chat.ToolCall{{ID: "c1", Name: "shell", Args: `{}`}}},
	}
	for _, m := range msgs {
		if _, err := w.Message(m); err != nil {
			t.Fatal(err)
		}
	}
	l, err := session.Load(w.Path())
	if err != nil || len(l.Dangling) != 1 {
		t.Fatalf("Load: %v, dangling %+v", err, l.Dangling)
	}
	r := startRig(t, plug)
	if err := r.a.Load(l.Messages(), WithMaxSeq(l.MaxSeq())); err != nil {
		t.Fatal(err)
	}
	wait(t, r.a)
	assertHistoryValid(t, r.a.History())
	l2 := sameAsFile(t, w, r.a, "after the repair")
	if len(l2.Dangling) != 0 || l2.Entries[2].Msg.Content != "cancelled by the user" {
		t.Errorf("file after repair: %+v", l2)
	}
}

func hasStub(h []chat.Message) bool {
	for _, m := range h {
		if m.Stub != "" {
			return true
		}
	}
	return false
}

func hasInjected(h []chat.Message) bool {
	for _, m := range h {
		if m.Injected {
			return true
		}
	}
	return false
}

func hasShrunk(h []chat.Message) bool {
	for _, m := range h {
		for _, c := range m.ToolCalls {
			if c.Name == "write_file" && len(c.Args) < 500 {
				return true
			}
		}
	}
	return false
}
