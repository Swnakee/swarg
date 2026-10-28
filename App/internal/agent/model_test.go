package agent

import (
	"errors"
	"strings"
	"testing"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Model choice (R-30, I-9)
// ---------------------------------------------------------------------------

func TestSetModel_ChangeRecordsTracesAndAnnounces(t *testing.T) {
	st, tr := &fakeStore{}, &fakeTrace{}
	r := newRig(t, func(d *Deps) { d.Store, d.Trace = st, tr })
	if err := r.a.SetModel("  big  "); err != nil {
		t.Fatal(err)
	}
	if got := r.a.Model(); got != "big" {
		t.Errorf("Model() = %q, want the trimmed id", got)
	}
	ops := st.all()
	if len(ops) != 1 || ops[0].kind != "model" || ops[0].model != "big" {
		t.Errorf("store ops %+v, want one model op", ops)
	}
	if recs := tr.of("model"); len(recs) != 1 || recs[0].D["model"] != "big" {
		t.Errorf("trace %+v", recs)
	}
	if got := r.rec.texts(EvModel); len(got) != 1 || got[0] != "big" {
		t.Errorf("EvModel texts %q", got)
	}
}

func TestSetModel_SameModelDoesNothing(t *testing.T) {
	st := &fakeStore{}
	r := newRig(t, func(d *Deps) { d.Store = st })
	if err := r.a.SetModel("m"); err != nil {
		t.Fatal(err)
	}
	if len(st.all()) != 0 || len(r.rec.of(EvModel)) != 0 {
		t.Errorf("store %+v, events %+v; want nothing", st.all(), r.rec.of(EvModel))
	}
}

func TestSetModel_BlankIsRefused(t *testing.T) {
	st := &fakeStore{}
	r := newRig(t, func(d *Deps) { d.Store = st })
	for _, name := range []string{"", "  ", "\t\n"} {
		if err := r.a.SetModel(name); !errors.Is(err, ErrBlankModel) {
			t.Errorf("SetModel(%q) = %v", name, err)
		}
	}
	if r.a.Model() != "m" || len(st.all()) != 0 {
		t.Errorf("model %q, store %+v", r.a.Model(), st.all())
	}
}

func TestSetModel_NextRequestUsesIt(t *testing.T) {
	r := startRig(t)
	r.a.Submit("one")
	c := r.p.next(t)
	if c.req.Model != "m" {
		t.Errorf("first request model %q", c.req.Model)
	}
	if err := r.a.SetModel("big"); err != nil { // while the request streams
		t.Fatal(err)
	}
	c.reply("a")
	wait(t, r.a)
	r.a.Submit("two")
	if c := r.p.next(t); c.req.Model != "big" {
		t.Errorf("next request model %q, want big", c.req.Model)
	} else {
		c.reply("b")
	}
	wait(t, r.a)
}

func TestSetModel_RecordedInHistoryOrder(t *testing.T) {
	st := &fakeStore{}
	r := startRig(t, func(d *Deps) { d.Store = st })
	r.a.Submit("one")
	r.p.next(t).reply("a")
	wait(t, r.a)
	if err := r.a.SetModel("big"); err != nil {
		t.Fatal(err)
	}
	r.a.Submit("two")
	r.p.next(t).reply("b")
	wait(t, r.a)
	got := strings.Join(st.kinds(), ",")
	if want := "message,message,flush,model,message,message,flush"; got != want {
		t.Errorf("store ops %s, want %s", got, want)
	}
}

func TestLoadWithModel_SetsWithoutRecording(t *testing.T) {
	st := &fakeStore{}
	r := startRig(t, func(d *Deps) { d.Store = st })
	h := []chat.Message{{Role: chat.RoleUser, Content: "q", Seq: 1}, {Role: chat.RoleAssistant, Content: "a", Seq: 2}}
	if err := r.a.Load(h, WithModel("saved")); err != nil {
		t.Fatal(err)
	}
	wait(t, r.a)
	if r.a.Model() != "saved" || st.count("model") != 0 {
		t.Errorf("model %q, model ops %d", r.a.Model(), st.count("model"))
	}
	if got := r.rec.texts(EvModel); len(got) != 1 || got[0] != "saved" {
		t.Errorf("EvModel %q", got)
	}
	r.a.Submit("again")
	if c := r.p.next(t); c.req.Model != "saved" {
		t.Errorf("request model %q", c.req.Model)
	} else {
		c.reply("x")
	}
	wait(t, r.a)
}

func TestLoadWithModel_SameOrBlankKeepsTheModel(t *testing.T) {
	for _, name := range []string{"", "m"} {
		r := startRig(t)
		if err := r.a.Load(nil, WithModel(name)); err != nil {
			t.Fatal(err)
		}
		wait(t, r.a)
		if r.a.Model() != "m" || len(r.rec.of(EvModel)) != 0 {
			t.Errorf("%q: model %q, EvModel %d", name, r.a.Model(), len(r.rec.of(EvModel)))
		}
	}
}

func TestCompact_SummaryUsesChosenModel(t *testing.T) {
	et := newEconTools()
	et.out["c1"] = strings.Repeat("h", 3000)
	r := econRig(t, et, func(d *Deps) { d.Approve = nil; d.ContextLimit = 20000 })
	r.a.Submit("go")
	r.p.next(t).toolStep(noteText(1, 3000), readCall(1, "a.go"))
	r.p.next(t).reply("ok")
	wait(t, r.a)
	if err := r.a.SetModel("big"); err != nil {
		t.Fatal(err)
	}
	mustCompact(t, r.a)
	c := nextSummary(t, r)
	if c.req.Model != "big" {
		t.Errorf("summary request model %q, want big", c.req.Model)
	}
	c.reply("did the work")
	wait(t, r.a)
}

// F3 (agent-2's code review): a Store error switched saving off for the Agent's life, so after
// the app switched to another SessionFile (Load) nothing was saved any more, silently.
func TestStore_LoadRearmsSavingAfterAnError(t *testing.T) {
	st := &fakeStore{failAt: 1}
	r := startRig(t, func(d *Deps) { d.Store = st })
	r.a.Submit("one")
	r.p.next(t).reply("a")
	wait(t, r.a)
	if n := len(r.rec.texts(EvWarning)); n != 1 {
		t.Fatalf("warnings %q, want the one 'no longer saved'", r.rec.texts(EvWarning))
	}
	h := []chat.Message{{Role: chat.RoleUser, Content: "old", Seq: 1}, {Role: chat.RoleAssistant, Content: "x", Seq: 2}}
	if err := r.a.Load(h); err != nil { // the app switched to another SessionFile
		t.Fatal(err)
	}
	wait(t, r.a)
	r.a.Submit("two")
	r.p.next(t).reply("b")
	wait(t, r.a)
	var got []string
	for _, op := range st.all() {
		if op.kind == "message" {
			got = append(got, op.msg.Content)
		}
	}
	if strings.Join(got, ",") != "two,b" {
		t.Errorf("messages stored after the switch %q, want two,b", got)
	}
}
