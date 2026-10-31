# Module mcp (`App/internal/mcp`)

Purpose: the MCP client. Starts the configured MCP Servers (stdio subprocesses by `command`,
Streamable HTTP endpoints by `url`), offers their tools to the model as `mcp__<server>__<tool>`
and runs the calls (R-85..R-88, R-91..R-94). A failing or crashing server never stops swarg
(I-31). Protocol: JSON-RPC 2.0, MCP specification 2025-11-25 (`initialize` ->
`notifications/initialized`, `tools/list` with `nextCursor`, `tools/call` with
`content`/`isError`); stdio = one message per line, HTTP = one POST per message (`http.go`).
Depends on `chat`, `config`, `tools` (`Cap`, `ClipRunes`) and the stdlib (`net/http`); no SDK.

API:
- `New(servers map[string]config.MCPServer, deps Deps) *Client`; `Deps{Dir, Confirm, Dial, Project, Approved, OnApprove}` - `Dir` is the servers' working directory, `Confirm` = `mcp.confirm`, `Dial` the transport seam (nil = the real transports, picked by `url`); `Project` names the servers a project file defined, `Approved(name)` says whether the user approved one, `OnApprove(name)` persists an approval (R-97, I-34). An unapproved project server is `needs approval`: `Start` and `Restart` skip it (`Restart` answers `... waiting for your approval; /mcp approve x`), no Tool of it is offered.
- `Approve(ctx, name) error` - records the approval through `OnApprove`, then starts the server (a no-op for an approved one).
- `CheckURL(url) error` - R-94: plain `http` only to `localhost`/`*.localhost` or a loopback, private or link-local address (a host name is resolved, 5 s); `https` anywhere. Used at dial; exported for tests and callers that want the check early.
- `Client.Start(ctx) []string` - starts every enabled server in parallel, each bounded by 10 s (dial, handshake, tools/list); returns one warning per failed server (`mcp: server "x" failed to start: <reason>`), the others run.
- `Specs() []chat.ToolSpec` - offered tools of enabled, running or crashed servers (name order); descriptions cut at 200 runes; `inputSchema` as the server sent it (`{"type":"object"}` when absent).
- `Kind(name) (known, readOnly, confirm)` - known for an offered tool of an enabled server that is not off/failed (a crashed server keeps its tools known so a call explains the state); `readOnly` only when the server has `trust: true` AND the tool has `annotations.readOnlyHint` (R-87); `confirm` = `mcp.confirm` and not read-only.
- `Run(ctx, name, args) (output, brief, err)` - `tools/call` with the server's `timeout_sec` (default 60 s); on an HTTP server whose Session expired (404) it restarts the server once and repeats the call (R-93); text blocks joined by blank lines, other blocks one line each (`[image image/png, N bytes]`, `[resource uri]`), `structuredContent` as JSON when there is no text; output through `Cap`. Errors the model sees: `isError` -> the text; a JSON-RPC error -> `mcp "x": tools/call: <message>`; no answer -> `mcp "x": no answer in 60s` (the request is dropped, the server stays running); the server gone -> `mcp server "x" exited; /mcp restart x`; a server that is not running -> `mcp server "x" is crashed (exited); /mcp restart x`.
- `Label(name, args)` = the first string argument, clipped (the UI adds `name(...)` itself, like for built-in Tools); `Summary(args)`; `Stub(name)` = `[name: result shown earlier]`; `Owns(name)` = has the `mcp__` prefix.
- `Servers() []ServerInfo{Name, State, Reason, Tools, Enabled, Source, Transport}` (states `running`, `off`, `failed`, `crashed`, `needs approval`; `Source` global/project, `Transport` stdio/http); `Tools(server) ([]ToolInfo{Name, Description, ReadOnly, Offered}, stderr, error)`; `Instructions()` - `server: <instructions cut at 200>` per running server that gave some (the `mcp` prompt block).
- `SetEnabled(server, on)` - hides or shows the tools for this run (the process keeps running); `Restart(ctx, server)` - closes a running one, starts again, switches it on; `Close()` - every server: stdio = stdin closed, killed after 2 s; HTTP = exchanges in flight aborted, DELETE with the Session id (2 s, 405 ignored).
- `Transport` interface {Send, Recv, Close, Stderr} - implemented by the stdio subprocess, the HTTP transport and `mcptest`.

HTTP transport (`http.go`, R-92..R-94): `Send` posts the message in the background (`Content-Type: application/json`, `Accept: application/json, text/event-stream`, the configured `headers` after `os.ExpandEnv`, `Mcp-Session-Id` once the server gave one, `MCP-Protocol-Version` once `initialize` answered); the answer lands in `Recv`: one JSON body, or every `data` event of an SSE stream (`sseEvents`: `data` lines joined by `\n`, comments and other fields skipped, non-JSON data dropped); 202 = accepted notification. An HTTP failure becomes a JSON-RPC error response with the request's id (code `-32010`, or `-32011` for a 404 while a Session id was held = `session expired`), so `conn.call` keeps the timeout and the error mapping; the text is also in `Stderr()` (`/mcp <server>`). Texts: `HTTP 401: the server needs authentication; set headers.Authorization (e.g. "Bearer ${TOKEN}")`, `HTTP <code>: <first 120 bytes of the body>`, `cannot reach <url>: <cause>`, `bad answer: <content type> instead of JSON or an event stream`. Redirects: same scheme and host only (`redirect to another host refused`), at most 5. One exchange is bounded by `max(timeout_sec, 10 s) + 2 s` so a dropped request does not hold a goroutine. No GET stream, no `Last-Event-ID` resumption.

`mcptest` (`internal/mcp/mcptest`): an in-process fake `Server{Tools, Instructions, Version, PageSize, Silent, FailInit, Stderr}` with `Dial` (the `Deps.Dial` seam; each Dial is a fresh connection, so Restart works), `Crash()`, `Connected()`, `Calls` (tool names called). Tools answer through `Handler(args) (text, isError)`. `HTTPServer{*Server, SSE, Sessions, Token, ExpireCalls}` is the same fake as an `http.Handler` for `httptest` (JSON or SSE answers with a priming event, a notification and the response split over two `data` lines; Session ids `s-N` with 400/404; `Forget()` expires the current one; `Inits`, `Deletes`, `Headers` of the last request). The eval's `App/tests/eval/mcp/mcp_http.py` is the same server in Python (stdlib) for the sandbox.

Invariants:
- Nothing of a server runs before `Start`; `Start` returns only when every server is running, failed or timed out.
- A stdout line that is not JSON ends the connection (the spec forbids anything else on stdout); the server is `crashed`, its stderr tail (4 KB) is in the reason and `/mcp <server>`.
- `env` and `headers` values pass through `os.ExpandEnv`: the config names a secret, never holds it. Header values never leave the HTTP transport (I-32): not in errors, warnings, `Stderr()` or reasons.
- A url server fails to start (never crashes swarg) when the url is plain http to a public host, the host does not resolve or answer, the status is not 2xx, or the answer is neither JSON nor an event stream.
- Requests from the server (sampling, roots, elicitation) get `method not found`; its notifications are ignored.

Gotchas:
- The `tools` allowlist filters what the model sees; `Tools(server)` still lists everything (`Offered` false) and an allowlisted name the server lacks lands in the server's `Reason`.
- A late response after a timeout is dropped by id; a server that answers out of order is fine.
- `Run` on an `off` server is unreachable through the agent (`Kind` says unknown) but answers with the state if called directly.
- The HTTP transport reads the protocol version from any response whose `result.protocolVersion` is set and the Session id from any `Mcp-Session-Id` response header: no bookkeeping of the initialize id.
- `http://<host>` with a host name costs a DNS lookup at start (the R-94 check); `https://` skips it. In the sandbox `mcp.test` resolves to the container's private address, so plain http is fine there.
- A `url` with a different port on the same host counts as another host for the redirect check (`req.URL.Host` includes the port).

## Keys in `headers` and `env` (T12, R-107)

`dialHTTP` (header values) and `dialStdio` (`env` values) expand `$VAR` / `${VAR}` with `config.UserKeys().Expand`: the environment variable wins, else the Keys file `<config dir>/keys.env` (then the Old Key directory `<config dir>/keys/VAR`), else "". The expansion runs at every dial, so `/mcp restart` picks up a key edited in the Keys file. The values stay inside the transport (I-32, I-37). Tests isolate the config dir (`isolateConfigDir` in `http_test.go`).
