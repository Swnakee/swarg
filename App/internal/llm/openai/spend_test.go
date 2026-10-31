package openai

import (
	"strings"
	"testing"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// usage.cost (R-111) and key info (R-113)
// ---------------------------------------------------------------------------

func TestClient_UsageCostIsRead(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{Usage: &chat.Usage{PromptTokens: 7, CompletionTokens: 1, Cost: 0.00123}})
	evs := streamAll(t, New(srv.URL, "", nil), userReq("hi"))
	if evs[0].Kind != chat.EventUsage || evs[0].Usage.Cost != 0.00123 {
		t.Fatalf("events = %+v, want usage with cost 0.00123", evs)
	}
}

func TestClient_UsageWithoutOrBadCostIsZero(t *testing.T) {
	cases := map[string]string{
		"absent":   `{"prompt_tokens":3,"completion_tokens":1}`,
		"string":   `{"prompt_tokens":3,"completion_tokens":1,"cost":"x"}`,
		"null":     `{"prompt_tokens":3,"completion_tokens":1,"cost":null}`,
		"negative": `{"prompt_tokens":3,"completion_tokens":1,"cost":-1}`,
	}
	for name, usage := range cases {
		t.Run(name, func(t *testing.T) {
			body := `data: {"choices":[],"usage":` + usage + "}\n\ndata: [DONE]\n\n"
			c := rawClient(t, "", sseHandler(body))
			evs := streamAll(t, c, userReq("hi"))
			if evs[0].Kind != chat.EventUsage || evs[0].Usage.Cost != 0 || evs[0].Usage.PromptTokens != 3 {
				t.Fatalf("events = %+v, want usage with zero cost", evs)
			}
		})
	}
}

func TestKeyInfo_ReadsLimitsAndUsage(t *testing.T) {
	limit, left := 10.0, 7.5
	srv := llmtest.New(t)
	srv.Key = &llmtest.Key{Label: "k", Limit: &limit, Remaining: &left, Usage: 2.5, Daily: 0.5, Weekly: 1, Monthly: 2.5}
	got, err := New(srv.URL, "sk-test", nil).KeyInfo(t.Context())
	if err != nil {
		t.Fatalf("KeyInfo: %v", err)
	}
	if got.Limit == nil || *got.Limit != 10 || *got.Remaining != 7.5 || got.Usage != 2.5 || got.Daily != 0.5 || got.Monthly != 2.5 {
		t.Errorf("got %+v", got)
	}
}

func TestKeyInfo_NoLimitIsNil(t *testing.T) {
	srv := llmtest.New(t)
	srv.Key = &llmtest.Key{Usage: 1}
	got, err := New(srv.URL, "", nil).KeyInfo(t.Context())
	if err != nil || got.Limit != nil || got.Remaining != nil {
		t.Errorf("got %+v, %v; want nil limits", got, err)
	}
}

func TestKeyInfo_ErrorsNeverNameTheKey(t *testing.T) {
	srv := llmtest.New(t)
	srv.Key = &llmtest.Key{Status: 401, Body: "no"}
	_, err := New(srv.URL, "sk-test-secret", nil).KeyInfo(t.Context())
	if err == nil || strings.Contains(err.Error(), "sk-test-secret") {
		t.Errorf("err = %v, want an error without the key", err)
	}
	srv2 := llmtest.New(t) // no endpoint: 404
	if _, err := New(srv2.URL, "", nil).KeyInfo(t.Context()); err == nil {
		t.Error("a server without /key must give an error")
	}
}
