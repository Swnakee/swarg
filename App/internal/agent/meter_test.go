package agent

import (
	"errors"
	"strings"
	"testing"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Meter through the agent
// ---------------------------------------------------------------------------

func TestMeter_EstimateBeforeUsage(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hello")
	r.p.next(t).end(chat.FinishError, errors.New("boom"))
	wait(t, r.a)
	m := r.rec.lastMeter(t)
	if !m.Estimated || m.Used != chat.EstimateTokens("SYShello") || m.Limit != 1000 {
		t.Fatalf("meter = %+v", m)
	}
}

func TestMeter_ExactAfterUsage(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hello")
	c := r.p.next(t)
	c.usage(300, 40)
	c.reply("ok")
	wait(t, r.a)
	if m := r.rec.lastMeter(t); m.Used != 340 || m.Estimated {
		t.Fatalf("meter = %+v", m)
	}
}

func TestMeter_KeepsLastExactOnError(t *testing.T) {
	r := startRig(t)
	r.a.Submit("a")
	c := r.p.next(t)
	c.usage(300, 40)
	c.reply("ok")
	wait(t, r.a)
	r.a.Submit("b")
	r.p.next(t).end(chat.FinishError, errors.New("boom"))
	wait(t, r.a)
	if m := r.rec.lastMeter(t); m.Used != 340 || m.Estimated {
		t.Fatalf("meter = %+v", m)
	}
}

// ---------------------------------------------------------------------------
// Warnings
// ---------------------------------------------------------------------------

// R-5
func TestMeter_WarningsOncePerUpwardCrossing(t *testing.T) {
	steps := []struct {
		used int
		want int // number of warnings
	}{
		{799, 0}, {800, 1}, {801, 0}, {999, 0}, {1000, 1}, {1200, 0},
		{700, 0}, {800, 1},
	}
	m := meter{limit: 1000}
	for _, s := range steps {
		if got := m.check(s.used, false); len(got) != s.want {
			t.Fatalf("check(%d) = %q, want %d warnings", s.used, got, s.want)
		}
	}
}

// R-5
func TestMeter_JumpsStraightTo100(t *testing.T) {
	m := meter{limit: 1000}
	got := m.check(1100, false)
	want := []string{"context 80% full (110% · 1.1k/1k)", "context full (110% · 1.1k/1k): use /clear"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("warnings = %q", got)
	}
}

func TestMeter_EstimateCanWarn(t *testing.T) {
	m := meter{limit: 1000}
	got := m.check(900, true)
	if len(got) != 1 || got[0] != "context 80% full (~90% · ~900/1k)" {
		t.Fatalf("warnings = %q", got)
	}
}

// ---------------------------------------------------------------------------
// FormatMeter
// ---------------------------------------------------------------------------

func TestFormatMeter(t *testing.T) {
	cases := []struct {
		used, limit int
		est         bool
		want        string
	}{
		{0, 16000, false, "0% · 0/16k"},
		{999, 1000, false, "99% · 999/1k"},
		{1000, 16000, false, "6% · 1k/16k"},
		{1500, 16000, false, "9% · 1.5k/16k"},
		{5600, 16000, false, "35% · 5.6k/16k"},
		{16384, 16384, false, "100% · 16.4k/16.4k"},
		{20000, 16000, false, "125% · 20k/16k"},
		{5600, 16000, true, "~35% · ~5.6k/16k"},
		{10, 0, false, "0% · 10/0"},
	}
	for _, c := range cases {
		if got := FormatMeter(c.used, c.limit, c.est); got != c.want {
			t.Errorf("FormatMeter(%d,%d,%v) = %q, want %q", c.used, c.limit, c.est, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Cache share
// ---------------------------------------------------------------------------

func TestMeter_CachePercent(t *testing.T) {
	cases := []struct{ cached, prompt, want int }{
		{800, 1000, 80}, {0, 1000, 0}, {1500, 1000, 100}, {5, 0, 0}, {-3, 100, 0},
	}
	for _, c := range cases {
		if got := cachePercent(c.cached, c.prompt); got != c.want {
			t.Errorf("cachePercent(%d,%d) = %d, want %d", c.cached, c.prompt, got, c.want)
		}
	}
}

func TestMeter_EventCarriesCacheOfLastRequest(t *testing.T) {
	r := startRig(t)
	if m := r.rec.of(EvMeter); len(m) != 0 {
		t.Fatalf("unexpected meter before any turn: %v", m)
	}
	r.a.Submit("hello")
	c := r.p.next(t)
	c.ch <- chat.StreamEvent{Kind: chat.EventUsage, Usage: chat.Usage{PromptTokens: 1000, CompletionTokens: 10, CachedTokens: 800}}
	c.reply("ok")
	wait(t, r.a)
	if m := r.rec.lastMeter(t); !m.HasCache || m.CachePct != 80 {
		t.Fatalf("meter = %+v", m)
	}
	r.a.Submit("b") // an error Turn keeps the last value
	r.p.next(t).end(chat.FinishError, errors.New("boom"))
	wait(t, r.a)
	if m := r.rec.lastMeter(t); !m.HasCache || m.CachePct != 80 {
		t.Fatalf("after error: meter = %+v", m)
	}
}

func TestMeter_NoUsageNoCache(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hello")
	r.p.next(t).end(chat.FinishError, errors.New("boom"))
	wait(t, r.a)
	if m := r.rec.lastMeter(t); m.HasCache {
		t.Fatalf("meter = %+v", m)
	}
}

func TestFormatCache(t *testing.T) {
	if got := FormatCache(80, true); got != "cache 80%" {
		t.Errorf("got %q", got)
	}
	if got := FormatCache(0, false); got != "cache –" {
		t.Errorf("got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Breakdown
// ---------------------------------------------------------------------------

func breakdownRequest() chat.Request {
	return chat.Request{
		Messages: []chat.Message{
			{Role: chat.RoleSystem, Content: "system prompt"},
			{Role: chat.RoleUser, Content: "please read the file"},
			{Role: chat.RoleAssistant, Content: "ok", ToolCalls: []chat.ToolCall{{ID: "c1", Name: "read_file", Args: `{"path":"a.go"}`}}},
			{Role: chat.RoleTool, ToolCallID: "c1", Name: "read_file", Content: strings.Repeat("1: code\n", 100)},
			{Role: chat.RoleAssistant, Content: "it is a Go file"},
		},
		Tools: []chat.ToolSpec{{Name: "read_file", Description: "Read a file.", Schema: []byte(`{"type":"object"}`)}},
	}
}

func TestEstimateRequest_CategoriesSumToWholeEstimate(t *testing.T) {
	req := breakdownRequest()
	b := estimateRequest(req)
	var all strings.Builder
	for _, m := range req.Messages {
		all.WriteString(m.Content)
		for _, c := range m.ToolCalls {
			all.WriteString(c.Name + c.Args)
		}
	}
	for _, s := range req.Tools {
		all.WriteString(s.Name + s.Description + string(s.Schema))
	}
	if b.Total() != chat.EstimateTokens(all.String()) {
		t.Errorf("total %d, want %d (%+v)", b.Total(), chat.EstimateTokens(all.String()), b)
	}
	if b.System == 0 || b.User == 0 || b.Answers == 0 || b.CallArgs == 0 || b.ResultsFull == 0 || b.Stubs != 0 {
		t.Errorf("categories = %+v", b)
	}
}

func TestEstimateRequest_IncludesToolSchemas(t *testing.T) {
	with, without := breakdownRequest(), breakdownRequest()
	without.Tools = nil
	if estimateRequest(with).System <= estimateRequest(without).System {
		t.Error("schemas not counted in the system category")
	}
}

func TestEstimateRequest_StubMovesTokensFromFullToStubs(t *testing.T) {
	full := estimateRequest(breakdownRequest())
	req := breakdownRequest()
	req.Messages[3].Stub = "[read_file a.go: 100 lines shown earlier; read it again if needed]"
	stubbed := estimateRequest(req)
	if stubbed.ResultsFull != 0 || stubbed.Stubs == 0 || stubbed.Stubs >= full.ResultsFull {
		t.Errorf("full %+v, stubbed %+v", full, stubbed)
	}
	if stubbed.User != full.User || stubbed.Answers != full.Answers || stubbed.CallArgs != full.CallArgs {
		t.Errorf("other categories moved: %+v vs %+v", full, stubbed)
	}
	if stubbed.Total() >= full.Total() {
		t.Error("stub did not shrink the estimate")
	}
}

func TestMeter_EventBreakdownMatchesEstimate(t *testing.T) {
	r := startRig(t)
	r.a.Submit("hello")
	r.p.next(t).end(chat.FinishError, errors.New("boom"))
	wait(t, r.a)
	m := r.rec.lastMeter(t)
	if m.Breakdown.Total() != m.Used || m.Breakdown.User == 0 {
		t.Fatalf("meter = %+v", m)
	}
}

// ---------------------------------------------------------------------------
// Truncated prompt (R-65)
// ---------------------------------------------------------------------------

func TestMeter_TruncationWarnsOnce(t *testing.T) {
	var m meter
	if w := m.cut(3000, 6000); w != "" {
		t.Errorf("exactly half read must not warn, got %q", w)
	}
	if w := m.cut(500, 1500); w != "" {
		t.Errorf("a small prompt (under the floor) must not warn, got %q", w)
	}
	w := m.cut(1000, 6000)
	if !strings.HasPrefix(w, "the server read 1k of ~6k prompt tokens") || !strings.Contains(w, "OLLAMA_CONTEXT_LENGTH") {
		t.Fatalf("warning = %q", w)
	}
	if w := m.cut(1000, 6000); w != "" {
		t.Errorf("second truncation must stay silent, got %q", w)
	}
	m.reset()
	if w := m.cut(1000, 6000); w == "" {
		t.Error("reset must re-arm the truncation warning")
	}
}
