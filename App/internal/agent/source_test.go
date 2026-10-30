package agent

import (
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Source switch (R-100, I-36)
// ---------------------------------------------------------------------------

// A switch while a request streams leaves it alone; the next request goes to the new Provider
// with the Source's model, output limit and presets.
func TestSetSource_NextRequestUsesTheNewProviderAndModel(t *testing.T) {
	r := startRig(t)
	r.a.Submit("one")
	c := r.p.next(t)
	p2 := newProvider()
	err := r.a.SetSource(Source{Name: "cloud", Provider: p2, Model: "m2", MaxTokens: 77, ContextLimit: 1000,
		Extra: map[string]any{"k": 1}})
	if err != nil {
		t.Fatal(err)
	}
	c.reply("a") // the old request ends on the old Provider
	wait(t, r.a)
	r.a.Submit("two")
	c2 := p2.next(t)
	if c2.req.Model != "m2" || c2.req.MaxTokens != 77 || !reflect.DeepEqual(c2.req.Extra, map[string]any{"k": 1}) {
		t.Errorf("request to the new source %+v", c2.req)
	}
	c2.reply("b")
	wait(t, r.a)
	select {
	case c := <-r.p.calls:
		t.Errorf("the old provider got a request after the switch: %+v", c.req)
	default:
	}
	if got := r.a.Source(); got.Name != "cloud" || r.a.Model() != "m2" {
		t.Errorf("Source %q model %q", got.Name, r.a.Model())
	}
}

func TestSetSource_RecordsProviderAndModelAndEmits(t *testing.T) {
	st, tr := &fakeStore{}, &fakeTrace{}
	r := newRig(t, func(d *Deps) { d.Store, d.Trace = st, tr })
	err := r.a.SetSource(Source{Name: "cloud", Provider: newProvider(), Model: "m2", ContextLimit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(st.kinds(), ","); got != "provider,model" {
		t.Errorf("store ops %s, want provider,model", got)
	}
	if recs := tr.of("provider"); len(recs) != 1 || recs[0].D["provider"] != "cloud" || recs[0].D["model"] != "m2" {
		t.Errorf("trace %+v", recs)
	}
	if got := r.rec.texts(EvProvider); len(got) != 1 || got[0] != "cloud" {
		t.Errorf("EvProvider %q", got)
	}
	if got := r.rec.texts(EvModel); len(got) != 1 || got[0] != "m2" {
		t.Errorf("EvModel %q", got)
	}
	if ms := r.rec.of(EvMeter); len(ms) != 1 || ms[0].Limit != 500 {
		t.Errorf("EvMeter %+v, want the new limit 500", ms)
	}
	for _, bad := range []Source{{Provider: newProvider(), Model: "m"}, {Name: "x", Model: "m"}, {Name: "x", Provider: newProvider()}} {
		if err := r.a.SetSource(bad); err == nil {
			t.Errorf("SetSource(%+v) accepted", bad)
		}
	}
}

// A history larger than the new Source's limit is compacted (on the new Provider) before the
// next Turn, which then fits.
func TestSetSource_OverLimitQueuesCompactionFirst(t *testing.T) {
	et := newEconTools()
	et.out["c1"] = strings.Repeat("h", 3000)
	r := econRig(t, et, func(d *Deps) { d.Approve = nil; d.ContextLimit = 20000 })
	r.a.Submit("go")
	r.p.next(t).toolStep(noteText(1, 3000), readCall(1, "a.go"))
	r.p.next(t).reply("ok")
	wait(t, r.a)
	p2 := newProvider()
	if err := r.a.SetSource(Source{Name: "small", Provider: p2, Model: "s", ContextLimit: 300}); err != nil {
		t.Fatal(err)
	}
	r.a.Submit("two")
	c := p2.next(t)
	if !isSummaryRequest(c.req) {
		t.Fatalf("first request to the new source is not the summary: %+v", c.req)
	}
	c.reply("did the work")
	c2 := p2.next(t)
	for i := 0; isSummaryRequest(c2.req) && i < 5; i++ { // a small limit chunks the summary
		c2.reply("did the work")
		c2 = p2.next(t)
	}
	if isSummaryRequest(c2.req) || c2.req.Model != "s" {
		t.Errorf("request after the summaries %q, want the Turn on model s", c2.req.Model)
	}
	c2.reply("b")
	wait(t, r.a)
	if len(r.rec.of(EvCompacted)) != 1 {
		t.Error("no EvCompacted")
	}
}

// The thinking overlay and output limit come from the Source in use.
func TestSetSource_ThinkingBodyFollowsTheSource(t *testing.T) {
	r := thinkRig(t)
	r.a.SetThinking(true)
	p2 := newProvider()
	think := map[string]any{"reasoning": map[string]any{"effort": "high"}}
	if err := r.a.SetSource(Source{Name: "or", Provider: p2, Model: "m2", MaxTokens: 50, ContextLimit: 1000, ThinkExtra: think}); err != nil {
		t.Fatal(err)
	}
	r.a.Submit("hi")
	c := p2.next(t)
	c.reply("ok")
	wait(t, r.a)
	if !reflect.DeepEqual(c.req.Extra, think) || c.req.MaxTokens != 50 {
		t.Errorf("extra %v tokens %d, want %v 50", c.req.Extra, c.req.MaxTokens, think)
	}
}
