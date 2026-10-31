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
		a.addSpend("m", v)
	}
	a.addSpend("", 1)
	if s := a.Spend(); s.Total != 0 || len(s.ByModel) != 0 {
		t.Errorf("spend %+v, want empty", s)
	}
	a.addSpend("m", 0.5)
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
