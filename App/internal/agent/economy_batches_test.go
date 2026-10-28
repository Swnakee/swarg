package agent

import (
	"fmt"
	"strings"
	"testing"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Emergency batch (ContextLimit 1000 tokens = ~4000 bytes; 65% = 2600 bytes)
// ---------------------------------------------------------------------------

// shellSteps returns n Steps of one shell call each, ids c1..cn.
func shellSteps(n int) [][]chat.ToolCall {
	var out [][]chat.ToolCall
	for i := 1; i <= n; i++ {
		out = append(out, []chat.ToolCall{shellCall(i)})
	}
	return out
}

func stubbed(req chat.Request) (out []bool) {
	for _, m := range req.Messages {
		if m.Role == chat.RoleTool {
			out = append(out, m.Stub != "")
		}
	}
	return out
}

func TestEmergency_BatchOnceWhenOver65Percent(t *testing.T) {
	et := newEconTools()
	for i := 1; i <= 6; i++ {
		et.out[fmt.Sprintf("c%d", i)] = big(1000) // 250 tokens each
	}
	r := econRig(t, et, func(d *Deps) { d.Approve = nil })
	reqs := script(t, r, "go", shellSteps(6), "done")

	// request 4 (index 3) is the first whose estimate (3 results + base) is over 65%
	if got := stubbed(reqs[3]); got[0] != true || got[1] || got[2] {
		t.Fatalf("request 4 stubs = %v, want only the oldest result stubbed (the last 2 stay)", got)
	}
	if got := stubbed(reqs[2]); got[0] || got[1] {
		t.Errorf("request 3 stubs = %v, want none yet", got)
	}
	// after the batch the Turn appends again, and no second batch fires
	for n := 4; n < len(reqs); n++ {
		if !isPrefix(reqs[n-1].Messages, reqs[n].Messages) {
			t.Errorf("request %d does not start with request %d (a second batch?)", n+1, n)
		}
	}
}

func TestEmergency_StopsAtTheTargetShare(t *testing.T) {
	et := newEconTools()
	// 6 results: 5 x 1200 bytes and the last two; after the cheapest-needed stubs the estimate
	// must fall to <= 50% and the remaining old results stay full.
	for i := 1; i <= 8; i++ {
		et.out[fmt.Sprintf("c%d", i)] = big(1000)
	}
	r := econRig(t, et, func(d *Deps) { d.ContextLimit = 2000; d.Approve = nil })
	reqs := script(t, r, "go", shellSteps(8), "done")
	var batch chat.Request
	for n := 1; n < len(reqs); n++ {
		if !isPrefix(reqs[n-1].Messages, reqs[n].Messages) {
			batch = reqs[n]
			break
		}
	}
	if batch.Messages == nil {
		t.Fatal("no emergency batch happened")
	}
	if est := estimateRequest(batch).Total(); est*100 > targetPercent*2000 {
		t.Errorf("estimate after the batch = %d tokens, want <= 50%% of 2000", est)
	}
	got := stubbed(batch)
	full := 0
	for _, s := range got {
		if !s {
			full++
		}
	}
	if full < keepRecent {
		t.Errorf("stubs = %v, want at least the last %d results full", got, keepRecent)
	}
}

func TestEmergency_NeverStubsTheLastTwoResults(t *testing.T) {
	et := newEconTools()
	et.out["c1"], et.out["c2"] = big(3000), big(3000)
	r := econRig(t, et, func(d *Deps) { d.Approve = nil })
	reqs := script(t, r, "go", shellSteps(2), "done")
	if got := stubbed(reqs[2]); got[0] || got[1] {
		t.Errorf("stubs = %v, want both of two results full though the estimate is over 65%%", got)
	}
}

func TestEmergency_UnderThresholdNoBatch(t *testing.T) {
	et := newEconTools()
	for i := 1; i <= 4; i++ {
		et.out[fmt.Sprintf("c%d", i)] = big(600) // 4*150 = 600 tokens + base < 650
	}
	r := econRig(t, et, func(d *Deps) { d.Approve = nil })
	reqs := script(t, r, "go", shellSteps(4), "done")
	for _, req := range reqs {
		for _, s := range stubbed(req) {
			if s {
				t.Fatalf("a result was stubbed at %d%%", estimateRequest(req).Total()*100/1000)
			}
		}
	}
}

func TestEmergency_StaleReadsGoFirstThenTheLargest(t *testing.T) {
	et := newEconTools()
	et.out["c1"] = big(900)  // read a.go: stale after c2 reads it again
	et.out["c2"] = big(900)  // read a.go again: newest, not stale, a read: protected
	et.out["c3"] = big(500)  // shell, small
	et.out["c4"] = big(1500) // shell, largest: goes before c3
	et.out["c5"] = big(300)
	et.out["c6"] = big(300)
	r := econRig(t, et, func(d *Deps) { d.Approve = nil })
	reqs := script(t, r, "go", [][]chat.ToolCall{{readCall(1, "a.go")}, {readCall(2, "a.go")}, {shellCall(3)}, {shellCall(4)}, {shellCall(5)}}, "done")
	// first request over 65%: after c4 (c1..c4 = 3800 bytes ~ 950 tokens)
	var batch chat.Request
	for n := 1; n < len(reqs); n++ {
		if !isPrefix(reqs[n-1].Messages, reqs[n].Messages) {
			batch = reqs[n]
			break
		}
	}
	if batch.Messages == nil {
		t.Fatal("no emergency batch")
	}
	got := stubbed(batch)
	if !got[0] {
		t.Errorf("stubs = %v, want the stale read c1 stubbed first", got)
	}
	if got[1] {
		t.Errorf("stubs = %v, the newest read must stay full (the model may still edit from it)", got)
	}
	var stale string
	for _, m := range batch.Messages {
		if m.ToolCallID == "c1" {
			stale = m.Content
		}
	}
	if !strings.Contains(stale, "stale") {
		t.Errorf("stale read sent as %q", stale)
	}
}

func TestEmergency_ReadsNotYetEditedAreKept(t *testing.T) {
	et := newEconTools()
	for i := 1; i <= 5; i++ {
		et.out[fmt.Sprintf("c%d", i)] = big(1200)
	}
	r := econRig(t, et)
	reqs := script(t, r, "go", [][]chat.ToolCall{{readCall(1, "a.go")}, {readCall(2, "b.go")}, {readCall(3, "c.go")}, {readCall(4, "d.go")}, {readCall(5, "e.go")}}, "done")
	for n, req := range reqs {
		for _, s := range stubbed(req) {
			if s {
				t.Fatalf("request %d stubbed a read that was not edited after reading", n+1)
			}
		}
	}
}

func TestEmergency_OnePerTurnAndAgainNextTurn(t *testing.T) {
	et := newEconTools()
	for i := 1; i <= 12; i++ {
		et.out[fmt.Sprintf("c%d", i)] = big(1000)
	}
	r := econRig(t, et, func(d *Deps) { d.Approve = nil })
	script(t, r, "one", shellSteps(6), "done")
	// second Turn: ids continue, results are big again
	r.a.Submit("two")
	var reqs []chat.Request
	for i := 7; i <= 12; i++ {
		c := r.p.next(t)
		reqs = append(reqs, c.req)
		c.toolStep("", shellCall(i))
	}
	c := r.p.next(t)
	reqs = append(reqs, c.req)
	c.reply("done")
	wait(t, r.a)
	batches := 0
	for n := 1; n < len(reqs); n++ {
		if !isPrefix(reqs[n-1].Messages, reqs[n].Messages) {
			batches++
		}
	}
	if batches != 1 {
		t.Errorf("second Turn had %d prefix changes, want exactly its own one emergency batch", batches)
	}
}

func TestEmergency_MeterFallsBackToTheEstimate(t *testing.T) {
	et := newEconTools()
	for i := 1; i <= 5; i++ {
		et.out[fmt.Sprintf("c%d", i)] = big(1000)
	}
	r := econRig(t, et, func(d *Deps) { d.Approve = nil })
	r.a.Submit("go")
	for i := 1; i <= 5; i++ {
		c := r.p.next(t)
		if i == 4 {
			if m := r.rec.lastMeter(t); !m.Estimated {
				t.Errorf("meter after the batch = %+v, want an estimate", m)
			}
		}
		c.usage(100*i, 10)
		c.toolStep("", shellCall(i))
	}
	r.p.next(t).reply("done")
	wait(t, r.a)
}

func TestEmergency_NoLimitNoBatch(t *testing.T) {
	et := newEconTools()
	for i := 1; i <= 5; i++ {
		et.out[fmt.Sprintf("c%d", i)] = big(1000)
	}
	r := econRig(t, et, func(d *Deps) { d.ContextLimit = 0; d.Approve = nil })
	reqs := script(t, r, "go", shellSteps(5), "done")
	for n := 1; n < len(reqs); n++ {
		if !isPrefix(reqs[n-1].Messages, reqs[n].Messages) {
			t.Fatal("a batch ran without a context limit")
		}
	}
}

// ---------------------------------------------------------------------------
// Inject batch
// ---------------------------------------------------------------------------

func TestInject_StubsEarlierResultsButNotTheLastStep(t *testing.T) {
	et := newEconTools()
	for _, id := range []string{"c1", "c2", "c3", "c4"} {
		et.out[id] = big(300)
	}
	r := econRig(t, et, func(d *Deps) { d.Approve = nil })
	et.onRun = func(id string) {
		if id == "c4" {
			r.a.Submit("also please")
		}
	}
	reqs := script(t, r, "go", [][]chat.ToolCall{{shellCall(1), shellCall(2)}, {readCall(3, "keep.go")}, {readCall(4, "x.go")}}, "done")

	last := reqs[len(reqs)-1]
	var tools []chat.Message
	var injected chat.Message
	for _, m := range last.Messages {
		switch {
		case m.Role == chat.RoleTool:
			tools = append(tools, m)
		case m.Role == chat.RoleUser && m.Injected:
			injected = m
		}
	}
	if len(tools) != 4 {
		t.Fatalf("tool messages = %d", len(tools))
	}
	if tools[0].Stub == "" || tools[1].Stub == "" {
		t.Errorf("shell results of earlier Steps must be stubbed at inject: %+v", tools[:2])
	}
	if tools[2].Stub != "" {
		t.Errorf("a read not yet edited must stay full at inject: %+v", tools[2])
	}
	if tools[3].Stub != "" {
		t.Errorf("results of the last Step have not been answered yet and must stay full: %+v", tools[3])
	}
	if !strings.Contains(injected.Content, "also please") {
		t.Errorf("injected text missing or not marked Injected: %+v", last.Messages)
	}
}

func TestInject_NothingQueuedNoBatch(t *testing.T) {
	et := newEconTools()
	et.out["c1"], et.out["c2"] = big(300), big(300)
	r := econRig(t, et, func(d *Deps) { d.Approve = nil })
	reqs := script(t, r, "go", [][]chat.ToolCall{{shellCall(1)}, {shellCall(2)}}, "done")
	for n := 1; n < len(reqs); n++ {
		if !isPrefix(reqs[n-1].Messages, reqs[n].Messages) {
			t.Fatalf("request %d is not an append", n+1)
		}
	}
}

// ---------------------------------------------------------------------------
// Bookkeeping
// ---------------------------------------------------------------------------

func TestBookkeeping_SeqTurnStartInjectedAndToolFields(t *testing.T) {
	et := newEconTools()
	et.fail["c2"] = true
	r := econRig(t, et)
	et.onRun = func(id string) {
		if id == "c1" {
			r.a.Submit("injected")
		}
	}
	script(t, r, "one", [][]chat.ToolCall{{readCall(1, "a.go")}, {readCall(2, "b.go")}}, "done")
	nextTurn(t, r, "two")

	h := r.a.History()
	for i, m := range h {
		if m.Seq != i+1 {
			t.Errorf("history[%d].Seq = %d, want %d", i, m.Seq, i+1)
		}
		if m.TurnStart != (m.Role == chat.RoleUser && (m.Content == "one" || m.Content == "two")) {
			t.Errorf("history[%d] TurnStart = %v for %q", i, m.TurnStart, m.Content)
		}
		if m.Injected != (m.Role == chat.RoleUser && m.Content == "injected") {
			t.Errorf("history[%d] Injected = %v for %q", i, m.Injected, m.Content)
		}
		if m.Role != chat.RoleTool {
			continue
		}
		if m.Label != "L:read_file" || m.Brief == "" && m.ToolCallID == "c1" {
			t.Errorf("tool message %+v lacks Label or Brief", m)
		}
		if m.Failed != (m.ToolCallID == "c2") {
			t.Errorf("tool message %s Failed = %v", m.ToolCallID, m.Failed)
		}
	}
	if err := r.a.Clear(); err != nil {
		t.Fatal(err)
	}
	nextTurn(t, r, "three")
	if h := r.a.History(); h[0].Seq <= len(h)+3 {
		t.Errorf("Seq after Clear = %d, want it to continue, never repeat", h[0].Seq)
	}
}

func TestBookkeeping_SeqIsUniqueAcrossTurnsAndClear(t *testing.T) {
	r := econRig(t, newEconTools())
	nextTurn(t, r, "a")
	first := r.a.History()
	if err := r.a.Clear(); err != nil {
		t.Fatal(err)
	}
	nextTurn(t, r, "b")
	if second := r.a.History(); second[0].Seq <= first[len(first)-1].Seq {
		t.Errorf("Seq went back: %d after %d", second[0].Seq, first[len(first)-1].Seq)
	}
}
