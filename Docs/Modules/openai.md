# Module openai (`App/internal/llm/openai`)

Purpose: streaming client for OpenAI-compatible chat completion servers (mlx_lm.server in this
project). Turns the SSE stream into `chat.StreamEvent`s. Satisfies `agent.Provider` by shape; the
package does not import `agent`.

API:
- `New(baseURL, apiKey string, hc *http.Client) *Client` - nil `hc` gets a cloned default transport with a 5 min response header timeout and no overall timeout. A trailing `/` in `baseURL` is trimmed.
- `Client.IdleTimeout time.Duration` - exported field, default 600 s (main sets it from the provider's `idle_timeout_s`, R-63). No bytes for this long ends the stream with an idle error. mlx_lm.server sends a tool call in ONE chunk at the end of the generation: a long `write_file` at a few tok/s is minutes of silence, which is why the default is long.
- `(*Client).Stream(ctx, chat.Request) (<-chan chat.StreamEvent, error)` - POSTs `{base}/chat/completions`; returns `(nil, err)` if the request cannot start.
- `(*Client).Health(ctx, model string) error` - non-streaming POST `{model, messages:[{role:user,content:"hi"}], max_tokens:1, stream:false}`; nil on 2xx, else an error with status and body snippet. Uses its own 30 s timeout on top of ctx.
- `(*Client).Catalog(ctx) ([]ModelInfo, error)` (T11, R-102) - `GET {base}/models` parsed as `ModelInfo{ID, Name, Context, Prompt, Completion, Tools, Reasoning, HasParameters}`: `name`, `context_length`, `pricing.prompt/completion` (USD per token, strings on the wire), `supported_parameters` (`Tools` = has `tools`, `Reasoning` = has `reasoning`; `HasParameters` says the field was present). Sorted by id, blanks and duplicates dropped; a plain server fills only `ID`; body cap 16 MB (OpenRouter lists ~500 models); 10 s timeout. `ModelInfo.Free()` = both prices zero. `(*Client).Models(ctx) ([]string, error)` - the Catalog's ids.
- `(*Client).KeyInfo(ctx) (KeyInfo, error)` (T13, R-113) - `GET {base}/key` (OpenRouter): `KeyInfo{Label, Limit, Remaining *float64 (nil = no limit), Usage, Daily, Weekly, Monthly}` from `data.{label, limit, limit_remaining, usage, usage_daily, usage_weekly, usage_monthly}`; 10 s timeout; errors come from the shared status texts, which never contain the key.
- Status texts (`statusError`, T11, R-103, I-35): 401 `the model server refused the API key (<status>): <snippet>`, 402 `no credits on the model server (...)`, 404 `the model server has no such model (...): <snippet>; /model lists the available ones`, 429 `the model server is rate limited (<status>, retry after N s)` or `... try again later`, 5xx `the model server failed upstream (...)`, else `model server returned <status>: <snippet>`. A 429 with `Retry-After` <= 30 s (`retryWaitMax`) is retried ONCE after the wait (ctx-aware); a longer wait, a missing header or a second 429 is reported. The key is never in a text.
- `sse.go` (unexported): `readSSE`, `sseEvent{Comment, Data}` - SSE line parser.

OpenRouter reports `usage.cost` in every response by itself: `usage: {include: true}` and `stream_options.include_usage` are deprecated no-ops there, so the request body is unchanged for cost (docs checked 2026-10-31).

Request body: `{model, messages, stream: true, stream_options: {include_usage: true}, max_tokens, tools}`.
- `max_tokens` is omitted when `MaxTokens` is 0. `Extra` keys go to the top level.
- `tools` is sent only when `Request.Tools` is non-empty: `[{type:"function", function:{name, description, parameters}}]`. A nil `Schema` becomes `{"type":"object","properties":{}}`.
- Message fields: `{role, content}` always (`content` may be `""`). An assistant message with `ToolCalls` adds `tool_calls:[{id, type:"function", function:{name, arguments}}]`; a `RoleTool` message adds `tool_call_id` and `name`. Other fields are omitted when empty.

Event mapping:
- `: keepalive N/M` -> `EventProgress{Done: N, Total: M}`; other comments and malformed keepalives are ignored.
- non-empty `choices[0].delta.content` -> `EventDelta`.
- non-empty `delta.reasoning_content` (else `delta.reasoning`, never both) -> `EventReasoning{Text}`, emitted before the content delta of the same chunk.
- `finish_reason` -> remembered; `"tool_calls"` -> `FinishToolCalls`, `"length"` -> `FinishLength`, anything else -> `FinishStop`.
- `delta.tool_calls[]` -> accumulated per call (see below); nothing is emitted until `[DONE]`.
- chunk with `usage` -> `EventUsage` (cached tokens from `prompt_tokens_details.cached_tokens`; `Usage.Cost` from `usage.cost`, OpenRouter's charge in USD, read leniently: absent, null, non-numeric or <= 0 gives 0 and never an error, I-39).
- `data: [DONE]` -> `EventDone{remembered finish, ToolCalls}`. `ToolCalls` is nil or holds every call accumulated in the stream, sorted by wire index (stable), whatever the finish reason.
- Tool-call accumulation: a delta without `index` counts as index 0. It extends the latest call with that index. If the delta carries a non-empty `id` different from that call's id, a new call starts instead (index-0 parallel calls). `id` and `function.name` are set when present; `function.arguments` is appended. The client never generates ids: a missing id stays `""` and the agent fills it.
- EOF before `[DONE]`, malformed JSON, read error, idle timeout -> `EventDone{FinishError, Err}`.

Invariants:
- Errors before the stream (connection refused, non-2xx) come as `(nil, err)`: `cannot reach the model server at <base>: <err>` or `model server returned <status>: <body snippet, max 512 bytes>`.
- After a successful start, a stream ends with exactly one `EventDone`, unless ctx is cancelled first. Then the channel closes.
- The channel closes only after the body is drained and closed, so a consumer that sees the close also sees the server finish writing (the llmtest `Completed` counter is up to date).
- Every send is `select` on ctx, so a consumer that stops reading does not leak the goroutine after cancel.
- Unknown JSON fields are ignored; `Choices` is read at index 0 only; no panics on any input.
- `model`, `messages`, `stream`, `stream_options` and `tools` always win over `Extra`.
- Every channel send in the pump (deltas, progress, usage, done) selects on ctx, so a cancel while the consumer is not reading still closes the channel.

Gotchas:
- Outgoing assistant messages carry the reasoning under two keys, `reasoning_content` (mlx templates) and `reasoning` (Ollama, R-64), both from `Message.Reasoning` and omitted when empty, so the template reproduces the prompt prefix and the server cache hits. A strict OpenAI server may reject the keys; mlx_lm.server and Ollama ignore the one they do not know.
- Ollama (`/v1`) streams reasoning as `delta.reasoning` (the fallback name), sends usage in the last chunk, and reads only `temperature`, `top_p`, `presence_penalty`, `frequency_penalty`, `seed`, `stop`, `max_tokens`, `reasoning_effort` from the body; other `Extra` keys are ignored silently.
- mlx_lm.server sends `: keepalive i/N` comments during prompt prefill. They count as bytes for the idle timer, so a long prefill does not trip `IdleTimeout`. Keep that in mind before changing the idle logic.
- The idle timer runs only while a `Read` is blocked; a slow consumer (blocked on the events channel) does not trigger it.
- The idle error text uses `time.Duration.String()`, so the default reads `idle 10m0s`.
- `GET /v1/models` on mlx_lm.server lists all cached models, not only the loaded one (the server loads the requested one per request, so any of them can be chosen). Do not use it to discover the served model (the e2e runner reads the process argv instead).
- The mlx server is single-lane. Owner rule: never disconnect mid-generation. Cancel the ctx only at shutdown; the agent never cancels a request on interrupt.
- The server may send usage in a chunk with empty `choices`; that chunk yields `EventUsage` only.
- mlx_lm.server sends each tool call whole in one delta (`index`, `id`, full `arguments`), then `finish_reason: "tool_calls"`.
- OpenRouter-style servers split one call into pieces: the first piece has `id` and `name`, the rest only `arguments`. Concatenation makes the call.
- Some servers send every parallel call with `index: 0`, each with its own `id`. The id change starts a new call; without it the calls would merge into one.
- Some servers finish tool calls with `finish_reason: "stop"`. The calls are still delivered in `ToolCalls`; the finish stays `FinishStop`.
- `arguments` is passed through raw. Malformed JSON is not caught here; the agent normalizes it before storing (mlx re-parses stored arguments on every later request).

## Per-request key (T12, R-107)

`Client.Key func() string` (optional): when set it supplies the API key on every request instead of the string given to `New`, so a key set or rotated in `/keys` is used by the client the running Source already holds. `cmd/swarg/sources.go` sets it to `Provider.APIKey`. An empty result sends no `Authorization`.
