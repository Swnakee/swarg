package openai

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// Catalog (R-102) and the status texts (R-103, I-35)
// ---------------------------------------------------------------------------

const openRouterList = `{"data":[
 {"id":"a/paid","name":"A: Paid","context_length":200000,"pricing":{"prompt":"0.0000001","completion":"0.000001"},
  "supported_parameters":["tools","reasoning","temperature"]},
 {"id":"b/free:free","name":"B (free)","context_length":32768,"pricing":{"prompt":"0","completion":"0"},
  "supported_parameters":["temperature"]}]}`

func TestCatalog_ParsesOpenRouterFields(t *testing.T) {
	c := rawClient(t, "", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, openRouterList) })
	got, err := c.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries", len(got))
	}
	a, b := got[0], got[1]
	if a.ID != "a/paid" || a.Name != "A: Paid" || a.Context != 200000 || a.Prompt != 1e-7 || a.Completion != 1e-6 || !a.Tools || !a.Reasoning {
		t.Errorf("paid entry %+v", a)
	}
	if b.ID != "b/free:free" || b.Context != 32768 || b.Prompt != 0 || b.Tools || b.Reasoning {
		t.Errorf("free entry %+v", b)
	}
}

// A plain OpenAI-style list fills only the ids; Models keeps its sorted unique ids.
func TestCatalog_PlainListGivesIDsOnly(t *testing.T) {
	c := rawClient(t, "", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"m-b"},{"id":"m-a"}]}`)
	})
	got, err := c.Catalog(t.Context())
	if err != nil || len(got) != 2 || got[0].ID != "m-a" || got[0].Context != 0 || got[0].Tools {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDo_StatusTexts(t *testing.T) {
	const key = "sk-test-secret"
	for _, tc := range []struct {
		status int
		want   string
	}{
		{401, "refused the API key"},
		{402, "no credits"},
		{404, "no such model"},
		{404, "/model"},
		{429, "rate limited"},
		{502, "failed upstream"},
		{400, "model server returned 400"},
	} {
		c := rawClient(t, key, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, `{"error":{"message":"details"}}`)
		})
		_, err := c.Stream(t.Context(), userReq("hi"))
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "details") {
			t.Errorf("%d: err = %v, want %q and the body snippet", tc.status, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), key) {
			t.Errorf("%d: the key leaked into %q", tc.status, err)
		}
	}
}

func TestDo_RateLimitWaitsOnceThenSucceeds(t *testing.T) {
	var n atomic.Int32
	c := rawClient(t, "", func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		sseHandler("data: [DONE]\n\n")(w, nil)
	})
	ch, err := c.Stream(t.Context(), userReq("hi"))
	if err != nil {
		t.Fatalf("Stream after one 429: %v", err)
	}
	for range ch {
	}
	if n.Load() != 2 {
		t.Errorf("requests = %d, want 2", n.Load())
	}
}

// A long Retry-After (or a second 429) is reported at once, never a retry loop.
func TestDo_RateLimitLongRetryAfterStops(t *testing.T) {
	var n atomic.Int32
	c := rawClient(t, "", func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := c.Stream(t.Context(), userReq("hi"))
	if err == nil || !strings.Contains(err.Error(), "rate limited") || !strings.Contains(err.Error(), "120") {
		t.Fatalf("err = %v, want rate limited with the wait", err)
	}
	if n.Load() != 1 {
		t.Errorf("requests = %d, want 1", n.Load())
	}
}
