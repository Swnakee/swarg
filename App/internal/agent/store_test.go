package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Fakes: an in-memory Store and Trace
// ---------------------------------------------------------------------------

// storeOp is one call the fakeStore saw.
type storeOp struct {
	kind  string // message | patch | clear | compact | repair | model | flush
	model string // model: the chosen id
	on    bool   // thinking: the new mode
	from  int    // compact: first Seq replaced
	to    int    // compact: last Seq replaced
	msg   chat.Message
	msgs  []chat.Message
	patch []chat.Patch
}

type fakeStore struct {
	mu      sync.Mutex
	ops     []storeOp
	failAt  int // 1-based number of the call that fails; 0 = never
	calls   int
	onEvery func() // runs inside every call, outside fakeStore's lock
}

var _ Store = (*fakeStore)(nil)

func (f *fakeStore) add(op storeOp) error {
	if f.onEvery != nil {
		f.onEvery()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls == f.failAt {
		return errors.New("disk full")
	}
	f.ops = append(f.ops, op)
	return nil
}

func (f *fakeStore) Message(m chat.Message) error { return f.add(storeOp{kind: "message", msg: m}) }
func (f *fakeStore) Patch(ps []chat.Patch) error  { return f.add(storeOp{kind: "patch", patch: ps}) }
func (f *fakeStore) Clear() error                 { return f.add(storeOp{kind: "clear"}) }
func (f *fakeStore) Compact(from, to int, s chat.Message) error {
	return f.add(storeOp{kind: "compact", from: from, to: to, msg: s})
}
func (f *fakeStore) Repair(ms []chat.Message) error { return f.add(storeOp{kind: "repair", msgs: ms}) }
func (f *fakeStore) Flush() error                   { return f.add(storeOp{kind: "flush"}) }
func (f *fakeStore) Thinking(on bool) error         { return f.add(storeOp{kind: "thinking", on: on}) }
func (f *fakeStore) Model(name string) error        { return f.add(storeOp{kind: "model", model: name}) }

func (f *fakeStore) all() []storeOp {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]storeOp(nil), f.ops...)
}

func (f *fakeStore) kinds() (out []string) {
	for _, op := range f.all() {
		out = append(out, op.kind)
	}
	return out
}

// messages returns the Seq of every stored message, in call order.
func (f *fakeStore) seqs() (out []int) {
	for _, op := range f.all() {
		if op.kind == "message" {
			out = append(out, op.msg.Seq)
		}
	}
	return out
}

func (f *fakeStore) count(kind string) (n int) {
	for _, k := range f.kinds() {
		if k == kind {
			n++
		}
	}
	return n
}

// traceRec is one record the fakeTrace saw; D is the payload as JSON would show it.
type traceRec struct {
	Kind string
	D    map[string]any
}

type fakeTrace struct {
	mu      sync.Mutex
	recs    []traceRec
	onEvery func()
	err     error
}

var _ Trace = (*fakeTrace)(nil)

func (f *fakeTrace) Record(kind string, v any) {
	if f.onEvery != nil {
		f.onEvery()
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("trace payload of %s is not JSON: %v", kind, err))
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		panic(err)
	}
	f.mu.Lock()
	f.recs = append(f.recs, traceRec{kind, d})
	f.mu.Unlock()
}

func (f *fakeTrace) Err() error { return f.err }

func (f *fakeTrace) of(kind string) (out []traceRec) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.recs {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeTrace) kinds() (out []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.recs {
		out = append(out, r.Kind)
	}
	return out
}

// withStore plugs the fakes into a rig.
func withStore(s *fakeStore, tr *fakeTrace) func(*Deps) {
	return func(d *Deps) {
		if s != nil {
			d.Store = s
		}
		if tr != nil {
			d.Trace = tr
		}
	}
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

func TestStore_CalledInHistoryOrder(t *testing.T) {
	et := newEconTools()
	st := &fakeStore{}
	r := econRig(t, et, withStore(st, nil), func(d *Deps) { d.Approve = nil })
	script(t, r, "go", [][]chat.ToolCall{{readCall(1, "a.go"), readCall(2, "b.go")}, {editCall(3, "a.go")}}, "done")

	var want []int
	for _, m := range r.a.History() {
		want = append(want, m.Seq)
	}
	if got := st.seqs(); !reflect.DeepEqual(got, want) {
		t.Errorf("stored seqs %v, history seqs %v", got, want)
	}
	// What was stored is what was appended: roles and call ids in the same order.
	var roles []string
	for _, op := range st.all() {
		if op.kind == "message" {
			roles = append(roles, string(op.msg.Role)+op.msg.ToolCallID)
		}
	}
	if got := strings.Join(roles, ","); got != "user,assistant,toolc1,toolc2,assistant,toolc3,assistant" {
		t.Errorf("stored order %s", got)
	}
}

func TestStore_NotUnderLock(t *testing.T) {
	st := &fakeStore{}
	r := startRig(t, withStore(st, nil))
	st.onEvery = func() { r.a.History(); r.a.Busy() } // would deadlock if mu were held
	tr := &fakeTrace{onEvery: func() { r.a.History() }}
	r.a.deps.Trace = tr
	r.a.Submit("hi")
	r.p.next(t).reply("hello")
	wait(t, r.a)
	if st.count("message") != 2 || len(tr.of("request")) != 1 {
		t.Errorf("ops %v trace %v", st.kinds(), tr.kinds())
	}
}

func TestStore_FlushAtTurnEnd(t *testing.T) {
	st := &fakeStore{}
	r := startRig(t, withStore(st, nil))
	for i := 0; i < 2; i++ {
		r.a.Submit("q")
		r.p.next(t).reply("a")
		wait(t, r.a)
	}
	want := []string{"message", "message", "flush", "message", "message", "flush"}
	if got := st.kinds(); !reflect.DeepEqual(got, want) {
		t.Errorf("ops %v, want %v", got, want)
	}
}

func TestStore_FlushAlsoAfterAnErrorTurn(t *testing.T) {
	st := &fakeStore{}
	r := startRig(t, withStore(st, nil))
	r.p.failNext = []error{errors.New("boom")}
	r.a.Submit("q")
	wait(t, r.a)
	if got := st.kinds(); !reflect.DeepEqual(got, []string{"message", "flush"}) {
		t.Errorf("ops %v", got)
	}
}

func TestStore_ClearRecorded(t *testing.T) {
	st := &fakeStore{}
	r := startRig(t, withStore(st, nil))
	r.a.Submit("q")
	r.p.next(t).reply("a")
	wait(t, r.a)
	if err := r.a.Clear(); err != nil {
		t.Fatal(err)
	}
	if got := st.kinds(); got[len(got)-1] != "clear" || st.count("clear") != 1 {
		t.Errorf("ops %v", got)
	}
}

func TestStore_RefusedClearIsNotRecorded(t *testing.T) {
	st := &fakeStore{}
	r := startRig(t, withStore(st, nil))
	r.a.Submit("q")
	c := r.p.next(t)
	if err := r.a.Clear(); !errors.Is(err, ErrBusy) {
		t.Fatalf("Clear = %v", err)
	}
	c.reply("a")
	wait(t, r.a)
	if st.count("clear") != 0 {
		t.Errorf("ops %v", st.kinds())
	}
}

func TestStore_FailureIsOneWarningAndTheTurnGoesOn(t *testing.T) {
	st := &fakeStore{failAt: 2}
	r := startRig(t, withStore(st, nil))
	for i := 0; i < 2; i++ {
		r.a.Submit("q")
		r.p.next(t).reply("answer")
		wait(t, r.a)
	}
	ws := r.rec.texts(EvWarning)
	if len(ws) != 1 || !strings.Contains(ws[0], "disk full") {
		t.Errorf("warnings %q, want exactly one naming the error", ws)
	}
	if len(r.a.History()) != 4 || len(r.rec.of(EvTurnEnded)) != 2 {
		t.Errorf("history %d messages, %d TurnEnded", len(r.a.History()), len(r.rec.of(EvTurnEnded)))
	}
	if st.calls != 2 { // the Store is not called again after its first failure
		t.Errorf("the Store was called %d times, want 2", st.calls)
	}
	// TurnEnded stays the last event of a Turn even when the Flush warning joins it.
	evs := r.rec.all()
	if evs[len(evs)-1].Kind != EvTurnEnded {
		t.Errorf("last event %v", evs[len(evs)-1].Kind)
	}
}

func TestStore_FlushFailureWarnsBeforeTurnEnded(t *testing.T) {
	st := &fakeStore{failAt: 3} // user, assistant, flush
	r := startRig(t, withStore(st, nil))
	r.a.Submit("q")
	r.p.next(t).reply("a")
	wait(t, r.a)
	evs := r.rec.all()
	warnAt := -1
	for i, e := range evs {
		if e.Kind == EvWarning {
			warnAt = i
		}
	}
	if evs[len(evs)-1].Kind != EvTurnEnded || warnAt < 0 {
		t.Errorf("want a Warning and TurnEnded last: %v", evs)
	}
	if len(r.rec.texts(EvWarning)) != 1 {
		t.Errorf("warnings %q", r.rec.texts(EvWarning))
	}
}

func TestStore_NilStoreAndTraceRunAToolTurn(t *testing.T) {
	et := newEconTools()
	r := econRig(t, et, func(d *Deps) { d.Approve = nil })
	script(t, r, "go", [][]chat.ToolCall{{readCall(1, "a.go")}}, "done")
	if len(r.a.History()) != 4 {
		t.Errorf("history %d", len(r.a.History()))
	}
}

func TestStore_InterruptedMessageStored(t *testing.T) {
	st := &fakeStore{}
	r := startRig(t, withStore(st, nil))
	r.a.Submit("q")
	c := r.p.next(t)
	c.say("part")
	r.rec.until(t, EvDelta, 1)
	r.a.Interrupt()
	c.reply("rest")
	wait(t, r.a)
	var got chat.Message
	for _, op := range st.all() {
		if op.kind == "message" && op.msg.Role == chat.RoleAssistant {
			got = op.msg
		}
	}
	if got.Content != "part" || !got.Interrupted {
		t.Errorf("stored %+v", got)
	}
}

func TestStore_StubAndShrinkPatchesCarryTheSeqs(t *testing.T) {
	et := newEconTools()
	et.out["c1"] = big(400)
	st := &fakeStore{}
	r := econRig(t, et, withStore(st, nil), func(d *Deps) { d.Approve = nil })
	write := toolCall(2, "write_file", fmt.Sprintf(`{"path":"w.go","content":%q}`, big(2000)))
	script(t, r, "go", [][]chat.ToolCall{{readCall(1, "a.go")}, {write}}, "done")

	h := r.a.History()
	var patches []chat.Patch
	for _, op := range st.all() {
		if op.kind == "patch" {
			patches = append(patches, op.patch...)
		}
	}
	if st.count("patch") != 1 {
		t.Fatalf("patch records = %d (%v), want one batch", st.count("patch"), st.kinds())
	}
	var stubSeq, argSeq int
	for _, m := range h {
		if m.Stub != "" {
			stubSeq = m.Seq
		}
		if m.Role == chat.RoleAssistant && len(m.ToolCalls) > 0 && m.ToolCalls[0].Name == "write_file" {
			argSeq = m.Seq
		}
	}
	if stubSeq == 0 || argSeq == 0 {
		t.Fatalf("history has no stub or shrunk call: %+v", h)
	}
	for _, p := range patches {
		switch p.Seq {
		case stubSeq:
			if p.Stub == "" {
				t.Errorf("patch %+v has no stub", p)
			}
		case argSeq:
			if p.Args["c2"] == "" || !strings.Contains(p.Args["c2"], "omitted from history") {
				t.Errorf("patch %+v has no shrunk args", p)
			}
		default:
			t.Errorf("patch for unexpected seq %d", p.Seq)
		}
	}
	// The patch comes after every message of the Turn and before the Flush.
	k := st.kinds()
	if k[len(k)-2] != "patch" || k[len(k)-1] != "flush" {
		t.Errorf("ops %v", k)
	}
}

func TestStore_InjectBatchRecordsItsPatchBeforeTheInjectedMessage(t *testing.T) {
	et := newEconTools()
	for _, id := range []string{"c1", "c2"} {
		et.out[id] = big(300)
	}
	st := &fakeStore{}
	r := econRig(t, et, withStore(st, nil), func(d *Deps) { d.Approve = nil })
	et.onRun = func(id string) {
		if id == "c2" {
			r.a.Submit("also")
		}
	}
	script(t, r, "go", [][]chat.ToolCall{{shellCall(1)}, {shellCall(2)}}, "done")
	k := st.kinds()
	pi, ui := -1, -1
	for i, op := range st.all() {
		if op.kind == "patch" && pi < 0 {
			pi = i
		}
		if op.kind == "message" && op.msg.Injected {
			ui = i
		}
	}
	if pi < 0 || ui < 0 || ui > pi {
		t.Errorf("want the injected message then the inject patch, got %v", k)
	}
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

func savedHistory() []chat.Message {
	return []chat.Message{
		{Role: chat.RoleUser, Content: "first", Seq: 1, TurnStart: true},
		{Role: chat.RoleAssistant, Content: "", Seq: 2, ToolCalls: []chat.ToolCall{{ID: "call_7", Name: "read_file", Args: `{"path":"a"}`}}},
		{Role: chat.RoleTool, Content: "text", Seq: 3, ToolCallID: "call_7", Name: "read_file", Brief: "1 line", Stub: "[read a]"},
		{Role: chat.RoleAssistant, Content: "done", Seq: 4},
	}
}

func TestLoad_ReplacesHistoryAndResetsMeter(t *testing.T) {
	st := &fakeStore{}
	r := startRig(t, withStore(st, nil))
	r.a.Submit("old")
	c := r.p.next(t)
	c.usage(500, 20)
	c.reply("old answer")
	wait(t, r.a)
	r.rec.until(t, EvMeter, 1)
	before := len(r.rec.of(EvMeter))
	nOps := len(st.all())

	h := savedHistory()
	if err := r.a.Load(h); err != nil {
		t.Fatal(err)
	}
	wait(t, r.a)
	if got := r.a.History(); !reflect.DeepEqual(got, h) {
		t.Errorf("history %+v, want %+v", got, h)
	}
	ms := r.rec.of(EvMeter)
	if len(ms) != before+1 || !ms[len(ms)-1].Estimated {
		t.Errorf("want one new estimated Meter event, got %d new, last %+v", len(ms)-before, ms[len(ms)-1])
	}
	if len(st.all()) != nOps {
		t.Errorf("Load stored something: %v", st.kinds()[nOps:])
	}
	h[0].Content = "mutated" // the Agent keeps its own copy
	if r.a.History()[0].Content != "first" {
		t.Error("Load kept the caller's slice")
	}
}

func TestLoad_DuringATurnWaitsForIt(t *testing.T) {
	r := startRig(t)
	r.a.Submit("running")
	c := r.p.next(t)
	h := savedHistory()
	if err := r.a.Load(h); err != nil {
		t.Fatalf("Load during a Turn = %v, want it queued", err)
	}
	r.a.Submit("after the load")
	if got := r.a.History(); len(got) != 1 || got[0].Content != "running" {
		t.Fatalf("history changed while the Turn runs: %+v", got)
	}
	c.reply("answer")
	c2 := r.p.next(t) // the queued text starts its Turn after the Load
	if !isPrefix(plainMsgs(h), plainMsgs(c2.req.Messages[1:])) {
		t.Errorf("the next request does not start with the loaded history: %+v", c2.req.Messages)
	}
	last := c2.req.Messages[len(c2.req.Messages)-1]
	if last.Role != chat.RoleUser || last.Content != "after the load" {
		t.Errorf("last message %+v", last)
	}
	c2.reply("ok")
	wait(t, r.a)
}

func plainMsgs(ms []chat.Message) []chat.Message {
	out := plain(ms)
	for i := range out {
		out[i].Stub = ""
		if out[i].Role == chat.RoleTool {
			out[i].Content = "" // the request carries the Stub in its place
		}
	}
	return out
}

func TestLoad_TextQueuedBeforeALoadJoinsTheTurnTextAfterDoesNot(t *testing.T) {
	et := newEconTools()
	r := econRig(t, et, func(d *Deps) { d.Approve = nil })
	et.onRun = func(id string) {
		if id == "c1" {
			r.a.Submit("before")
			_ = r.a.Load(savedHistory())
			r.a.Submit("behind the load")
		}
	}
	r.a.Submit("go")
	r.p.next(t).toolStep("", readCall(1, "a.go"))
	c2 := r.p.next(t)
	var texts []string
	for _, m := range c2.req.Messages {
		if m.Role == chat.RoleUser {
			texts = append(texts, m.Content)
		}
	}
	if strings.Contains(strings.Join(texts, "|"), "behind the load") {
		t.Errorf("a text behind the Load was injected: %q", texts)
	}
	if !strings.Contains(strings.Join(texts, "|"), "before") {
		t.Errorf("the text before the Load was not injected: %q", texts)
	}
	c2.reply("done")
	c3 := r.p.next(t)
	if got := c3.req.Messages[len(c3.req.Messages)-1].Content; got != "behind the load" {
		t.Errorf("last message %q", got)
	}
	if first := c3.req.Messages[1].Content; first != "first" {
		t.Errorf("history did not start with the loaded one: %q", first)
	}
	c3.reply("ok")
	wait(t, r.a)
}

func TestLoad_RepairsDanglingCallsAndRecordsExactlyThose(t *testing.T) {
	st := &fakeStore{}
	r := startRig(t, withStore(st, nil))
	h := []chat.Message{
		{Role: chat.RoleUser, Content: "go", Seq: 1, TurnStart: true},
		{Role: chat.RoleAssistant, Seq: 2, ToolCalls: []chat.ToolCall{
			{ID: "c1", Name: "read_file", Args: `{}`}, {ID: "c2", Name: "shell", Args: `{}`}}},
		{Role: chat.RoleTool, Seq: 3, ToolCallID: "c1", Name: "read_file", Content: "x"},
	}
	if err := r.a.Load(h, WithMaxSeq(9)); err != nil {
		t.Fatal(err)
	}
	wait(t, r.a)
	got := r.a.History()
	assertHistoryValid(t, got)
	if len(got) != 4 {
		t.Fatalf("history %+v", got)
	}
	rep := got[3]
	if rep.Content != "cancelled by the user" || rep.ToolCallID != "c2" || rep.Name != "shell" || rep.Seq != 10 {
		t.Errorf("repair message %+v", rep)
	}
	ops := st.all()
	if len(ops) != 1 || ops[0].kind != "repair" || !reflect.DeepEqual(ops[0].msgs, []chat.Message{rep}) {
		t.Errorf("Store saw %+v, want one repair with the live message", ops)
	}
}

func TestLoad_RestoresSeqAndCallSeq(t *testing.T) {
	st := &fakeStore{}
	r := econRig(t, newEconTools(), withStore(st, nil))
	if err := r.a.Load(savedHistory()); err != nil { // highest Seq 4, highest call id call_7
		t.Fatal(err)
	}
	r.a.Submit("next")
	c := r.p.next(t)
	c.endWith(chat.StreamEvent{Kind: chat.EventDone, Finish: chat.FinishToolCalls,
		ToolCalls: []chat.ToolCall{{Name: "read_file", Args: `{}`}}}) // no id: the Agent names it
	r.p.next(t).reply("ok")
	wait(t, r.a)
	h := r.a.History()
	if h[4].Seq != 5 || h[4].Content != "next" {
		t.Errorf("first new message %+v, want Seq 5", h[4])
	}
	if got := h[5].ToolCalls[0].ID; got != "call_8" {
		t.Errorf("generated id %q, want call_8", got)
	}
}

func TestLoad_WithMaxSeqKeepsNumberingAboveTheFile(t *testing.T) {
	r := startRig(t)
	if err := r.a.Load(savedHistory(), WithMaxSeq(40)); err != nil {
		t.Fatal(err)
	}
	r.a.Submit("next")
	r.p.next(t).reply("ok")
	wait(t, r.a)
	if h := r.a.History(); h[4].Seq != 41 {
		t.Errorf("Seq %d, want 41", h[4].Seq)
	}
}

func TestLoad_NumbersMessagesWithoutSeq(t *testing.T) {
	r := startRig(t)
	h := []chat.Message{{Role: chat.RoleUser, Content: "a", Seq: 5}, {Role: chat.RoleAssistant, Content: "b"}}
	if err := r.a.Load(h); err != nil {
		t.Fatal(err)
	}
	wait(t, r.a)
	got := r.a.History()
	if got[0].Seq != 5 || got[1].Seq != 6 {
		t.Errorf("seqs %d %d", got[0].Seq, got[1].Seq)
	}
}

func TestLoad_ThenSubmitContinuesWithTheLoadedPrefix(t *testing.T) {
	r := startRig(t)
	h := savedHistory()
	if err := r.a.Load(h); err != nil {
		t.Fatal(err)
	}
	r.a.Submit("again")
	c := r.p.next(t)
	msgs := c.req.Messages
	if len(msgs) != 6 || msgs[1].Content != "first" || msgs[3].Content != "[read a]" || msgs[5].Content != "again" {
		t.Errorf("request %+v", msgs)
	}
	c.reply("ok")
	wait(t, r.a)
}

func TestLoad_AfterTheWorkerStoppedIsAnError(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.a.Start(ctx)
	cancel()
	timeout := time.After(deadline)
	for { // the worker marks itself stopped asynchronously
		err := r.a.Load(savedHistory())
		if errors.Is(err, ErrStopped) {
			return
		}
		select {
		case <-timeout:
			t.Fatalf("Load = %v, want ErrStopped", err)
		default:
			runtime.Gosched()
		}
	}
}
