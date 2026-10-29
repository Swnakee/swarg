package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

// streamAll starts a stream and collects its events until the channel closes.
func streamAll(t *testing.T, c *Client, req chat.Request) []chat.StreamEvent {
	t.Helper()
	ch, err := c.Stream(t.Context(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	return drain(t, ch)
}

// drain collects events until the channel closes. It fails the test if the channel stays open.
func drain(t *testing.T, ch <-chan chat.StreamEvent) []chat.StreamEvent {
	t.Helper()
	var evs []chat.StreamEvent
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return evs
			}
			evs = append(evs, ev)
		case <-timeout:
			t.Fatal("stream channel did not close")
		}
	}
}

// doneEvents returns the EventDone events in evs.
func doneEvents(evs []chat.StreamEvent) []chat.StreamEvent {
	var out []chat.StreamEvent
	for _, ev := range evs {
		if ev.Kind == chat.EventDone {
			out = append(out, ev)
		}
	}
	return out
}

// userReq is a one-message request for model "m".
func userReq(text string) chat.Request {
	return chat.Request{Model: "m", Messages: []chat.Message{{Role: chat.RoleUser, Content: text}}}
}

// rawClient starts an httptest server with handler h and returns a Client for it.
func rawClient(t *testing.T, key string, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL, key, nil)
}

// sseHandler serves body as an event stream.
func sseHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestClient_StreamsDeltas(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{
		Chunks: []string{"a", "b", "c"},
		Usage:  &chat.Usage{PromptTokens: 5, CompletionTokens: 3, CachedTokens: 2},
	})
	evs := streamAll(t, New(srv.URL, "", nil), userReq("hi"))

	want := []chat.StreamEvent{
		{Kind: chat.EventDelta, Text: "a"},
		{Kind: chat.EventDelta, Text: "b"},
		{Kind: chat.EventDelta, Text: "c"},
		{Kind: chat.EventUsage, Usage: chat.Usage{PromptTokens: 5, CompletionTokens: 3, CachedTokens: 2}},
		{Kind: chat.EventDone, Finish: chat.FinishStop},
	}
	if len(evs) != len(want) {
		t.Fatalf("got %d events %+v, want %d", len(evs), evs, len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(evs[i], want[i]) {
			t.Errorf("event %d = %+v, want %+v", i, evs[i], want[i])
		}
	}
}

func TestClient_KeepaliveProgress(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{Keepalives: 2, Chunks: []string{"x"}})
	evs := streamAll(t, New(srv.URL, "", nil), userReq("hi"))
	want := []chat.StreamEvent{
		{Kind: chat.EventProgress, Done: 1, Total: 2},
		{Kind: chat.EventProgress, Done: 2, Total: 2},
		{Kind: chat.EventDelta, Text: "x"},
		{Kind: chat.EventDone, Finish: chat.FinishStop},
	}
	if len(evs) != len(want) {
		t.Fatalf("got %+v, want %+v", evs, want)
	}
	for i := range want {
		if !reflect.DeepEqual(evs[i], want[i]) {
			t.Errorf("event %d = %+v, want %+v", i, evs[i], want[i])
		}
	}

	bad := rawClient(t, "", sseHandler(
		": keepalive x/y\n\n: keepalive\n\n: keepalive 1/2\n\ndata: [DONE]\n\n"))
	evs = streamAll(t, bad, userReq("hi"))
	if len(evs) != 2 || evs[0].Kind != chat.EventProgress || evs[0].Done != 1 || evs[0].Total != 2 {
		t.Errorf("malformed keepalive not ignored: %+v", evs)
	}
}

func TestClient_FinishLength(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{Chunks: []string{"a"}, Finish: "length"})
	evs := streamAll(t, New(srv.URL, "", nil), userReq("hi"))
	done := doneEvents(evs)
	if len(done) != 1 || done[0].Finish != chat.FinishLength || done[0].Err != nil {
		t.Errorf("done = %+v, want one Done(length)", done)
	}
}

func TestClient_EmptyDeltaIgnored(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{Chunks: []string{"", "x", ""}})
	evs := streamAll(t, New(srv.URL, "", nil), userReq("hi"))
	var texts []string
	for _, ev := range evs {
		if ev.Kind == chat.EventDelta {
			texts = append(texts, ev.Text)
		}
	}
	if len(texts) != 1 || texts[0] != "x" {
		t.Errorf("deltas = %q, want [x]", texts)
	}
}

func TestClient_RequestBody(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{}, llmtest.Reply{})
	c := New(srv.URL+"/", "", nil) // trailing slash is trimmed
	req := chat.Request{
		Model: "qwen",
		Messages: []chat.Message{
			{Role: chat.RoleSystem, Content: "sys", Truncated: true},
			{Role: chat.RoleUser, Content: "привет \"q\"\n"},
		},
		MaxTokens: 100,
		Extra: map[string]any{
			"chat_template_kwargs": map[string]any{"enable_thinking": false},
			"temperature":          0.2,
			"model":                "evil",
			"messages":             "evil",
			"stream":               false,
		},
	}
	streamAll(t, c, req)
	streamAll(t, c, chat.Request{Model: "qwen", Messages: req.Messages[:1]})

	got := srv.Requests()
	if len(got) != 2 {
		t.Fatalf("requests = %d, want 2", len(got))
	}
	b := got[0]
	if b["model"] != "qwen" || b["stream"] != true {
		t.Errorf("model/stream = %v/%v", b["model"], b["stream"])
	}
	if so, _ := b["stream_options"].(map[string]any); so["include_usage"] != true {
		t.Errorf("stream_options = %v", b["stream_options"])
	}
	if b["max_tokens"] != float64(100) || b["temperature"] != 0.2 {
		t.Errorf("max_tokens/temperature = %v/%v", b["max_tokens"], b["temperature"])
	}
	if ct, _ := b["chat_template_kwargs"].(map[string]any); ct["enable_thinking"] != false {
		t.Errorf("chat_template_kwargs = %v", b["chat_template_kwargs"])
	}
	msgs, _ := b["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v", b["messages"])
	}
	first := msgs[0].(map[string]any)
	if len(first) != 2 || first["role"] != "system" || first["content"] != "sys" {
		t.Errorf("message 0 = %v, want only role and content", first)
	}
	if msgs[1].(map[string]any)["content"] != "привет \"q\"\n" {
		t.Errorf("message 1 content = %v", msgs[1])
	}
	if _, ok := got[1]["max_tokens"]; ok {
		t.Errorf("max_tokens sent with MaxTokens 0: %v", got[1]["max_tokens"])
	}
}

func TestClient_AuthHeader(t *testing.T) {
	cases := []struct {
		name, key, want string
	}{
		{"with key", "secret", "Bearer secret"},
		{"empty key", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hdr := make(chan http.Header, 1)
			c := rawClient(t, tc.key, func(w http.ResponseWriter, r *http.Request) {
				hdr <- r.Header.Clone()
				sseHandler("data: [DONE]\n\n")(w, r)
			})
			streamAll(t, c, userReq("hi"))
			h := <-hdr
			if got := h.Get("Authorization"); got != tc.want {
				t.Errorf("Authorization = %q, want %q", got, tc.want)
			}
			if h.Get("Content-Type") != "application/json" || h.Get("Accept") != "text/event-stream" {
				t.Errorf("headers = %v", h)
			}
		})
	}
}

func TestClient_HTTPError(t *testing.T) {
	big := strings.Repeat("x", 4096)
	c := rawClient(t, "", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, big)
	})
	_, err := c.Stream(t.Context(), userReq("hi"))
	if err == nil {
		t.Fatal("Stream: no error for 400")
	}
	msg := err.Error()
	if !strings.Contains(msg, "400") {
		t.Errorf("error %q lacks status", msg)
	}
	if !strings.Contains(msg, strings.Repeat("x", 512)) || strings.Contains(msg, strings.Repeat("x", 513)) {
		t.Errorf("body snippet not exactly 512 bytes: %d bytes in error", len(msg))
	}
}

func TestClient_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := New(url, "", nil).Stream(t.Context(), userReq("hi"))
	if err == nil || !strings.Contains(err.Error(), "cannot reach the model server") {
		t.Errorf("err = %v, want unreachable-server error", err)
	}
}

func TestClient_NoDone(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{Chunks: []string{"a"}, NoDone: true})
	evs := streamAll(t, New(srv.URL, "", nil), userReq("hi"))
	done := doneEvents(evs)
	if len(done) != 1 || done[0].Finish != chat.FinishError || done[0].Err == nil {
		t.Errorf("done = %+v, want one Done(error)", done)
	}
}

func TestClient_MalformedChunk(t *testing.T) {
	cases := []struct {
		name    string
		data    string
		wantErr bool
		wantTxt string
	}{
		{"truncated JSON", `{"choices":[{"delta":{"content":"a"`, true, ""},
		{"choices null", `{"choices":null}`, false, ""},
		{"choices empty", `{"choices":[]}`, false, ""},
		{"unknown fields", `{"foo":{"bar":1},"choices":[{"index":0,"delta":{"content":"ok","extra":true}}]}`, false, "ok"},
		{"wrong types", `{"choices":"x"}`, true, ""},
		{"content wrong type", `{"choices":[{"delta":{"content":5}}]}`, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := rawClient(t, "", sseHandler("data: "+tc.data+"\n\ndata: [DONE]\n\n"))
			evs := streamAll(t, c, userReq("hi"))
			done := doneEvents(evs)
			if len(done) != 1 || evs[len(evs)-1].Kind != chat.EventDone {
				t.Fatalf("events = %+v, want exactly one trailing Done", evs)
			}
			if (done[0].Err != nil) != tc.wantErr {
				t.Errorf("Done.Err = %v, wantErr %v", done[0].Err, tc.wantErr)
			}
			if tc.wantTxt != "" && (len(evs) < 2 || evs[0].Text != tc.wantTxt) {
				t.Errorf("events = %+v, want delta %q", evs, tc.wantTxt)
			}
		})
	}
}

func TestClient_UsageOnlyChunk(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{Usage: &chat.Usage{PromptTokens: 7, CompletionTokens: 1}})
	evs := streamAll(t, New(srv.URL, "", nil), userReq("hi"))
	if len(evs) != 2 || evs[0].Kind != chat.EventUsage || evs[0].Usage.PromptTokens != 7 {
		t.Fatalf("events = %+v, want Usage then Done", evs)
	}
	if evs[1].Kind != chat.EventDone {
		t.Errorf("last event = %+v, want Done", evs[1])
	}
}

func TestClient_IdleTimeout(t *testing.T) {
	gate := make(chan struct{}) // never closed: the server stalls after the keepalive
	srv := llmtest.New(t, llmtest.Reply{Keepalives: 1, Gate: gate, GateAfter: 0, Chunks: []string{"a"}})
	c := New(srv.URL, "", nil)
	c.IdleTimeout = 50 * time.Millisecond
	evs := streamAll(t, c, userReq("hi"))
	done := doneEvents(evs)
	if len(done) != 1 || done[0].Finish != chat.FinishError || done[0].Err == nil {
		t.Fatalf("done = %+v, want one Done(error)", done)
	}
	if !strings.Contains(done[0].Err.Error(), "idle") {
		t.Errorf("err = %v, want idle timeout", done[0].Err)
	}
}

func TestClient_BodyFullyRead(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{Chunks: []string{"a", "b"}})
	ch, err := New(srv.URL, "", nil).Stream(t.Context(), userReq("hi"))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	drain(t, ch) // the channel closes only after the body is drained
	if n := srv.Completed(); n != 1 {
		t.Errorf("Completed = %d after the channel closed, want 1", n)
	}
}

func TestClient_ContextCancel(t *testing.T) {
	gate := make(chan struct{})
	srv := llmtest.New(t, llmtest.Reply{Keepalives: 1, Chunks: []string{"a"}, GateAfter: 1, Gate: gate})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := New(srv.URL, "", nil).Stream(ctx, userReq("hi"))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for ev := range ch {
		if ev.Kind == chat.EventDelta {
			break // the stream is now blocked on the gate
		}
	}
	cancel()
	drain(t, ch) // the channel must close; closing is the signal that the goroutine has finished
}

// ---------------------------------------------------------------------------
// Tool calls
// ---------------------------------------------------------------------------

// sseBody joins payloads into an SSE body, one data event each.
func sseBody(payloads ...string) string {
	var sb strings.Builder
	for _, p := range payloads {
		sb.WriteString("data: " + p + "\n\n")
	}
	return sb.String()
}

func TestClient_ToolCallAccumulation(t *testing.T) {
	const done = "[DONE]"
	cases := []struct {
		name       string
		sse        string
		wantText   string
		want       []chat.ToolCall
		wantFinish chat.FinishReason
	}{
		{
			name: "mlx whole calls keep index order",
			sse: sseBody(
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","type":"function","function":{"name":"list_dir","arguments":"{}"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				done),
			want: []chat.ToolCall{
				{ID: "c1", Name: "read_file", Args: `{"path":"a"}`},
				{ID: "c2", Name: "list_dir", Args: `{}`},
			},
			wantFinish: chat.FinishToolCalls,
		},
		{
			name: "split arguments are concatenated",
			sse: sseBody(
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a\"}"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				done),
			want:       []chat.ToolCall{{ID: "c1", Name: "read_file", Args: `{"path":"a"}`}},
			wantFinish: chat.FinishToolCalls,
		},
		{
			name: "no index counts as index 0",
			sse: sseBody(
				`{"choices":[{"delta":{"tool_calls":[{"id":"c1","function":{"name":"read_file","arguments":"{\"path\""}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":":\"a\"}"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				done),
			want:       []chat.ToolCall{{ID: "c1", Name: "read_file", Args: `{"path":"a"}`}},
			wantFinish: chat.FinishToolCalls,
		},
		{
			name: "interleaved indices are sorted",
			sse: sseBody(
				`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","function":{"name":"list_dir","arguments":"{"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"}"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				done),
			want: []chat.ToolCall{
				{ID: "a", Name: "read_file", Args: `{"path":"a"}`},
				{ID: "b", Name: "list_dir", Args: `{}`},
			},
			wantFinish: chat.FinishToolCalls,
		},
		{
			name: "parallel calls all sent as index 0 start new calls on a new id",
			sse: sseBody(
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"b","function":{"name":"read_file","arguments":"{\"path\":"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"b\"}"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				done),
			want: []chat.ToolCall{
				{ID: "a", Name: "read_file", Args: `{"path":"a"}`},
				{ID: "b", Name: "read_file", Args: `{"path":"b"}`},
			},
			wantFinish: chat.FinishToolCalls,
		},
		{
			name: "missing id is left empty for the agent to fill",
			sse: sseBody(
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"list_dir","arguments":"{}"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				done),
			want:       []chat.ToolCall{{Name: "list_dir", Args: `{}`}},
			wantFinish: chat.FinishToolCalls,
		},
		{
			name: "finish stop still delivers the calls",
			sse: sseBody(
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"list_dir","arguments":"{}"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
				done),
			want:       []chat.ToolCall{{ID: "c1", Name: "list_dir", Args: `{}`}},
			wantFinish: chat.FinishStop,
		},
		{
			name: "text and calls in one answer",
			sse: sseBody(
				`{"choices":[{"delta":{"content":"Checking."}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"list_dir","arguments":"{}"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				done),
			wantText:   "Checking.",
			want:       []chat.ToolCall{{ID: "c1", Name: "list_dir", Args: `{}`}},
			wantFinish: chat.FinishToolCalls,
		},
		{
			name: "malformed arguments pass through raw",
			sse: sseBody(
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read_file","arguments":"{\"path\":"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				done),
			want:       []chat.ToolCall{{ID: "c1", Name: "read_file", Args: `{"path":`}},
			wantFinish: chat.FinishToolCalls,
		},
		{
			name: "no calls gives nil",
			sse: sseBody(
				`{"choices":[{"delta":{"content":"hi"}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
				done),
			wantText:   "hi",
			wantFinish: chat.FinishStop,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := rawClient(t, "", sseHandler(tc.sse))
			evs := streamAll(t, c, userReq("hi"))
			dones := doneEvents(evs)
			if len(dones) != 1 || evs[len(evs)-1].Kind != chat.EventDone {
				t.Fatalf("events = %+v, want exactly one trailing Done", evs)
			}
			d := dones[0]
			if d.Finish != tc.wantFinish || d.Err != nil {
				t.Errorf("finish = %v, err = %v; want %v, nil", d.Finish, d.Err, tc.wantFinish)
			}
			if !reflect.DeepEqual(d.ToolCalls, tc.want) {
				t.Errorf("ToolCalls = %+v, want %+v", d.ToolCalls, tc.want)
			}
			var text strings.Builder
			for _, ev := range evs {
				if ev.Kind == chat.EventDelta {
					text.WriteString(ev.Text)
				}
			}
			if text.String() != tc.wantText {
				t.Errorf("text = %q, want %q", text.String(), tc.wantText)
			}
		})
	}
}

func TestClient_CancelBeforeDoneRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	c := rawClient(t, "", sseHandler(sseBody(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"list_dir","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		"[DONE]")))
	ch, err := c.Stream(ctx, userReq("hi"))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	cancel() // nobody reads: the pump must not block on its final send
	drain(t, ch)
}

func TestClient_RequestToolsBody(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{}, llmtest.Reply{})
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	req := userReq("hi")
	req.Tools = []chat.ToolSpec{
		{Name: "read_file", Description: "Read a file.", Schema: schema},
		{Name: "list_dir", Description: "List a folder."},
	}
	streamAll(t, New(srv.URL, "", nil), req)
	streamAll(t, New(srv.URL, "", nil), userReq("hi"))

	got := srv.Requests()
	if len(got) != 2 {
		t.Fatalf("requests = %d, want 2", len(got))
	}
	tools, _ := got[0]["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %v, want 2 entries", got[0]["tools"])
	}
	first := tools[0].(map[string]any)
	fn := first["function"].(map[string]any)
	if first["type"] != "function" || fn["name"] != "read_file" || fn["description"] != "Read a file." {
		t.Errorf("tool 0 = %v", first)
	}
	var wantSchema any
	_ = json.Unmarshal(schema, &wantSchema)
	if !reflect.DeepEqual(fn["parameters"], wantSchema) {
		t.Errorf("parameters = %v, want %v", fn["parameters"], wantSchema)
	}
	fn2 := tools[1].(map[string]any)["function"].(map[string]any)
	if want := map[string]any{"type": "object", "properties": map[string]any{}}; !reflect.DeepEqual(fn2["parameters"], want) {
		t.Errorf("empty schema parameters = %v, want %v", fn2["parameters"], want)
	}
	if _, ok := got[1]["tools"]; ok {
		t.Errorf("tools sent without Tools: %v", got[1]["tools"])
	}
}

func TestClient_SerializesToolMessages(t *testing.T) {
	srv := llmtest.New(t, llmtest.Reply{})
	req := chat.Request{Model: "m", Messages: []chat.Message{
		{Role: chat.RoleUser, Content: "list"},
		{Role: chat.RoleAssistant, ToolCalls: []chat.ToolCall{{ID: "c1", Name: "list_dir", Args: `{}`}}},
		{Role: chat.RoleTool, ToolCallID: "c1", Name: "list_dir", Content: "a.txt"},
	}}
	streamAll(t, New(srv.URL, "", nil), req)

	msgs, _ := srv.Requests()[0]["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v", msgs)
	}
	asst := msgs[1].(map[string]any)
	if c, ok := asst["content"]; !ok || c != "" {
		t.Errorf("assistant content = %v (present %v), want empty string", c, ok)
	}
	calls, _ := asst["tool_calls"].([]any)
	wantCall := map[string]any{
		"id":       "c1",
		"type":     "function",
		"function": map[string]any{"name": "list_dir", "arguments": `{}`},
	}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], wantCall) {
		t.Errorf("tool_calls = %v, want [%v]", calls, wantCall)
	}
	wantTool := map[string]any{"role": "tool", "tool_call_id": "c1", "name": "list_dir", "content": "a.txt"}
	if !reflect.DeepEqual(msgs[2], wantTool) {
		t.Errorf("tool message = %v, want %v", msgs[2], wantTool)
	}
}

// ---------------------------------------------------------------------------
// Reasoning
// ---------------------------------------------------------------------------

// chunkJSON is a data payload with one delta object.
func chunkJSON(delta string) string {
	return `{"choices":[{"index":0,"finish_reason":null,"delta":{` + delta + `}}]}`
}

func TestStream_ReasoningContentDelta(t *testing.T) {
	body := sseBody(
		chunkJSON(`"role":"assistant","reasoning_content":"think "`),
		chunkJSON(`"reasoning_content":"hard","content":"answer"`),
		`[DONE]`,
	)
	evs := streamAll(t, rawClient(t, "", sseHandler(body)), userReq("hi"))
	want := []chat.StreamEvent{
		{Kind: chat.EventReasoning, Text: "think "},
		{Kind: chat.EventReasoning, Text: "hard"},
		{Kind: chat.EventDelta, Text: "answer"},
		{Kind: chat.EventDone, Finish: chat.FinishStop},
	}
	if !reflect.DeepEqual(evs, want) {
		t.Errorf("events = %+v, want %+v", evs, want)
	}
}

func TestStream_ReasoningFallbackNotDoubled(t *testing.T) {
	body := sseBody(
		chunkJSON(`"reasoning":"only fallback"`),
		chunkJSON(`"reasoning_content":"primary","reasoning":"primary"`),
		`[DONE]`,
	)
	evs := streamAll(t, rawClient(t, "", sseHandler(body)), userReq("hi"))
	want := []chat.StreamEvent{
		{Kind: chat.EventReasoning, Text: "only fallback"},
		{Kind: chat.EventReasoning, Text: "primary"},
		{Kind: chat.EventDone, Finish: chat.FinishStop},
	}
	if !reflect.DeepEqual(evs, want) {
		t.Errorf("events = %+v, want %+v", evs, want)
	}
}

func TestBody_ReasoningRoundTrip(t *testing.T) {
	body, err := buildBody(chat.Request{Model: "m", Messages: []chat.Message{
		{Role: chat.RoleUser, Content: "q"},
		{Role: chat.RoleAssistant, Content: "a", Reasoning: "why"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if v := got.Messages[1]["reasoning_content"]; v != "why" {
		t.Errorf("reasoning_content = %v, want why", v)
	}
}

func TestBody_NoReasoningKeyWhenEmpty(t *testing.T) {
	body, err := buildBody(userReq("q"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "reasoning") {
		t.Errorf("body has a reasoning key: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestClient_Health(t *testing.T) {
	t.Run("2xx is nil and sends a one-token request", func(t *testing.T) {
		got := make(chan map[string]any, 1)
		c := rawClient(t, "", func(w http.ResponseWriter, r *http.Request) {
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			got <- b
			w.WriteHeader(http.StatusOK)
		})
		if err := c.Health(t.Context(), "qwen"); err != nil {
			t.Fatalf("Health: %v", err)
		}
		b := <-got
		msgs, _ := b["messages"].([]any)
		if b["model"] != "qwen" || b["max_tokens"] != float64(1) || b["stream"] != false || len(msgs) != 1 {
			t.Errorf("body = %v", b)
		}
	})

	t.Run("5xx is an error with the status", func(t *testing.T) {
		c := rawClient(t, "", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "boom")
		})
		err := c.Health(t.Context(), "m")
		if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
			t.Errorf("err = %v, want status 500 with body", err)
		}
	})

	t.Run("hanging server ends on ctx cancel", func(t *testing.T) {
		arrived := make(chan struct{}, 1)
		c := rawClient(t, "", func(_ http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body) // the server notices a disconnect only after the body is read
			arrived <- struct{}{}
			<-r.Context().Done()
		})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		errc := make(chan error, 1)
		go func() { errc <- c.Health(ctx, "m") }()
		<-arrived
		cancel()
		if err := <-errc; err == nil {
			t.Error("Health: nil error after cancel, want error")
		}
	})
}

func TestChat_NewFieldsNotOnTheWire(t *testing.T) {
	plain := []chat.Message{
		{Role: chat.RoleSystem, Content: "sys"},
		{Role: chat.RoleUser, Content: "go"},
		{Role: chat.RoleAssistant, ToolCalls: []chat.ToolCall{{ID: "c1", Name: "shell", Args: `{"command":"ls"}`}}},
		{Role: chat.RoleTool, Content: "full output", ToolCallID: "c1", Name: "shell"},
		{Role: chat.RoleAssistant, Content: "done"},
	}
	extra := make([]chat.Message, len(plain))
	copy(extra, plain)
	extra[1].TurnStart, extra[1].Injected, extra[1].Seq = true, true, 2
	extra[3].Brief, extra[3].Stub, extra[3].Label, extra[3].Failed, extra[3].Seq = "exit 0", "[exit 0]", "ls", true, 4
	extra[4].Summary, extra[4].Seq = true, 5

	want, err := buildBody(chat.Request{Model: "m", Messages: plain, MaxTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	got, err := buildBody(chat.Request{Model: "m", Messages: extra, MaxTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("new fields changed the wire JSON:\n got  %s\n want %s", got, want)
	}
}

// ---------------------------------------------------------------------------
// Streamed tool-call arguments are reported (R-61)
// ---------------------------------------------------------------------------

func TestStream_ToolArgsEmitToolDelta(t *testing.T) {
	call := chat.ToolCall{ID: "c1", Name: "write_file", Args: `{"path":"a.txt","content":"hello world"}`}
	srv := llmtest.New(t, llmtest.Reply{ToolCalls: []chat.ToolCall{call}, SplitArgs: true})
	evs := streamAll(t, New(srv.URL, "", nil), chat.Request{Model: "m", Messages: []chat.Message{{Role: chat.RoleUser, Content: "go"}}})

	var args strings.Builder
	n := 0
	for _, ev := range evs {
		if ev.Kind == chat.EventToolDelta {
			n++
			args.WriteString(ev.Text)
		}
	}
	if n < 2 || args.String() != call.Args {
		t.Errorf("%d EventToolDelta with text %q, want 3 chunks concatenating to %q", n, args.String(), call.Args)
	}
	last := evs[len(evs)-1]
	if last.Kind != chat.EventDone || len(last.ToolCalls) != 1 || last.ToolCalls[0].Args != call.Args {
		t.Errorf("EventDone = %+v, want the complete call", last)
	}
}
