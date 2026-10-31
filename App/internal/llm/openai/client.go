package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"swarg/internal/chat"
)

// Client satisfies agent.Provider (defined in internal/agent; not imported here to avoid a cycle).

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

const (
	defaultIdleTimeout = 600 * time.Second // a tool call comes in one chunk at the end of the generation (R-63)
	healthTimeout      = 30 * time.Second
	errorSnippetBytes  = 512
)

// Client streams chat completions from an OpenAI-compatible server.
type Client struct {
	// IdleTimeout ends a stream that receives no bytes for this long. Keepalives count as bytes.
	IdleTimeout time.Duration
	// Key, when set, supplies the API key on every request instead of the one given to New, so a
	// key set or rotated while swarg runs is used at once (R-107). "" sends no Authorization.
	Key func() string

	baseURL string
	apiKey  string
	hc      *http.Client
}

// New returns a Client for baseURL (for example http://127.0.0.1:8080/v1). A nil hc gets a
// transport with a 5 minute response header timeout and no overall timeout.
func New(baseURL, apiKey string, hc *http.Client) *Client {
	if hc == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.ResponseHeaderTimeout = 5 * time.Minute
		hc = &http.Client{Transport: tr}
	}
	return &Client{
		IdleTimeout: defaultIdleTimeout,
		baseURL:     strings.TrimSuffix(baseURL, "/"),
		apiKey:      apiKey,
		hc:          hc,
	}
}

// Stream starts a streamed completion. It returns an error if the request cannot start.
// Otherwise the channel carries events and ends with exactly one EventDone, then closes.
func (c *Client) Stream(ctx context.Context, req chat.Request) (<-chan chat.StreamEvent, error) {
	body, err := buildBody(req)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	resp, err := c.post(ctx, body, "text/event-stream")
	if err != nil {
		return nil, err
	}
	ch := make(chan chat.StreamEvent)
	go pump(ctx, resp.Body, c.IdleTimeout, ch)
	return ch, nil
}

// Health sends a 1-token non-streaming request for model. It returns nil when the server
// answers with a 2xx status within 30 s.
func (c *Client) Health(ctx context.Context, model string) error {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	body, err := json.Marshal(map[string]any{
		"model":      model,
		"messages":   []wireMessage{{Role: string(chat.RoleUser), Content: "hi"}},
		"max_tokens": 1,
		"stream":     false,
	})
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	resp, err := c.post(ctx, body, "application/json")
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// post sends body to /chat/completions; see do.
func (c *Client) post(ctx context.Context, body []byte, accept string) (*http.Response, error) {
	return c.do(ctx, http.MethodPost, "/chat/completions", body, accept)
}

// do sends one request to base+path (body nil = none). A non-2xx status is returned as an error
// that carries a snippet of the response body (statusError); the body is then already closed. A
// 429 with a short Retry-After is retried once after the wait (R-103), never more.
func (c *Client) do(ctx context.Context, method, path string, body []byte, accept string) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := c.once(ctx, method, path, body, accept)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			return resp, nil
		}
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, errorSnippetBytes))
		resp.Body.Close()
		wait := retryAfter(resp)
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 && wait > 0 && wait <= retryWaitMax {
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return nil, statusError(resp.Status, resp.StatusCode, strings.TrimSpace(string(snippet)), wait)
	}
}

// once sends one request; the caller reads the status.
func (c *Client) once(ctx context.Context, method, path string, body []byte, accept string) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	hreq, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		hreq.Header.Set("Content-Type", "application/json")
	}
	hreq.Header.Set("Accept", accept)
	key := c.apiKey
	if c.Key != nil {
		key = c.Key()
	}
	if key != "" {
		hreq.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := c.hc.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the model server at %s: %w", c.baseURL, err)
	}
	return resp, nil
}

// retryWaitMax is the longest Retry-After honoured before a 429 is reported instead.
const retryWaitMax = 30 * time.Second

// retryAfter reads a Retry-After header in seconds; 0 when absent or not a number.
func retryAfter(resp *http.Response) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// statusError words a non-2xx status for the user (R-103): the status and the body snippet always,
// a clear cause for the common cloud statuses. The key is never part of it (I-35).
func statusError(status string, code int, snippet string, wait time.Duration) error {
	switch {
	case code == http.StatusUnauthorized:
		return fmt.Errorf("the model server refused the API key (%s): %s", status, snippet)
	case code == http.StatusPaymentRequired:
		return fmt.Errorf("no credits on the model server (%s): %s", status, snippet)
	case code == http.StatusNotFound:
		return fmt.Errorf("the model server has no such model (%s): %s; /model lists the available ones", status, snippet)
	case code == http.StatusTooManyRequests && wait > 0:
		return fmt.Errorf("the model server is rate limited (%s, retry after %d s): %s", status, int(wait/time.Second), snippet)
	case code == http.StatusTooManyRequests:
		return fmt.Errorf("the model server is rate limited (%s): try again later; %s", status, snippet)
	case code >= 500:
		return fmt.Errorf("the model server failed upstream (%s): %s", status, snippet)
	}
	return fmt.Errorf("model server returned %s: %s", status, snippet)
}

// ---------------------------------------------------------------------------
// Request body
// ---------------------------------------------------------------------------

// wireMessage is a message as the server expects it. Tool fields are sent only when set.
type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ReasoningContent and Reasoning both echo Message.Reasoning: mlx templates read
	// reasoning_content, Ollama reads reasoning (R-64). Omitting it changes the prompt prefix and
	// misses the server's cache.
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	Reasoning        string        `json:"reasoning,omitempty"`
	ToolCalls        []wireCallOut `json:"tool_calls,omitempty"`
	ToolCallID       string        `json:"tool_call_id,omitempty"`
	Name             string        `json:"name,omitempty"`
}

// wireCallOut is a tool call in an assistant message.
type wireCallOut struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// wireTool is one entry of the request's tools array.
type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// emptySchema is the parameters object of a tool without a schema.
var emptySchema = json.RawMessage(`{"type":"object","properties":{}}`)

// wireMessages converts chat messages to their wire form.
func wireMessages(in []chat.Message) []wireMessage {
	out := make([]wireMessage, len(in))
	for i, m := range in {
		out[i] = wireMessage{Role: string(m.Role), Content: m.Content, ReasoningContent: m.Reasoning, Reasoning: m.Reasoning, ToolCallID: m.ToolCallID, Name: m.Name}
		for _, tc := range m.ToolCalls {
			var wc wireCallOut
			wc.ID, wc.Type = tc.ID, "function"
			wc.Function.Name, wc.Function.Arguments = tc.Name, tc.Args
			out[i].ToolCalls = append(out[i].ToolCalls, wc)
		}
	}
	return out
}

// wireTools converts tool specs to the request's tools array.
func wireTools(in []chat.ToolSpec) []wireTool {
	out := make([]wireTool, len(in))
	for i, s := range in {
		out[i].Type = "function"
		out[i].Function.Name = s.Name
		out[i].Function.Description = s.Description
		out[i].Function.Parameters = s.Schema
		if len(s.Schema) == 0 {
			out[i].Function.Parameters = emptySchema
		}
	}
	return out
}

// buildBody encodes req. Extra is copied first, so model, messages, stream, stream_options
// and tools always win over it; max_tokens is set only when positive, tools only when given.
func buildBody(req chat.Request) ([]byte, error) {
	body := make(map[string]any, len(req.Extra)+5)
	for k, v := range req.Extra {
		body[k] = v
	}
	body["model"] = req.Model
	body["messages"] = wireMessages(req.Messages)
	body["stream"] = true
	body["stream_options"] = map[string]any{"include_usage": true}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		body["tools"] = wireTools(req.Tools)
	}
	return json.Marshal(body)
}

// ---------------------------------------------------------------------------
// Reader goroutine
// ---------------------------------------------------------------------------

// errDone stops reading after [DONE] has been sent; errCanceled stops it when ctx ends.
var (
	errDone     = errors.New("stream finished")
	errCanceled = errors.New("stream canceled")
)

// pumper turns SSE events into chat.StreamEvents.
type pumper struct {
	ctx    context.Context
	ch     chan<- chat.StreamEvent
	finish chat.FinishReason    // remembered from finish_reason, default stop
	calls  []*pendingCall       // tool calls in arrival order
	latest map[int]*pendingCall // the call that the next delta with an index extends
}

// pendingCall is a tool call assembled from deltas. idx is the wire index it arrived with.
type pendingCall struct {
	idx      int
	id, name string
	args     strings.Builder
}

// pump reads body until the stream ends and sends events on ch. It sends exactly one
// EventDone unless ctx is done, drains and closes body, and closes ch last, so a
// consumer that sees the channel close also sees the body fully read.
func pump(ctx context.Context, body io.ReadCloser, idle time.Duration, ch chan<- chat.StreamEvent) {
	defer close(ch)
	defer func() {
		_, _ = io.Copy(io.Discard, body)
		_ = body.Close()
	}()
	ir := newIdleReader(body, idle)
	defer ir.timer.Stop()
	p := &pumper{ctx: ctx, ch: ch, finish: chat.FinishStop, latest: map[int]*pendingCall{}}
	err := readSSE(ir, p.handle)
	switch {
	case errors.Is(err, errDone), errors.Is(err, errCanceled):
		return
	case ir.timedOut():
		err = fmt.Errorf("model server stopped responding (idle %s)", idle)
	case err == nil:
		err = errors.New("stream ended before [DONE]")
	}
	p.emit(chat.StreamEvent{Kind: chat.EventDone, Finish: chat.FinishError, Err: err})
}

// emit sends ev unless ctx is done first. It returns errCanceled in that case.
func (p *pumper) emit(ev chat.StreamEvent) error {
	select {
	case p.ch <- ev:
		return nil
	case <-p.ctx.Done():
		return errCanceled
	}
}

// handle maps one SSE event to stream events. A returned error stops reading.
func (p *pumper) handle(ev sseEvent) error {
	if ev.Data == "" {
		return p.keepalive(ev.Comment)
	}
	if ev.Data == "[DONE]" {
		if err := p.emit(chat.StreamEvent{Kind: chat.EventDone, Finish: p.finish, ToolCalls: p.toolCalls()}); err != nil {
			return err
		}
		return errDone
	}
	return p.chunk(ev.Data)
}

// keepalive turns a "keepalive N/M" comment into EventProgress. Other comments and
// malformed keepalives are ignored.
func (p *pumper) keepalive(comment string) error {
	var done, total int
	if _, err := fmt.Sscanf(comment, "keepalive %d/%d", &done, &total); err != nil {
		return nil
	}
	return p.emit(chat.StreamEvent{Kind: chat.EventProgress, Done: done, Total: total})
}

// wireChunk is the part of a chat.completion.chunk the client reads.
type wireChunk struct {
	Choices []wireChoice `json:"choices"`
	Usage   *wireUsage   `json:"usage"`
}

type wireChoice struct {
	Delta struct {
		Content          string          `json:"content"`
		ReasoningContent string          `json:"reasoning_content"`
		Reasoning        string          `json:"reasoning"` // fallback of servers that use this name
		ToolCalls        []wireCallDelta `json:"tool_calls"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// wireCallDelta is one entry of delta.tool_calls. Index is nil when the server omits it.
type wireCallDelta struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	Cost json.RawMessage `json:"cost"` // OpenRouter: credits (USD) charged; absent on other servers, read leniently (I-39)
}

// chunk handles one JSON data event: delta text, finish reason and usage.
func (p *pumper) chunk(data string) error {
	var c wireChunk
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		return fmt.Errorf("malformed chunk: %w", err)
	}
	if len(c.Choices) > 0 {
		first := c.Choices[0]
		reasoning := first.Delta.ReasoningContent
		if reasoning == "" {
			reasoning = first.Delta.Reasoning
		}
		if reasoning != "" {
			if err := p.emit(chat.StreamEvent{Kind: chat.EventReasoning, Text: reasoning}); err != nil {
				return err
			}
		}
		if first.Delta.Content != "" {
			if err := p.emit(chat.StreamEvent{Kind: chat.EventDelta, Text: first.Delta.Content}); err != nil {
				return err
			}
		}
		for _, d := range first.Delta.ToolCalls {
			p.addCall(d)
			if d.Function.Arguments != "" {
				if err := p.emit(chat.StreamEvent{Kind: chat.EventToolDelta, Text: d.Function.Arguments}); err != nil {
					return err
				}
			}
		}
		if first.FinishReason != nil {
			p.finish = finishOf(*first.FinishReason)
		}
	}
	if c.Usage == nil {
		return nil
	}
	u := chat.Usage{PromptTokens: c.Usage.PromptTokens, CompletionTokens: c.Usage.CompletionTokens}
	if c.Usage.PromptTokensDetails != nil {
		u.CachedTokens = c.Usage.PromptTokensDetails.CachedTokens
	}
	var f float64
	if json.Unmarshal(c.Usage.Cost, &f) == nil && f > 0 { // absent, null or not a number: no cost, never an error (I-39)
		u.Cost = f
	}
	return p.emit(chat.StreamEvent{Kind: chat.EventUsage, Usage: u})
}

// addCall merges one tool-call delta. A delta extends the latest call with its index,
// unless it carries a different non-empty id: then it starts a new call (servers that
// send every parallel call as index 0 rely on this).
func (p *pumper) addCall(d wireCallDelta) {
	idx := 0
	if d.Index != nil {
		idx = *d.Index
	}
	c := p.latest[idx]
	if c == nil || (d.ID != "" && c.id != "" && d.ID != c.id) {
		c = &pendingCall{idx: idx}
		p.calls = append(p.calls, c)
		p.latest[idx] = c
	}
	if d.ID != "" {
		c.id = d.ID
	}
	if d.Function.Name != "" {
		c.name = d.Function.Name
	}
	c.args.WriteString(d.Function.Arguments)
}

// toolCalls returns the accumulated calls sorted by index (stable), or nil when there are none.
func (p *pumper) toolCalls() []chat.ToolCall {
	if len(p.calls) == 0 {
		return nil
	}
	sort.SliceStable(p.calls, func(i, j int) bool { return p.calls[i].idx < p.calls[j].idx })
	out := make([]chat.ToolCall, len(p.calls))
	for i, c := range p.calls {
		out[i] = chat.ToolCall{ID: c.id, Name: c.name, Args: c.args.String()}
	}
	return out
}

// finishOf maps a server finish_reason to a chat.FinishReason.
func finishOf(reason string) chat.FinishReason {
	switch reason {
	case "length":
		return chat.FinishLength
	case "tool_calls":
		return chat.FinishToolCalls
	}
	return chat.FinishStop
}

// ---------------------------------------------------------------------------
// Idle timeout
// ---------------------------------------------------------------------------

// idleReader closes the body when no Read has returned data within the timeout.
// The timer runs only while a Read is blocked, so a slow consumer does not trigger it.
type idleReader struct {
	body  io.ReadCloser
	timer *time.Timer
	idle  time.Duration
	fired atomic.Bool
}

// newIdleReader wraps body with an idle timeout of d.
func newIdleReader(body io.ReadCloser, d time.Duration) *idleReader {
	ir := &idleReader{body: body, idle: d}
	ir.timer = time.AfterFunc(d, func() {
		ir.fired.Store(true)
		_ = body.Close()
	})
	ir.timer.Stop()
	return ir
}

// Read arms the timer, reads, and disarms the timer.
func (ir *idleReader) Read(p []byte) (int, error) {
	ir.timer.Reset(ir.idle)
	n, err := ir.body.Read(p)
	ir.timer.Stop()
	return n, err
}

// timedOut reports whether the idle timer closed the body.
func (ir *idleReader) timedOut() bool {
	return ir.fired.Load()
}
