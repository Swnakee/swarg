# Module chat (`App/internal/chat`)

Purpose: shared domain types of a conversation. No dependencies; every other module may import it.

API:
- `Role`, `RoleSystem|RoleUser|RoleAssistant|RoleTool` - message author.
- `ToolCall{ID, Name, Args}` - a call requested by the model (`Args` raw JSON); `ToolSpec{Name, Description, Schema}` - a tool offered to the model.
- `Message{Role, Content, Reasoning, Truncated, Interrupted, ToolCalls, ToolCallID, Name}` - one history entry; assistant messages carry `ToolCalls`, tool messages carry `ToolCallID` + `Name`. `Reasoning` (assistant) is the model's reasoning, a wire field sent back as `reasoning_content`.
- History-only fields of `Message` (never sent to the API; the openai client builds the wire message field by field, proved by `TestChat_NewFieldsNotOnTheWire`): `Brief` (tool: one-line result for the user), `Stub` (tool: the text a request sends instead of `Content`; `Content` stays in history; the agent substitutes it when building a request and keeps `Stub` set so the estimate can tell stubs from full results), `Summary` (assistant: a WorkSummary), `Seq` (stable id assigned at append, 0 = unassigned), `TurnStart` (user: started a Turn), `Injected` (user: queued text injected mid-Turn), `Label` and `Failed` (tool: call description and error flag for the resumed UI). Plain `json` round trip keeps all of them.
- `Patch{Seq, Stub, Args}` - a change of a stored message (stub or shrunk call arguments, call id -> raw JSON); the agent's `Store.Patch` and `session.Patch` share it.
- `FinishReason`: `FinishStop|FinishLength|FinishInterrupted|FinishError|FinishToolCalls`.
- `Usage{PromptTokens, CompletionTokens, CachedTokens}` - server token accounting.
- `StreamEvent{Kind, Text, Done, Total, Usage, Finish, Err}` with `EventDelta|EventProgress|EventUsage|EventDone|EventReasoning` (`Text` carries a reasoning chunk).
- `EstimateTokens(s) int` - `(len(s)+3)/4` bytes; the only token estimator in the project. `EstimateBytes(n)` is the same for a byte count.
- `StreamEvent.ToolCalls` - set on `EventDone` when the model requested tools.
- `Request{Model, Messages, MaxTokens, Extra, Tools}` - `Messages[0]` is the system prompt; `Extra` is merged into the JSON body.

Invariants:
- A stream ends with exactly one `EventDone`, then the channel is closed.
- Every assistant `ToolCalls` entry in a history is followed by exactly one tool message per call.
- Types only; no behavior lives here.
