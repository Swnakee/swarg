// Package llmtest is a fake OpenAI-compatible chat server for tests. It replays
// scripted replies in the stream format of mlx_lm.server and records requests.
package llmtest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"unicode/utf8"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Scripted replies
// ---------------------------------------------------------------------------

// Reply is one scripted response to a chat completion request.
type Reply struct {
	Keepalives int      // ": keepalive i/N" comment lines sent first
	Reasoning  []string // delta.reasoning_content chunks, sent before Chunks
	Chunks     []string // delta contents, in order
	GateAfter  int      // with Gate: send Chunks[:GateAfter], then wait for Gate (0..len(Chunks))
	Gate       chan struct{}
	ToolCalls  []chat.ToolCall // sent after the chunks, one chunk per call, index = position
	SplitArgs  bool            // send each call's arguments in 3 chunks (OpenRouter style)
	Finish     string          // finish_reason: "stop" or "length"; default "tool_calls" if ToolCalls, else "stop"
	Usage      *chat.Usage     // final usage chunk, only if non-nil
	Status     int             // non-zero and not 200: error response with body "fake error" (or Body)
	Body       string          // the error body sent with Status; "" = "fake error"
	RetryAfter int             // Retry-After header (seconds) sent with Status, 0 = none
	NoDone     bool            // end without finish chunk and [DONE] (truncated stream)
	Malformed  bool            // one invalid JSON data line after the chunks
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

// Model is one entry of the fake's model list (GET /v1/models), OpenRouter-shaped when Name,
// Context, prices or Parameters are set; a plain id otherwise.
type Model struct {
	ID, Name           string
	Context            int
	Prompt, Completion string   // USD per token as OpenRouter writes them, e.g. "0.0000001"; "" = omitted
	Parameters         []string // supported_parameters; nil = omitted
}

// Key is the fake's key info (GET /v1/key), OpenRouter-shaped: nil Limit and Remaining are written
// as null (a key without a limit). Status non-zero answers that error instead (Body = its text).
type Key struct {
	Label                string
	Limit, Remaining     *float64
	Usage, Daily, Weekly float64
	Monthly              float64
	Status               int
	Body                 string
}

// Server is a running fake server.
type Server struct {
	URL    string  // base URL ending in /v1
	Models []Model // answered at GET /v1/models; set before the first call
	Key    *Key    // answered at GET /v1/key (OpenRouter key info); nil = 404; set before the first call

	mu          sync.Mutex
	replies     []Reply
	next        int
	requests    []map[string]any
	completedAt []int
	completed   int
	gone        chan struct{}
}

// New starts a fake server that answers POST /v1/chat/completions with replies
// in order. Requests beyond the replies get 500. The server is closed when t ends.
func New(t testing.TB, replies ...Reply) *Server {
	s := &Server{replies: replies, gone: make(chan struct{}, 64)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.serve)
	mux.HandleFunc("GET /v1/models", s.serveModels)
	mux.HandleFunc("GET /v1/key", s.serveKey)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s.URL = srv.URL + "/v1"
	return s
}

// Requests returns the decoded JSON bodies of all requests, in arrival order.
func (s *Server) Requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.requests...)
}

// CompletedAt returns, for each request, the Completed value when it arrived.
func (s *Server) CompletedAt() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.completedAt...)
}

// Completed returns the number of responses fully written.
func (s *Server) Completed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completed
}

// Gone receives one value each time a client disconnected before its reply was fully written.
func (s *Server) Gone() <-chan struct{} { return s.gone }

// serve records the request, takes the next reply and writes it.
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, body)
	s.completedAt = append(s.completedAt, s.completed)
	ok := s.next < len(s.replies)
	var rep Reply
	if ok {
		rep = s.replies[s.next]
		s.next++
	}
	s.mu.Unlock()

	written := true
	switch {
	case !ok:
		http.Error(w, "no reply scripted", http.StatusInternalServerError)
	case rep.Status != 0 && rep.Status != http.StatusOK:
		if rep.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(rep.RetryAfter))
		}
		w.WriteHeader(rep.Status)
		body := rep.Body
		if body == "" {
			body = "fake error"
		}
		_, err := io.WriteString(w, body)
		written = err == nil
	default:
		written = streamReply(r.Context(), w, rep)
	}
	if written {
		s.mu.Lock()
		s.completed++
		s.mu.Unlock()
		return
	}
	select {
	case s.gone <- struct{}{}:
	default:
	}
}

// serveModels answers the model list in the OpenRouter shape (plain entries carry only "id").
func (s *Server) serveModels(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	models := s.Models
	s.mu.Unlock()
	data := []map[string]any{}
	for _, m := range models {
		e := map[string]any{"id": m.ID}
		if m.Name != "" {
			e["name"] = m.Name
		}
		if m.Context > 0 {
			e["context_length"] = m.Context
		}
		if m.Prompt != "" || m.Completion != "" {
			e["pricing"] = map[string]string{"prompt": m.Prompt, "completion": m.Completion}
		}
		if m.Parameters != nil {
			e["supported_parameters"] = m.Parameters
		}
		data = append(data, e)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}

// serveKey answers the key info in the OpenRouter shape; without Key it is a 404.
func (s *Server) serveKey(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	k := s.Key
	s.mu.Unlock()
	switch {
	case k == nil:
		http.NotFound(w, nil)
	case k.Status != 0 && k.Status != http.StatusOK:
		w.WriteHeader(k.Status)
		_, _ = io.WriteString(w, k.Body)
	default:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"label": k.Label, "limit": k.Limit, "limit_remaining": k.Remaining, "usage": k.Usage,
			"usage_daily": k.Daily, "usage_weekly": k.Weekly, "usage_monthly": k.Monthly, "is_free_tier": false,
		}})
	}
}

// ---------------------------------------------------------------------------
// SSE writing
// ---------------------------------------------------------------------------

// streamReply writes rep as an event stream. It reports false if the client went away.
func streamReply(ctx context.Context, w http.ResponseWriter, rep Reply) bool {
	w.Header().Set("Content-Type", "text/event-stream")
	st := &stream{w: w, rc: http.NewResponseController(w)}
	for i := 1; i <= rep.Keepalives; i++ {
		st.send(fmt.Sprintf(": keepalive %d/%d\n\n", i, rep.Keepalives))
	}
	for _, r := range rep.Reasoning {
		st.json(reasoningFrame(r))
	}
	for i := 0; i <= len(rep.Chunks); i++ {
		if rep.Gate != nil && i == rep.GateAfter {
			select {
			case <-rep.Gate:
			case <-ctx.Done():
				return false
			}
		}
		if i < len(rep.Chunks) {
			st.json(deltaFrame(rep.Chunks[i]))
		}
	}
	for i, tc := range rep.ToolCalls {
		for _, d := range toolDeltas(i, tc, rep.SplitArgs) {
			st.json(toolFrame(d))
		}
	}
	if rep.Malformed {
		st.send("data: {not json\n\n")
	}
	if rep.NoDone {
		return st.err == nil
	}
	finish := rep.Finish
	if finish == "" {
		finish = "stop"
		if len(rep.ToolCalls) > 0 {
			finish = "tool_calls"
		}
	}
	st.json(finishFrame(finish))
	if rep.Usage != nil {
		st.json(usageFrame(*rep.Usage))
	}
	st.send("data: [DONE]\n\n")
	return st.err == nil
}

// stream writes SSE frames and flushes each one. The first write error is kept.
type stream struct {
	w   http.ResponseWriter
	rc  *http.ResponseController
	err error
}

// send writes one raw frame and flushes it.
func (st *stream) send(frame string) {
	if st.err != nil {
		return
	}
	if _, st.err = io.WriteString(st.w, frame); st.err == nil {
		st.err = st.rc.Flush()
	}
}

// json writes v as one "data:" event.
func (st *stream) json(v any) {
	b, _ := json.Marshal(v) // frame types always marshal
	st.send("data: " + string(b) + "\n\n")
}

// ---------------------------------------------------------------------------
// JSON frames (field order and shape follow mlx_lm.server)
// ---------------------------------------------------------------------------

type frame struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Model   string      `json:"model"`
	Created int         `json:"created"`
	Choices []choice    `json:"choices"`
	Usage   *tokenUsage `json:"usage,omitempty"`
}

type choice struct {
	Index        int     `json:"index"`
	FinishReason *string `json:"finish_reason"`
	Delta        delta   `json:"delta"`
}

type delta struct {
	Role      string          `json:"role"`
	Content   string          `json:"content,omitempty"`
	Reasoning string          `json:"reasoning_content,omitempty"`
	ToolCalls []toolCallDelta `json:"tool_calls,omitempty"`
}

type toolCallDelta struct {
	Function toolFunction `json:"function"`
	Type     string       `json:"type,omitempty"`
	ID       string       `json:"id,omitempty"`
	Index    int          `json:"index"`
}

type toolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

type tokenUsage struct {
	PromptTokens     int           `json:"prompt_tokens"`
	CompletionTokens int           `json:"completion_tokens"`
	TotalTokens      int           `json:"total_tokens"`
	PromptDetails    promptDetails `json:"prompt_tokens_details"`
	Cost             float64       `json:"cost,omitempty"`
}

type promptDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

func newFrame(object string, choices []choice) frame {
	return frame{ID: "chatcmpl-x", Object: object, Model: "m", Created: 1, Choices: choices}
}

func deltaFrame(content string) frame {
	return newFrame("chat.completion.chunk", []choice{{Delta: delta{Role: "assistant", Content: content}}})
}

func reasoningFrame(text string) frame {
	return newFrame("chat.completion.chunk", []choice{{Delta: delta{Role: "assistant", Reasoning: text}}})
}

func toolFrame(d toolCallDelta) frame {
	return newFrame("chat.completion.chunk", []choice{{Delta: delta{Role: "assistant", ToolCalls: []toolCallDelta{d}}}})
}

func finishFrame(reason string) frame {
	return newFrame("chat.completion.chunk", []choice{{FinishReason: &reason, Delta: delta{Role: "assistant"}}})
}

// toolDeltas returns the chunks of one tool call: one chunk with the whole arguments, or
// with split, three chunks whose arguments concatenate to the original. Only the first
// chunk carries id, type and name.
func toolDeltas(index int, tc chat.ToolCall, split bool) []toolCallDelta {
	pieces := []string{tc.Args}
	if split {
		pieces = splitThirds(tc.Args)
	}
	out := make([]toolCallDelta, len(pieces))
	for i, p := range pieces {
		out[i] = toolCallDelta{Index: index, Function: toolFunction{Arguments: p}}
	}
	out[0].ID, out[0].Type, out[0].Function.Name = tc.ID, "function", tc.Name
	return out
}

// splitThirds cuts s into three pieces of about equal size. Cuts move forward to the next
// rune start, so no piece holds half a UTF-8 character.
func splitThirds(s string) []string {
	cut := func(i int) int {
		for i < len(s) && !utf8.RuneStart(s[i]) {
			i++
		}
		return i
	}
	a, b := cut(len(s)/3), cut(2*len(s)/3)
	return []string{s[:a], s[a:b], s[b:]}
}

func usageFrame(u chat.Usage) frame {
	f := newFrame("chat.completion", []choice{})
	f.Usage = &tokenUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.PromptTokens + u.CompletionTokens,
		PromptDetails:    promptDetails{CachedTokens: u.CachedTokens},
		Cost:             u.Cost,
	}
	return f
}
