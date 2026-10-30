package llmtest_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const reqBody = `{"model":"m","messages":[{"role":"user","content":"hi"}]}`

// post sends one chat completion request and returns the status and the body.
func post(t *testing.T, url, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(url+"/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

// frames decodes the "data:" events of a body, skipping [DONE].
func frames(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(body, "\n") {
		payload, ok := strings.CutPrefix(line, "data: ")
		if !ok || payload == "[DONE]" {
			continue
		}
		var f map[string]any
		if err := json.Unmarshal([]byte(payload), &f); err != nil {
			t.Fatalf("bad event %q: %v", payload, err)
		}
		out = append(out, f)
	}
	return out
}

// contents joins the delta contents of a body.
func contents(t *testing.T, body string) string {
	t.Helper()
	var sb strings.Builder
	for _, f := range frames(t, body) {
		for _, c := range f["choices"].([]any) {
			d := c.(map[string]any)["delta"].(map[string]any)
			if s, ok := d["content"].(string); ok {
				sb.WriteString(s)
			}
		}
	}
	return sb.String()
}

// lastFinish returns the finish_reason of the last event of a body without usage chunk.
func lastFinish(t *testing.T, body string) any {
	t.Helper()
	evs := frames(t, body)
	return evs[len(evs)-1]["choices"].([]any)[0].(map[string]any)["finish_reason"]
}

// toolCalls returns the tool_calls deltas of a body, in arrival order.
func toolCalls(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, f := range frames(t, body) {
		for _, c := range f["choices"].([]any) {
			d := c.(map[string]any)["delta"].(map[string]any)
			tcs, _ := d["tool_calls"].([]any)
			for _, tc := range tcs {
				out = append(out, tc.(map[string]any))
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestServer_ServesRepliesInOrder(t *testing.T) {
	srv := llmtest.New(t,
		llmtest.Reply{Chunks: []string{"привет \"мир\"\n"}, Finish: "length", Usage: &chat.Usage{PromptTokens: 18, CompletionTokens: 5, CachedTokens: 2}},
		llmtest.Reply{Chunks: []string{"second"}},
	)

	status, body := post(t, srv.URL, reqBody)
	if status != http.StatusOK {
		t.Fatalf("first status = %d", status)
	}
	if got := contents(t, body); got != "привет \"мир\"\n" {
		t.Errorf("first content = %q", got)
	}
	evs := frames(t, body)
	last := evs[len(evs)-1]
	usage := last["usage"].(map[string]any)
	if usage["prompt_tokens"] != 18.0 || usage["total_tokens"] != 23.0 {
		t.Errorf("usage chunk = %v", usage)
	}
	if fr := evs[len(evs)-2]["choices"].([]any)[0].(map[string]any)["finish_reason"]; fr != "length" {
		t.Errorf("finish_reason = %v", fr)
	}
	if !strings.Contains(body, "data: [DONE]\n\n") {
		t.Errorf("no [DONE] in %q", body)
	}

	if status, body = post(t, srv.URL, reqBody); status != http.StatusOK || contents(t, body) != "second" {
		t.Errorf("second reply: status %d, body %q", status, body)
	}
	if status, _ = post(t, srv.URL, reqBody); status != http.StatusInternalServerError {
		t.Errorf("extra request status = %d, want 500", status)
	}
}

func TestServer_ReasoningChunks(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{Reasoning: []string{"think ", "hard"}, Chunks: []string{"answer"}})
	_, body := post(t, srv.URL, reqBody)

	var order []string
	for _, f := range frames(t, body) {
		for _, c := range f["choices"].([]any) {
			d := c.(map[string]any)["delta"].(map[string]any)
			if s, ok := d["reasoning_content"].(string); ok {
				order = append(order, "r:"+s)
			}
			if s, ok := d["content"].(string); ok {
				order = append(order, "c:"+s)
			}
		}
	}
	want := []string{"r:think ", "r:hard", "c:answer"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("delta order = %v, want %v", order, want)
	}
}

func TestServer_ToolCallsMlxFormat(t *testing.T) {
	calls := []chat.ToolCall{
		{ID: "call_1", Name: "read_file", Args: `{"path": "README.md"}`},
		{ID: "call_2", Name: "list_dir", Args: `{"path": "."}`},
	}
	srv := llmtest.New(t,
		llmtest.Reply{Chunks: []string{"читаю"}, ToolCalls: calls},
		llmtest.Reply{ToolCalls: calls[:1], Finish: "stop"},
	)

	_, body := post(t, srv.URL, reqBody)
	want := `{"function":{"name":"read_file","arguments":"{\"path\": \"README.md\"}"},"type":"function","id":"call_1","index":0}`
	if !strings.Contains(body, want) {
		t.Errorf("first call frame missing in %q", body)
	}
	tcs := toolCalls(t, body)
	if len(tcs) != 2 {
		t.Fatalf("tool call frames = %d, want 2", len(tcs))
	}
	for i, tc := range tcs {
		fn := tc["function"].(map[string]any)
		if tc["index"] != float64(i) || tc["id"] != calls[i].ID || tc["type"] != "function" ||
			fn["name"] != calls[i].Name || fn["arguments"] != calls[i].Args {
			t.Errorf("call %d = %v", i, tc)
		}
	}
	if fr := lastFinish(t, body); fr != "tool_calls" {
		t.Errorf("finish_reason = %v, want tool_calls", fr)
	}

	_, body = post(t, srv.URL, reqBody)
	if fr := lastFinish(t, body); fr != "stop" {
		t.Errorf("explicit Finish not honored: finish_reason = %v", fr)
	}
}

func TestServer_SplitArgs(t *testing.T) {
	args := `{"path": "Привет мир.md", "n": 3}`
	srv := llmtest.New(t, llmtest.Reply{
		ToolCalls: []chat.ToolCall{{ID: "call_1", Name: "read_file", Args: args}},
		SplitArgs: true,
	})

	_, body := post(t, srv.URL, reqBody)
	tcs := toolCalls(t, body)
	if len(tcs) != 3 {
		t.Fatalf("tool call frames = %d, want 3", len(tcs))
	}
	var got strings.Builder
	for i, tc := range tcs {
		fn := tc["function"].(map[string]any)
		got.WriteString(fn["arguments"].(string))
		if tc["index"] != 0.0 {
			t.Errorf("frame %d index = %v", i, tc["index"])
		}
		if i == 0 {
			if tc["id"] != "call_1" || tc["type"] != "function" || fn["name"] != "read_file" {
				t.Errorf("first frame = %v", tc)
			}
			continue
		}
		if _, ok := tc["id"]; ok || tc["type"] != nil || fn["name"] != nil {
			t.Errorf("frame %d repeats id/type/name: %v", i, tc)
		}
	}
	if got.String() != args {
		t.Errorf("concatenated arguments = %q, want %q", got.String(), args)
	}
}

func TestServer_RecordsRequests(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{})
	post(t, srv.URL, `{"model":"m1","messages":[{"role":"system","content":"к \"x\""}]}`)

	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	if reqs[0]["model"] != "m1" {
		t.Errorf("model = %v", reqs[0]["model"])
	}
	msg := reqs[0]["messages"].([]any)[0].(map[string]any)
	if msg["content"] != "к \"x\"" {
		t.Errorf("content = %q", msg["content"])
	}
}

func TestServer_GateAfter(t *testing.T) {
	gate := make(chan struct{})
	srv := llmtest.New(t, llmtest.Reply{Chunks: []string{"a", "b"}, GateAfter: 1, Gate: gate})

	resp, err := http.Post(srv.URL+"/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended before chunk a: %v", err)
		}
		if strings.Contains(line, `"content":"a"`) {
			break
		}
	}
	if n := srv.Completed(); n != 0 {
		t.Errorf("Completed while gated = %d, want 0", n)
	}

	close(gate)
	rest, err := io.ReadAll(br)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rest), `"content":"b"`) || !strings.Contains(string(rest), "[DONE]") {
		t.Errorf("rest of stream = %q", rest)
	}
	if n := srv.Completed(); n != 1 {
		t.Errorf("Completed after gate = %d, want 1", n)
	}
}

func TestServer_GateReleasedOnClientDisconnect(t *testing.T) {
	gate := make(chan struct{}) // never closed: the handler must return on disconnect
	var srv *llmtest.Server
	// Registered before New, so it runs after the server is closed: httptest.Close
	// waits for the handler, so reaching this check proves the handler returned.
	t.Cleanup(func() {
		if n := srv.Completed(); n != 0 {
			t.Errorf("Completed after disconnect = %d, want 0", n)
		}
	})
	srv = llmtest.New(t, llmtest.Reply{Chunks: []string{"a", "b"}, GateAfter: 1, Gate: gate})

	resp, err := http.Post(srv.URL+"/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestServer_CompletedAndCompletedAt(t *testing.T) {
	srv := llmtest.New(t,
		llmtest.Reply{Chunks: []string{"a"}},
		llmtest.Reply{Chunks: []string{"b"}},
		llmtest.Reply{Status: http.StatusServiceUnavailable},
	)
	post(t, srv.URL, reqBody)
	post(t, srv.URL, reqBody)
	post(t, srv.URL, reqBody)

	if n := srv.Completed(); n != 3 {
		t.Errorf("Completed = %d, want 3", n)
	}
	at := srv.CompletedAt()
	if len(at) != 3 || at[0] != 0 || at[1] != 1 || at[2] != 2 {
		t.Errorf("CompletedAt = %v, want [0 1 2]", at)
	}
}

func TestServer_StatusNoDoneMalformed(t *testing.T) {
	srv := llmtest.New(t,
		llmtest.Reply{Status: http.StatusBadGateway},
		llmtest.Reply{Chunks: []string{"cut"}, NoDone: true},
		llmtest.Reply{Chunks: []string{"x"}, Malformed: true},
	)

	status, body := post(t, srv.URL, reqBody)
	if status != http.StatusBadGateway || body != "fake error" {
		t.Errorf("status reply: %d %q", status, body)
	}

	_, body = post(t, srv.URL, reqBody)
	if strings.Contains(body, "[DONE]") || strings.Contains(body, `"finish_reason":"stop"`) || contents(t, body) != "cut" {
		t.Errorf("NoDone body = %q", body)
	}

	_, body = post(t, srv.URL, reqBody)
	if !strings.Contains(body, "data: {not json\n\n") {
		t.Errorf("Malformed body has no invalid line: %q", body)
	}
	if !strings.Contains(body, `"content":"x"`) || !strings.Contains(body, "[DONE]") {
		t.Errorf("Malformed body did not continue normally: %q", body)
	}
}

func TestServer_GoneSignalsDisconnect(t *testing.T) {
	gate := make(chan struct{}) // never closed
	srv := llmtest.New(t, llmtest.Reply{Chunks: []string{"a", "b"}, GateAfter: 1, Gate: gate})

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/chat/completions", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	cancel()
	select {
	case <-srv.Gone():
	case <-time.After(10 * time.Second):
		t.Fatal("no disconnect signal")
	}
	if n := srv.Completed(); n != 0 {
		t.Errorf("Completed after disconnect = %d, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// Fake OpenRouter (T11): the model list and Retry-After
// ---------------------------------------------------------------------------

func TestServer_ModelsEndpoint(t *testing.T) {
	s := llmtest.New(t)
	s.Models = []llmtest.Model{
		{ID: "a/x:free", Name: "A X", Context: 32768, Prompt: "0", Completion: "0", Parameters: []string{"tools"}},
		{ID: "plain"},
	}
	resp, err := http.Get(s.URL + "/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || len(got.Data) != 2 {
		t.Fatalf("decode: %v, %d entries", err, len(got.Data))
	}
	a, p := got.Data[0], got.Data[1]
	if a["id"] != "a/x:free" || a["name"] != "A X" || a["context_length"] != float64(32768) || a["supported_parameters"] == nil {
		t.Errorf("openrouter entry = %v", a)
	}
	if _, has := p["name"]; has || p["id"] != "plain" {
		t.Errorf("plain entry = %v", p)
	}
}

func TestServer_RetryAfterHeader(t *testing.T) {
	s := llmtest.New(t, llmtest.Reply{Status: 429, RetryAfter: 3, Body: `{"error":{"message":"slow down"}}`})
	resp, err := http.Post(s.URL+"/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") != "3" || !strings.Contains(string(body), "slow down") {
		t.Fatalf("status %d, Retry-After %q, body %q", resp.StatusCode, resp.Header.Get("Retry-After"), body)
	}
}
