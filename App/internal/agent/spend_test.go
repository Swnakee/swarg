package agent

import (
	"testing"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
	"swarg/internal/llm/openai"
)

func TestFormatUSD(t *testing.T) {
	for _, c := range []struct {
		v    float64
		want string
	}{{0.0012, "$0.0012"}, {0.0099, "$0.0099"}, {0.01, "$0.010"}, {0.4567, "$0.457"}, {1, "$1.00"}, {12.5, "$12.50"}} {
		if got := FormatUSD(c.v); got != c.want {
			t.Errorf("FormatUSD(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestAddSpend_IgnoresNothingToCount(t *testing.T) {
	a := New(Deps{SystemPrompt: func() string { return "" }, Model: "m"})
	for _, v := range []float64{0, -1} {
		a.addUsage("m", chat.Usage{Cost: v})
	}
	a.addUsage("", chat.Usage{Cost: 1})
	if s := a.Spend(); s.Total != 0 || len(s.ByModel) != 0 {
		t.Errorf("spend %+v, want empty", s)
	}
	a.addUsage("m", chat.Usage{Cost: 0.5})
	s := a.Spend()
	s.ByModel["m"] = 99 // a copy: the Agent's spend does not change
	if got := a.Spend().Total; got != 0.5 {
		t.Errorf("total %v, want 0.5", got)
	}
}

// A response's cost reaches the Store once, with the model of the request (R-111).
func TestStore_SpendIsRecordedPerResponse(t *testing.T) {
	st := &fakeStore{}
	srv := llmtest.New(t, llmtest.Reply{Chunks: []string{"ok"}, Usage: &chat.Usage{PromptTokens: 5, CompletionTokens: 1, Cost: 0.5}})
	a := New(Deps{Provider: openai.New(srv.URL, "", nil), SystemPrompt: func() string { return "s" }, Model: "m", Store: st})
	a.Start(t.Context())
	a.Submit("hi")
	a.Wait()
	var got []storeOp
	for _, op := range st.all() {
		if op.kind == "spend" {
			got = append(got, op)
		}
	}
	if len(got) != 1 || got[0].model != "m" || got[0].usd != 0.5 {
		t.Errorf("spend ops %+v, want one for model m with 0.5", got)
	}
}

func TestFormatTokens(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{{0, "0"}, {999, "999"}, {1000, "1k"}, {12400, "12.4k"}, {999900, "999.9k"}, {1_000_000, "1M"}, {1_260_000, "1.3M"}} {
		if got := FormatTokens(c.n); got != c.want {
			t.Errorf("FormatTokens(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestAddUsage_TokensSumAndNegativesAreIgnored(t *testing.T) {
	a := New(Deps{SystemPrompt: func() string { return "" }, Model: "m"})
	a.addUsage("m", chat.Usage{})
	a.addUsage("m", chat.Usage{PromptTokens: -5, CachedTokens: -1, CompletionTokens: -2})
	if got := a.Tokens(); got != (Tokens{}) {
		t.Fatalf("tokens %+v, want zero", got)
	}
	a.addUsage("m", chat.Usage{PromptTokens: 100, CachedTokens: 60, CompletionTokens: 10})
	a.addUsage("", chat.Usage{PromptTokens: 20, CompletionTokens: 2}) // a blank model still counts tokens
	got := a.Tokens()
	if got != (Tokens{Prompt: 120, Cached: 60, Completion: 12}) || got.Total() != 132 {
		t.Errorf("tokens %+v (total %d), want 120/60/12 (132)", got, got.Total())
	}
}

// A response's tokens reach the Store as one usage op, and EvSpend carries the new total (R-131).
func TestStore_UsageIsRecordedPerResponseAndAnnounced(t *testing.T) {
	st := &fakeStore{}
	srv := llmtest.New(t, llmtest.Reply{Chunks: []string{"ok"}, Usage: &chat.Usage{PromptTokens: 5, CachedTokens: 2, CompletionTokens: 1}})
	var tokens int
	a := New(Deps{Provider: openai.New(srv.URL, "", nil), SystemPrompt: func() string { return "s" }, Model: "m", Store: st,
		OnEvent: func(ev Event) {
			if ev.Kind == EvSpend {
				tokens = ev.Tokens
			}
		}})
	a.Start(t.Context())
	a.Submit("hi")
	a.Wait()
	var got []storeOp
	for _, op := range st.all() {
		if op.kind == "usage" {
			got = append(got, op)
		}
		if op.kind == "spend" {
			t.Errorf("a free response wrote a spend op: %+v", op)
		}
	}
	if len(got) != 1 || got[0].tok != (Tokens{Prompt: 5, Cached: 2, Completion: 1}) {
		t.Errorf("usage ops %+v, want one 5/2/1", got)
	}
	if tokens != 6 {
		t.Errorf("EvSpend tokens %d, want 6", tokens)
	}
}

// A Compaction summary request is paid in tokens too (R-131).
func TestAsk_SummaryRequestTokensCount(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{Chunks: []string{"sum"}, Usage: &chat.Usage{PromptTokens: 30, CompletionTokens: 3}})
	a := New(Deps{Provider: openai.New(srv.URL, "", nil), SystemPrompt: func() string { return "s" }, Model: "m"})
	if _, err := a.ask(t.Context(), chat.Request{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if got := a.Tokens(); got != (Tokens{Prompt: 30, Completion: 3}) {
		t.Errorf("tokens %+v, want 30/0/3", got)
	}
}

func TestLoad_WithTokensSetsAndAnnounces(t *testing.T) {
	var tokens int
	a := New(Deps{SystemPrompt: func() string { return "" }, Model: "m", OnEvent: func(ev Event) {
		if ev.Kind == EvSpend {
			tokens = ev.Tokens
		}
	}})
	a.Start(t.Context())
	if err := a.Load([]chat.Message{{Role: chat.RoleUser, Content: "x", TurnStart: true}}, WithTokens(Tokens{Prompt: 7, Completion: 3})); err != nil {
		t.Fatal(err)
	}
	a.Wait()
	if got := a.Tokens(); got != (Tokens{Prompt: 7, Completion: 3}) {
		t.Errorf("tokens %+v", got)
	}
	if tokens != 10 {
		t.Errorf("EvSpend tokens %d, want 10", tokens)
	}
}
