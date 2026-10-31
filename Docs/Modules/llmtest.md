# Module llmtest (`App/internal/llm/llmtest`)

Purpose: fake OpenAI-compatible chat server for tests. It replays scripted replies in the exact
stream format of mlx_lm.server and records the requests. Test helper only; production code never
imports it.

API:
- `Reply{Keepalives, Reasoning, Chunks, GateAfter, Gate, ToolCalls, SplitArgs, Finish, Usage, Status, NoDone, Malformed}` - one scripted response.
- `Reply.Finish` - finish_reason. Default: `"tool_calls"` if `ToolCalls` is non-empty, else `"stop"`. An explicit value always wins (e.g. `"length"`).
- `Reply.ToolCalls []chat.ToolCall` - sent after the chunks, one chunk per call (mlx format: whole `arguments`, `index` = position, `id`, `type`, `name` in the same chunk).
- `Reply.SplitArgs` - each call is sent in 3 chunks (OpenRouter style): the first has id, type, name and a third of `arguments`; the next two have only `index` and the next pieces of `function.arguments`.
- `Reply.Usage *chat.Usage` - final usage chunk, written only if non-nil; a positive `Usage.Cost` is written as `usage.cost` (T13).
- `Server.Key *Key` (T13) - the answer of `GET /v1/key` in the OpenRouter shape (`data.{label, limit, limit_remaining, usage, usage_daily, usage_weekly, usage_monthly}`); `Key{Label, Limit, Remaining *float64, Usage, Daily, Weekly, Monthly, Status, Body}`, nil `Limit`/`Remaining` are `null`, non-zero `Status` answers that error with `Body`; nil `Key` = 404. Set before the first call.
- `Reply.Status` - non-zero and not 200: error response with body `fake error`, or `Reply.Body` when set; `Reply.RetryAfter` (seconds) adds a `Retry-After` header (T11: the fake OpenRouter's 429/401/402/404).
- `Reply.Reasoning` - chunks streamed as `delta.reasoning_content` frames before the text `Chunks`.
- `Reply.Gate` + `GateAfter` - send `Chunks[:GateAfter]`, then block until the gate is closed or the request is cancelled.
- `Reply.NoDone` - end the stream without finish chunk and `[DONE]`.
- `Reply.Malformed` - one invalid `data:` line after the chunks.
- `New(t testing.TB, replies ...Reply) *Server` - starts an httptest server; replies in order; extra requests get 500; closed by `t.Cleanup`.
- `Server.URL` - base URL ending in `/v1`; serves `POST /v1/chat/completions` and `GET /v1/models`.
- `Server.Models []Model` (T11) - the model list: `Model{ID, Name, Context, Prompt, Completion, Parameters}`; set fields are written in the OpenRouter shape (`name`, `context_length`, `pricing{prompt, completion}`, `supported_parameters`), a plain `Model{ID}` gives `{"id"}` only. Set before the first call.
- `Server.Requests() []map[string]any` - decoded request bodies in arrival order.
- `Server.CompletedAt() []int` - value of `Completed()` when each request arrived.
- `Server.Completed() int` - responses fully written.
- `Server.Gone() <-chan struct{}` - receives one value per client that disconnected before its reply was fully written (buffer 64, extra signals dropped); lets a test wait for a disconnect without sleeping.

Invariants:
- Output bytes match mlx_lm.server: keepalive comments, one event per chunk, tool call chunks, finish chunk, usage chunk (only with Usage), `[DONE]`, a blank line after each event, flush after each event.
- Tool call chunks follow the text chunks and precede `Malformed` / `NoDone`; `NoDone` still sends them.
- JSON is built with `encoding/json`; quotes, newlines and Cyrillic in content are safe.
- Replies are consumed in order; each request takes exactly one reply.
- A response counts in `Completed` only when the handler finished writing it without a client error. Error replies (`Status`), extra 500s and `NoDone` count.
- A request blocked on a gate returns when the client disconnects, so `Close` never hangs on it.

Gotchas:
- `GateAfter` must be within `0..len(Chunks)`; a larger value never gates.
- `SplitArgs` cuts the arguments on UTF-8 rune starts, so the three pieces always concatenate to the original; with empty `Args` the first piece is empty and the other two are too (still 3 chunks).
- `Requests()` and `CompletedAt()` include requests that got a 500 (no reply left). A request whose body is not JSON gets 400 and is not recorded.
- The fake always reports model `m`, id `chatcmpl-x`, created `1`, whatever the request asked for.
- `Completed` is incremented before the handler returns, so after the client reads EOF the counter is already up to date.
