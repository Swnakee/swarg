// Package chat holds the shared domain types of a conversation with a model:
// messages, streamed completion events and requests. It has no dependencies.
package chat

import "encoding/json"

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

// Role is the author of a Message.
type Role string

// Message roles.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one entry of the history.
type Message struct {
	Role        Role
	Content     string
	Reasoning   string // assistant: the model's reasoning, sent back as reasoning_content
	Truncated   bool   // ended by the token limit (finish_reason "length")
	Interrupted bool   // the user interrupted the answer

	ToolCalls  []ToolCall // assistant: tools the model asked to run
	ToolCallID string     // tool: the call this result answers
	Name       string     // tool: the tool name

	// History-only fields. None of them is sent to the API (the openai client builds the
	// wire message field by field); the session file stores them.
	Brief     string // tool: the one-line result shown to the user
	Stub      string // tool: when set, the request sends it instead of Content (Content stays in history)
	Summary   bool   // assistant: a WorkSummary that replaced a WorkSpan
	Seq       int    // stable id assigned at append; 0 = not assigned
	TurnStart bool   // user: the message that started a Turn
	Injected  bool   // user: a queued message injected mid-Turn
	Label     string // tool: one-line description of the call (resumed UI)
	Failed    bool   // tool: the call ended with an error (resumed UI)
}

// Patch changes a stored message after the fact: a Stub set by a stub batch and/or shrunk
// arguments of tool calls (call id -> raw JSON). The agent hands batches of them to its Store.
type Patch struct {
	Seq  int
	Stub string
	Args map[string]string
}

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

// ToolCall is one tool invocation requested by the model.
type ToolCall struct {
	ID   string
	Name string
	Args string // raw JSON arguments as sent by the model
}

// ToolSpec describes a tool to the model.
type ToolSpec struct {
	Name        string
	Description string
	Schema      json.RawMessage // JSON Schema object of the arguments
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

// FinishReason tells how a streamed completion ended.
type FinishReason string

// Finish reasons.
const (
	FinishStop        FinishReason = "stop"
	FinishLength      FinishReason = "length"
	FinishInterrupted FinishReason = "interrupted"
	FinishError       FinishReason = "error"
	FinishToolCalls   FinishReason = "tool_calls"
)

// Usage is the token and cost accounting reported by the server.
type Usage struct {
	PromptTokens, CompletionTokens, CachedTokens int
	Cost                                         float64 // USD charged for the response, when the server reports it (OpenRouter usage.cost); 0 = free or unknown
}

// EventKind is the kind of a StreamEvent.
type EventKind int

// Stream event kinds.
const (
	EventDelta EventKind = iota + 1
	EventProgress
	EventUsage
	EventDone
	EventReasoning // Text carries a reasoning chunk (delta.reasoning_content)
	EventToolDelta // Text carries a chunk of a tool call's arguments (R-61); the complete call comes with EventDone
)

// StreamEvent is one event of a streamed completion. A stream always ends with
// exactly one EventDone, after which the channel is closed.
type StreamEvent struct {
	Kind   EventKind
	Text   string       // EventDelta, EventReasoning, EventToolDelta
	Done   int          // EventProgress: prefill progress Done of Total
	Total  int          // EventProgress
	Usage  Usage        // EventUsage
	Finish FinishReason // EventDone
	Err    error        // EventDone with FinishError

	ToolCalls []ToolCall // EventDone: complete tool calls in index order
}

// Request is one chat completion request.
type Request struct {
	Model     string
	Messages  []Message // Messages[0] is the system prompt (RoleSystem)
	MaxTokens int
	Extra     map[string]any // merged into the JSON body top level
	Tools     []ToolSpec     // tools the model may call
}

// ---------------------------------------------------------------------------
// Token estimate
// ---------------------------------------------------------------------------

// EstimateTokens is the project's only token estimate: about 4 bytes per token,
// rounded up. Used before the server reports exact usage.
func EstimateTokens(s string) int { return EstimateBytes(len(s)) }

// EstimateBytes is EstimateTokens for text of n bytes.
func EstimateBytes(n int) int { return (n + 3) / 4 }
