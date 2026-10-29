package agent

import (
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// Thinking mode (R-60, I-30)
// ---------------------------------------------------------------------------

func thinkRig(t *testing.T, mod ...func(*Deps)) *rig {
	return startRig(t, append([]func(*Deps){func(d *Deps) {
		d.MaxTokens = 100
		d.Extra = map[string]any{"temperature": 0.7, "chat_template_kwargs": map[string]any{"enable_thinking": false, "x": 1}}
		d.ThinkExtra = map[string]any{"temperature": 0.6, "chat_template_kwargs": map[string]any{"enable_thinking": true}}
		d.ThinkTokens = 900
	}}, mod...)...)
}

func TestSetThinking_RequestUsesTheOverlay(t *testing.T) {
	r := thinkRig(t)
	r.a.SetThinking(true)
	r.a.Submit("hi")
	c := r.p.next(t)
	c.reply("ok")
	wait(t, r.a)
	want := map[string]any{"temperature": 0.6, "chat_template_kwargs": map[string]any{"enable_thinking": true, "x": 1}}
	if !reflect.DeepEqual(c.req.Extra, want) || c.req.MaxTokens != 900 {
		t.Errorf("extra %v tokens %d, want %v 900", c.req.Extra, c.req.MaxTokens, want)
	}
	if !r.a.Thinking() {
		t.Error("Thinking() = false")
	}
}

func TestSetThinking_KeepsALargerBaseLimit(t *testing.T) {
	r := thinkRig(t, func(d *Deps) { d.MaxTokens = 16384 })
	r.a.SetThinking(true)
	r.a.Submit("hi")
	c := r.p.next(t)
	c.reply("ok")
	wait(t, r.a)
	if c.req.MaxTokens != 16384 {
		t.Errorf("tokens %d, want the larger base limit 16384", c.req.MaxTokens)
	}
}

func TestSetThinking_OffKeepsTheBaseRequest(t *testing.T) {
	r := thinkRig(t)
	r.a.SetThinking(true)
	r.a.SetThinking(false)
	r.a.Submit("hi")
	c := r.p.next(t)
	c.reply("ok")
	wait(t, r.a)
	if c.req.Extra["temperature"] != 0.7 || c.req.MaxTokens != 100 {
		t.Errorf("extra %v tokens %d, want the base request", c.req.Extra, c.req.MaxTokens)
	}
}

func TestSetThinking_DoesNotChangeTheBaseMap(t *testing.T) {
	r := thinkRig(t)
	r.a.SetThinking(true)
	r.a.Submit("hi")
	r.p.next(t).reply("ok")
	wait(t, r.a)
	if r.a.deps.Extra["temperature"] != 0.7 || r.a.deps.Extra["chat_template_kwargs"].(map[string]any)["enable_thinking"] != false {
		t.Errorf("Deps.Extra was modified: %v", r.a.deps.Extra)
	}
}

func TestSetThinking_RecordsTracesAndAnnounces(t *testing.T) {
	st, tr := &fakeStore{}, &fakeTrace{}
	r := newRig(t, func(d *Deps) { d.Store, d.Trace = st, tr })
	r.a.SetThinking(true)
	r.a.SetThinking(true) // same mode: nothing
	ops := st.all()
	if len(ops) != 1 || ops[0].kind != "thinking" || !ops[0].on {
		t.Errorf("store ops %+v, want one thinking op", ops)
	}
	if recs := tr.of("thinking"); len(recs) != 1 || recs[0].D["on"] != true {
		t.Errorf("trace %+v", recs)
	}
	if got := r.rec.texts(EvThinking); len(got) != 1 || got[0] != "on" {
		t.Errorf("EvThinking texts %q", got)
	}
}

func TestLoad_WithThinkingSetsTheModeWithoutRecording(t *testing.T) {
	st := &fakeStore{}
	r := startRig(t, func(d *Deps) { d.Store = st })
	r.a.SetThinking(true)
	if err := r.a.Load(nil, WithThinking(false)); err != nil {
		t.Fatal(err)
	}
	r.a.Wait()
	if r.a.Thinking() {
		t.Error("still thinking after Load WithThinking(false)")
	}
	if got := r.rec.texts(EvThinking); len(got) != 2 || got[1] != "off" {
		t.Errorf("EvThinking texts %q", got)
	}
	for _, op := range st.all() {
		if op.kind == "thinking" && !op.on {
			t.Errorf("Load recorded the thinking mode: %+v", st.all())
		}
	}
	if err := r.a.Load(nil); err != nil { // no option keeps the mode
		t.Fatal(err)
	}
	r.a.Wait()
}

func TestMergeExtra_NilsAndNesting(t *testing.T) {
	if got := mergeExtra(nil, nil); got == nil || len(got) != 0 {
		t.Errorf("mergeExtra(nil, nil) = %v", got)
	}
	got := mergeExtra(nil, map[string]any{"a": 1})
	if !reflect.DeepEqual(got, map[string]any{"a": 1}) {
		t.Errorf("got %v", got)
	}
}
