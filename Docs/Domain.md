# Domain: Swarg

## Context

Swarg is a terminal coding agent: it runs a conversation between a user and a local LLM, lets the
model call tools on the user's machine, and lets mods react to events through hooks.

## Glossary

A new domain term is added here before it appears in code, scenarios or docs.

| Term | Meaning | Do not call it | Code name |
|------|---------|----------------|-----------|
| Session | One run of swarg from start to exit, with its history | chat, conversation | `Session` |
| Turn | One user request processed until the model gives a final answer, including all tool calls in between | step, round, iteration | `Turn` |
| Message | One entry of the history: user, assistant, tool or system | entry, record | `Message` |
| Step | One model request inside a Turn and the ToolCalls it asks for; a Turn is one or more Steps | round, iteration, cycle | `step` (in `runTurn`) |
| Tool | A named action the model may call (read_file, list_dir, grep, edit_file, write_file, shell, or one provided by a mod); read-only or mutating | function, command | `tools.Tool`, `agent.ToolRunner` |
| ToolCall | A request from the model to run a Tool with arguments | invocation, action | `chat.ToolCall` |
| ToolResult | The text a ToolCall gives back to the model (its output, or an error, skip, denial or cancel note); stored as a tool Message | response, output | tool `chat.Message` |
| Web tools | `web_search` (query to numbered results) and `web_fetch` (url to page text); built in, ask for Approval by default | browser, internet tool | `tools.WebSearch`, `tools.WebFetch` |
| Approval | The user's answer (allow or deny, with an optional note) to a mutating ToolCall | confirmation, permission | `agent.Approval` |
| Abort | The second interrupt of a Turn: the Provider connection is closed at once, tools are killed, the server is checked afterwards | kill, cancel | `Agent.Abort` |
| InputQueue | User input typed while a Turn runs, waiting to be injected at the next Step | buffer, pending input | `InputQueue` |
| Provider | A model backend the core streams completions from (local mlx, Ollama on another machine, OpenRouter); reached only through the `agent.Provider` interface; the config `type` (`openai`, `ollama`, `openrouter`) selects the request presets | backend, client, API | `Provider`, `config.Provider.Type` |
| Source | The Provider entry in use at run time with its client, model and request presets; the user calls it the server (`/server`), the config key stays `providers`; switched between requests (R-100) | backend switch, endpoint | `agent.Source`, `Agent.SetSource`, `agent.EvProvider` |
| Catalog | The model list of a Source as its server reports it: id, name, context length, prices, supported parameters (R-102) | model list, registry | `openai.ModelInfo`, `Client.Catalog` |
| Recent models | Per Source, the model ids used before, newest first, kept in the user config dir (R-101) | history, MRU | `config.Recent` |
| Keys file | The user's one global file `<config dir>/keys.env` with `NAME=value` lines, private to the user; the only place a Key value is written, by the user's own editor (R-106, I-37) | vault, secrets file, keychain, key store | `config.KeyStore.File`, `config.UserKeys` |
| Old Key directory | The earlier layout `<config dir>/keys/<NAME>`, one private file per Key; still read after the Keys file, never written or deleted by swarg (R-106) | key store, keys dir | `config.KeyStore.OldDir` |
| Key | An API key or token kept in the Keys file under an upper-case name (the `api_key_env` of a provider, a `$VAR` of an MCP header or env); the environment variable of the same name wins (R-105, R-107) | secret, credential, token file | `KeyStore.Lookup`, `ui.KeyRow` |
| Cloud notice | The one-time notice per project shown at the first switch to a Source that is not local (R-104) | privacy warning, consent | `Approvals.Notice`, `config.Provider.Local` |
| Spend | The money a Session has cost so far in USD: the sum of the costs the server reported per response (OpenRouter `usage.cost`), kept per model id and in the SessionFile; a local server adds 0 (R-111) | bill, budget, balance | `agent.Spend`, `Agent.Spend`, `EvSpend` |
| Warning level | The optional `cost.warn_usd`: the Spend at which the user is warned once; it never stops anything (R-114, I-39) | spend limit, budget cap | `config.Cost.WarnUSD`, `Deps.WarnUSD` |
| StreamEvent | One event of a streamed completion: text, prefill progress, usage, or the final done | chunk, token | `chat.StreamEvent` |
| SystemPrompt | The first message of every request, assembled from Blocks | preamble, instructions | `prompt.Set.Build` |
| Block | One switchable piece of the SystemPrompt (base, global `NN-name.md`, project `SWARG.md`, env, tool blocks); a `<name>.md` override file replaces a built-in one (R-49) | snippet, section, part | `prompt.Block` |
| Prompt Profile | How much instruction text the SystemPrompt carries: `compact` (the base Block alone), `standard` (+ the standard layer), `full` (+ the full layer); cumulative, chosen by the config or by the user (R-115, R-116) | mode, level, preset | `prompt.Profile` |
| Agent | A helper run for one job: a child conversation with its own history, Source (server and model), system prompt, tool allowlist and step limit; several run at once; hands back one AgentResult (R-120, R-122); not a Task (a Task shares the Session's Source and runs alone) | sub-agent run, worker, subtask | `agent.runAgent`, `conversation.agent` |
| Agent type | A named kind of Agent: name, one-line description, server, model, profile, tools, max steps and a body of instructions; from a markdown file or built in (`general`, `explore`, `plan`, `review`) (R-121) | role, persona, mode | `agents.Type`, `agent.AgentType` |
| Slot | Permission to run one Agent request: one per Source (`providers.<p>.max_parallel`) and one of the (at most 4) overall; an Agent without a free Slot waits (R-122) | lane, permit | `slots` |
| AgentResult | The ToolResult of an `agent` call: the TaskResult format (R-38) | report, answer | tool Message named `agent` |
| BackgroundJob | A shell command that outlives the ToolCall that started it; read and killed later by id (`bg1`, `bg2`, ...); not a Task (agent work) | daemon, task, process | `tools.Jobs` / `tools.JobInfo` |
| Detach | The user's move of the running detachable ToolCall to the background; the call returns at once and its command becomes a BackgroundJob | suspend, push to bg | `ToolHooks.Detach`, `Agent.Background` |
| Mod | An external process that extends swarg through the JSON protocol | plugin, extension | `Mod` |
| Skill | A folder with `SKILL.md` (front matter `name`, `description`; body: instructions for the model) and optional files beside it; global in `<config dir>/skills/<name>`, project in `.swarg/skills/<name>` | plugin, prompt, command | `skills.Skill` |
| Skill list | The one-line-per-Skill text (`- name: description`) in the SystemPrompt, enabled Skills only | menu, index | prompt block `skills` |
| Applying a Skill | The `skill` ToolCall whose ToolResult is the body of `SKILL.md` (or one file of the Skill folder) | loading, invoking | `skills.Tool` |
| MCP Server | An external program (stdio, `command`) or endpoint (Streamable HTTP, `url`) speaking MCP JSON-RPC that offers Tools; configured under `mcp.servers.<name>` | plugin, mod, integration | `mcp.Client`, `config.MCPServer` |
| Transport | How swarg reaches an MCP Server: a subprocess with one JSON message per line (stdio) or HTTP POSTs to the url (Streamable HTTP) | connection, channel | `mcp.Transport`, `stdioTransport`, `httpTransport` |
| Session (MCP) | The server-side state behind an `Mcp-Session-Id` an HTTP server hands out at `initialize`; swarg sends it back on every request and ends it with DELETE | token, cookie | `httpTransport.session` |
| MCP Tool | A Tool offered by an MCP Server, named `mcp__<server>__<tool>` in swarg | remote tool, function | `mcp.tool` |
| Server state | `running`, `off` (disabled or hidden), `failed` (did not start), `crashed` (exited later), `needs approval` (a project server the user has not approved) | status, health | `mcp.State` |
| Menu | A list in the list area navigated with arrows, typed filter, mouse and Esc: the Session picker, `/skills`, `/mcp`, `/model`, `/think`, `/config` | dialog, popup, widget | `ui.menu`, `ui.menuRow` |
| Scope | Where a setting is saved: `global` (the user config dir) or `project` (`<project>/.swarg/`) | level, layer, profile | `ui.ScopesAPI`, `config.ProjectConfig` |
| Project config | `<project>/.swarg/config.json` (+ `config.local.json`, personal, git-ignored by the user), layered over the global config | workspace settings, .mcp.json | `config.LoadLayered`, `config.Layers` |
| Approval (MCP) | The user's one-time consent per project to run a project MCP Server, stored in the user config dir | trust, allow | `config.Approvals`, `mcp.Client.Approve` |
| Hook | A named point where swarg emits an Event to Mods | callback, listener | `Hook` |
| Event | The data swarg sends to Mods at a Hook | notification, signal | `Event` |
| WorkSpan | The Messages of one Turn between its TurnStart Message and its final answer: assistant Messages with ToolCalls, their ToolResults, intermediate text | span, work block, turn log | `agent.span` |
| Stub | The short text a request sends in place of a stubbed ToolResult; the full ToolResult stays in the history | placeholder, truncation | `chat.Message.Stub` |
| Compaction | Replacing completed WorkSpans with one WorkSummary each, between Turns | summary of chat, clear, trim | `Agent.Compact` |
| WorkSummary | The assistant Message that replaces one WorkSpan in a Compaction | recap, digest | `chat.Message.Summary` |
| SessionFile | The append-only JSONL record of a Session, written by the Writer and replayed by Load | save, log, dump | `session.Writer`, `session.Load` |
| Trace | The debug log of everything the agent did (requests, replies, calls, timings), kept next to the SessionFile | log, debug dump | `trace.Writer` |
| TurnStart | The user Message that starts a Turn | first message, prompt | `chat.Message.TurnStart` |
| Injected Message | A user Message typed during a Turn and injected into the history at a Step | queued message, follow-up | `chat.Message.Injected` |
| Loop guard | The agent check that stops a streamed answer when it starts repeating itself; the answer is stored cut before the repetition | repeat filter, anti-loop | `agent.loopGuard` |
| Name | The title the user gives a Session (`/rename`); stored in its SessionFile and shown in lists instead of the first user Message | title, label, alias | `session.Loaded.Name`, `session.Info.Name` |
| Hover | The look a clickable element takes while the pointer is over it (underline); a pointer move changes nothing else | highlight, focus, mouse-over | `ui.hover`, `ui.hoverSpan` |
| Hotspot | One clickable span of screen cells registered during a draw, with an id used to dispatch a click | region, button area, hit box | `ui.hotspot`, `hover.add` |
| Waiting Message | A user text sent from the UI that the agent has not taken yet; shown at once, dim and marked `(waiting)` | pending, draft | `ui model.waiting` |
| Progress line | The row under the newest conversation item while the agent works: spinner, state, time, tokens, live speed | spinner row, status | `ui model.progressLine` |
| Bottom panel | The row under the input box: model, context use, speed, auto-approve, background jobs | status bar, footer | `ui model.panelLine` |
| Generation speed | Completion tokens per second of one Step, from the first generated token to the end of the stream | tps, throughput | `agent.EvSpeed` |
| Model choice | The model id requests name; chosen from the Provider's list, may change in the middle of a Session | model switch | `Agent.SetModel`, `agent.EvModel` |
| Thinking mode | On or off: with it on every request asks the model to think (`enable_thinking`, or `reasoning_effort` on Ollama), uses the thinking sampling and a larger output limit; off by default; switched with `/think` | reasoning switch, think flag | `Agent.SetThinking`, `agent.EvThinking`, `config.Provider.Thinking` |
| Suggestion | A command offered above the input while its name is being typed; Tab or a click completes it | autocomplete, hint, popup | `ui.suggestions`, `model.completeSuggestion` |
| Task | One goal worked on by the agent loop in its own history, started by a `task` ToolCall of a Session Turn or of another Task (its parent), ending in a TaskResult; the goal text is its TurnStart; not a BackgroundJob (a process) | sub-agent, job, thread, subtask | `agent.conversation` (depth >= 1), `agent.runTask` |
| TaskResult | The one text a Task returns to its parent: a status line (`done; files: ...`), the model's report and `steps: N/M`; stored as the ToolResult of the `task` ToolCall | summary, output, return value | `agent.taskResult` |
| Depth | How many Tasks are between a Task and the Session: 0 the Session, 1 a Task started by it, 2 a Task started by that Task | level, nesting | `conversation.depth` |
| Chain | The running Task and all its ancestors up to the Session Turn; one Esc acts on the whole Chain | stack, tree | `Agent.chain` |
| ContextLimit | The token budget a request is measured against; per model (R-41) | context size, window | `meter.limit`, `Deps.LimitFor` |
| Reasoning | The model's thinking text streamed before its answer (`reasoning_content`); kept on the assistant Message and sent back within the Turn (R-42) | thoughts, chain of thought | `chat.Message.Reasoning` |

## Rules

Rules are numbered `R-n`; each one gets BDD scenarios (normal, boundary, failure).

- R-1 User input submitted during a Turn never interrupts it; it waits in the InputQueue and reaches
  the Provider at the next Step of the running Turn. If the running answer has no ToolCalls the Turn
  ends and the queued text starts the next Turn. While a Task runs the text waits for the Session's
  next Step after the Task returns; it never enters a Task (R-40).
- R-2 An interrupt stops the output at once, keeps only the text already shown, and the Provider
  stream is still read to its end (never wedge a single-lane server).
- R-3 A failed Turn keeps the user Message, stores no assistant Message, and never ends the Session.
- R-4 The SystemPrompt is rebuilt at the start of every Turn from the enabled Blocks.
- R-5 Context warnings fire once per upward crossing of 80% and 100%; sending is never blocked.
- R-6 Every ToolCall gets a ToolResult, whatever happened to it (ran, failed, skipped, denied, cancelled).
- R-7 A wrong ToolCall (unknown Tool, bad arguments, tool error) returns an error ToolResult to the
  Provider; the Turn continues.
- R-8 Read-only Tools of one Step run together; mutating Tools run one at a time, in call order.
  ToolResults keep call order.
- R-9 A mutating Tool runs only with Approval; without an approval function every call is allowed;
  read-only Tools never ask. Two denied calls in one Turn end the Turn (two in one Task end the Task,
  which counts as one denial for its parent; the denial note inside a Task says "report it in your
  result" instead of "ask the user").
- R-10 Text sent during a Turn reaches the Provider at the next Step, after the ToolResults; mutating
  ToolCalls (and `task` calls) of the Session not yet started are skipped, already started calls are
  not cancelled. Inside a Task nothing is skipped: the text waits.
- R-11 The first interrupt is soft (output stops, tools are cancelled, the stream is drained); a second
  interrupt aborts at once: the Provider connection is closed, queued text still runs afterwards, and
  a failed server check is reported as a warning.
- R-12 A Turn ends after MaxSteps Steps with a warning; the history stays valid.
- R-13 A BackgroundJob outlives its ToolCall and its Turn.
- R-14 Reading a BackgroundJob returns only output not read before.
- R-15 A BackgroundJob ends with the Session.
- R-16 A user Message and a final answer are never changed by Compaction or stubbing.
- R-17 Inside a Turn every request only appends to the previous one, except right after an Injected
  Message or an emergency stub batch.
- R-18 Compaction and Load never run while a model request is in flight; both run in the worker
  between Turns (worker-serialized).
- R-19 A Stub always names the ToolCall it replaces; the history keeps the full ToolResult.
- R-20 A resumed Session has the same history as the saved one.
- R-21 After a Load every ToolCall has exactly one ToolResult.
- R-22 An answer that starts repeating itself (one line four times within 40 lines, the last 300
  characters seen twice before, or a phrase three times in a row inside one line) is cut at once:
  the request is disconnected, the answer is stored without the repetition and marked truncated, and
  the user gets a warning (not "cut off by a limit"). ToolCalls and the next Turn are unaffected.
- R-23 Automatic Compaction starts at a Turn end when the meter has reached `AutoCompact` percent of the
  limit and a WorkSpan exists; it runs before queued texts. Two failed or no-gain automatic runs in a row
  switch it off, with a warning, until a manual Compaction succeeds.

- R-24 A ToolCall repeated twice in a row with the same arguments and the same outcome (the same
  error, or the same result) gets a note telling the model not to repeat it; the third such call in a
  row ends the Turn with a warning. A different call or a different outcome restarts the count; it
  never spans Turns. Waiting on a BackgroundJob is not a repeat. Two different calls that keep
  alternating with the same outcomes are a cycle (R-44). The calls of one Step are one decision
  of the model: the same call with the same outcome counts once within a Step (both rules).
- R-25 A path written from the root (`/strutil.py`) that is not inside the project folder means the
  project folder (`strutil.py`). A path that climbs out of the project folder (`/../etc/passwd`, a
  symlink out, UNC) is refused.
- R-26 A saved Session can be deleted from the `swarg -r` picker (Ctrl+D, confirmed with y): its
  SessionFile and Trace are removed from the sessions directory and nothing else is touched, so the
  Session is never loaded again. Inside the Docker sandbox this deletes only the container's copy.
- R-27 A user text sent while the agent works is shown in the conversation at once as a Waiting
  Message, under every item already shown. When the agent takes it (injected at a Step or starting
  the next Turn) the mark goes and it stays in its place in history order. Waiting Messages keep
  their send order.
- R-28 While the agent works (a Turn or a Compaction) the Progress line is the last row of the
  conversation content: under the newest item and under the Waiting Messages. Idle, it is gone;
  during a confirmation prompt the prompt replaces it.
- R-29 The Generation speed of a Step is its completion tokens (usage) divided by the time from the
  first generated delta, text or reasoning (else the end of prompt reading, else the request), to the
  end of the stream (R-43). A
  Step without usage, with 0 completion tokens, ended by an error, an Abort or the Loop guard has
  no speed. The value is approximate (usage may count tokens generated before the first text; a
  Step without text or progress counts from the request). While text streams the Progress line and
  the Bottom panel show a live estimate `~N tok/s` (one streamed chunk = one token, true for mlx);
  afterwards the panel shows the last measured value.
- R-30 The chosen model is used from the next model request on (also inside a running Turn), by
  Compaction and by the server check. Every change is recorded in the SessionFile; a resumed
  Session continues with its last chosen model unless `--model` is given (then that choice is
  recorded). A blank model id is refused; choosing the model in use changes and records nothing. The
  ContextLimit follows the model (R-41).
- R-60 The Thinking mode is used from the next model request on (also inside a running Turn). On:
  `chat_template_kwargs.enable_thinking` true, temperature 0.6, top_p 0.95, top_k 20, min_p 0,
  presence_penalty 0, and `max_tokens` = `thinking_max_tokens` (default 8192); off: the request is
  exactly as before. `/think on|off` switches it, `/think` shows it, a bad argument is a usage
  notice; the mode in use changes and records nothing. The config `thinking` (default false) is the
  start mode of a new Session. The Bottom panel shows `think` while it is on.
- R-61 Every streamed chunk is reported to the UI while it arrives: visible text as a Delta,
  reasoning and tool-call arguments as Activity. The Progress line's token counter and live speed
  count all of them; Activity text is never shown or stored. (mlx_lm.server sends a tool call in
  one chunk at the end of the generation, so a silent server is still silent; other servers stream
  the arguments.)
- R-62 A running Task's block is open: its tool calls, nested Tasks and text show as they start,
  with the depth gutter and a background a little lighter than the Session. When the Task ends the
  block folds to its one-line summary (head + result line); a click or /tool N opens it again.
- R-63 A stream with no bytes for the provider's idle limit (`idle_timeout_s`, default 600 s) ends
  with the idle error. The limit must cover a long silent generation: mlx_lm.server sends a tool
  call in one chunk at the end, so a client cannot tell a slow tool call from a stopped server;
  Esc aborts at once meanwhile.
- R-64 A Provider of type `ollama` (Ollama's OpenAI-compatible `/v1`) switches the Thinking mode
  with `reasoning_effort`: off = `"none"`, on = `"high"` (temperature 0.6, top_p 0.95,
  presence_penalty 0); `chat_template_kwargs`, `top_k`, `min_p` and `repetition_penalty` are not
  sent because Ollama ignores them. Without `reasoning_effort` Qwen3.5 thinks by default, so the
  preset carries `"none"`. The model's reasoning arrives as `reasoning` and is sent back under both
  `reasoning` and `reasoning_content`, so the server's template can reproduce the prompt prefix.
- R-65 When the usage of a Step reports fewer than half of the estimated prompt tokens of a
  request above 2048 tokens, the user is warned once per conversation (the Session or a Task) that the server read only part of
  the prompt (its context window is smaller than the ContextLimit; Ollama: `OLLAMA_CONTEXT_LENGTH`).
  `/clear` re-arms the warning. A server cuts the prompt silently; the usage is the only trace.
- R-66 A file path whose first segment repeats the name of the project folder (`work/greet.py`
  written from `/work`) while no such subfolder exists is refused by `write_file` and `edit_file`
  with the right path in the error (`use "greet.py"`), so a nested copy of the folder never
  appears. Once the subfolder exists the path is ordinary. Small models repeat the folder name
  of the Working directory line of the prompt (T7: 11 of 155 eval runs, all four models).
- R-67 `write_file` takes `"append": true` to add the content after the existing one (a missing
  file is created; CRLF is kept), so a long file can be written in parts. The nudge after an
  undelivered Step (F11) names the output limit in tokens and this recipe (first part with
  `write_file`, the next parts with `append`): the 2B looked for an append in `edit_file` with an
  empty `old`. The 4B and 9B ignore the nudge and repeat the whole file, so the Ollama preset's
  max_tokens is 4096 (a 250-line file is ~2800 tokens; T7 code-07).
- R-31 The Bottom panel shows exactly: model, context use (percent and tokens), Generation speed,
  auto-approve state, running background jobs (only when > 0: nothing extra when idle), plus a
  transient `copied N lines`. A click on the model
  opens the model list (`/model`); a click on the jobs item lists the jobs (`/bg`).
- R-32 `/rename <name>` gives the Session a Name, stored in its SessionFile as one line (blank
  Names are refused, the last Name wins, a Clear keeps it); lists show the Name instead of the
  first user Message, and the first user Message when there is no Name; a SessionFile without a
  Name record loads as before (T3 ui, item 1).
- R-33 Opening a saved Session from inside the app continues it: the history equals the saved one
  and new Messages go to that Session's own SessionFile; the Session left behind keeps its file
  (a new file that got no Message is removed). Idle only: a running Turn ends first (T3 ui).
- R-34 Deleting a saved Session needs a confirmation (key or click); the Session being written
  cannot be deleted (T3 ui).
- R-35 Commands start only with `/`; `/help` lists every command with its description; an unknown
  command is reported with a hint to `/help`; `//text` sends the text. While a command is typed
  the matching commands are suggested and can be completed; the input shows it is a command (T3 ui).
- R-36 Every clickable element (tool block heads, session rows, buttons, panel items) shows the
  Hover while the pointer is over it, so it is clear it is interactive; one shared mechanism
  serves all of them (T3 ui, item 2).
- R-37 A Task's Messages never enter its parent's history: the parent gets exactly one ToolResult
  per `task` ToolCall, the TaskResult, and nothing else of the Task; the Session continues as if only
  the result had been there (T4 task).
- R-38 A Task ends with a TaskResult whatever happened to it: its first line is the status (`done`,
  `stopped: step limit`, `stopped: tool calls were denied`, `stopped: answer cut off` (the token limit),
  `stopped: ...` of a guard, `failed: <error>`, `interrupted`, `aborted`) followed by `; files: a, b` when files were changed (by the Task or a
  Task inside it); then the report (the final text, else the list of its steps), then `steps: N/M`.
  The first line is also the Brief of the ToolResult, so a Compaction of the parent keeps the status
  and the files. A failed Task is an error ToolResult and the parent Turn goes on (T4 task).
- R-39 The `task` Tool is offered only while the Depth is below `tasks.max_depth` (default 8, allowed 0..8, 0 = no Tasks; a 9th level cannot be configured); one
  Session Turn runs at most `tasks.max_per_turn` Tasks over its whole Chain; a goal that repeats the
  goal of a running ancestor or the Turn's own request, or an empty goal, is refused with an error
  ToolResult and no model request (the `/task` goal is exempt from the repeat check) (T4 task).
- R-40 One Esc interrupts the whole Chain: the request in flight is drained, every level ends with a
  TaskResult `interrupted`, innermost first, and the Turn ends interrupted; a second Esc aborts the
  same way (`aborted`). Text sent while a Task runs waits for the Session and never enters a Task.
  `/task <goal>` runs the goal as a Task first, then the Session model answers from its result; sent
  during a Turn it waits and starts its own Turn (T4 task).
- R-41 The ContextLimit follows the model: `context_limits` per model id (the built-in table gives
  32k to the Qwen3.5 models), else `context_limit`; a model switch re-arms the 80%/100% warnings
  when the limit changes; every request, also inside a Task, is measured against the limit of the
  model it names (T4 task).
- R-42 The model's reasoning (`reasoning_content`) is kept on its assistant Message and sent back
  with the requests of the running Turn, so a chat template that renders it keeps the server's
  prefix cache; earlier Turns' reasoning is not sent (T4 task).
- R-43 The Generation speed clock starts at the first generated delta of any kind, text or
  reasoning: reasoning tokens count in usage, so a clock started at the first text would inflate the
  speed of a thinking model. A Step with only ToolCalls counts from the end of prompt reading: the
  server sends the call in one chunk at the end of the generation, which is no start (T4 task).
- R-44 Within a Turn, two different ToolCalls that alternate with the same outcomes (ignoring
  BackgroundJob ids) form a cycle. At the second full repetition (4 calls) the ToolResult gets a
  note telling the model not to repeat them; at the third (6 calls) the Turn ends with a warning.
  A different call or outcome inside breaks the cycle; a denial clears it. Two successful polls
  of BackgroundJobs (`shell_output`) never form a cycle, so waiting on one or two jobs is safe
  (T4 guard).
- R-45 `web_search` and `web_fetch` ask for Approval like `shell`; the config key `web.confirm: false`
  frees them, and a freed web tool runs in parallel batches like a read-only one (T4 tools).
- R-46 A ToolCall without one of its required arguments, or with an empty string for it, fails with
  an error that names the argument and shows the correct call (`missing argument "path"; call
  read_file like {...}`); every built-in Tool with a required argument (`read_file`, `grep`, `shell`,
  `edit_file`, `write_file`, `shell_output`, `shell_kill`, `web_search`, `web_fetch`) (T4 tools, extended
  after the T4 live check: the 4B repeated `read_file {}` inside Tasks).
- R-47 `web_fetch` takes only absolute http and https URLs and refuses addresses that resolve to
  loopback, private, link-local, carrier-grade NAT, multicast or unspecified IPs (every resolved IP
  is checked, also after each redirect) unless `web.allow_private` is set; without a network it fails
  with a clear one-line error and never hangs (T4 tools).
- R-70 `web_fetch` returns a long page in pages of at most 8000 bytes (argument `page`, default 1);
  every page but the last ends with a note naming the next page, a page past the end is an error
  that says how many pages there are; nothing in the middle of a page is dropped (T8 web).
- R-71 `web_fetch` page text leaves out site chrome: `nav`, `aside` and `footer` (T8 web).
- R-72 A link in `web_fetch` text keeps its address: `text (absolute url)`; links within the same
  page and non-http links keep only their text (T8 web).
- R-48 The note in a cut tool result names what gets the rest back: for command output "rerun with a
  filter (grep, head, tail)", for files and other results "use read_file offset/limit or grep"
  (T4 tools).
- R-49 A file `<name>.md` in the global blocks folder, named after a built-in Block (`base` or a
  `tool-<name>` block), replaces that Block's text (global `NN-name` Blocks are not replaceable); the Block keeps its order and on/off state, and
  the replacement is reported as a warning at start. `project`, `env` and unknown names are not
  replaced (warning). This is how prompt variants are compared on one build (T5 prompts).

- R-80 Skills live in `<config dir>/skills/<name>/SKILL.md` (global) and `<cwd>/.swarg/skills/<name>/SKILL.md`
  (project; the working directory only). The folder name is the Skill's name (`^[a-z0-9_-]{1,32}$`); a
  project Skill replaces a global one of the same name. A SKILL.md without a `description` is skipped
  with a warning. Loaded once at start (T8 S).
- R-81 The enabled Skills are listed in the SystemPrompt as `- name: description` (descriptions cut at
  160 characters) under one heading; no Skills loaded means no list and no `skill` Tool, so a
  project without Skills pays no prompt tokens.
- R-82 The `skill` Tool (`{name, file?}`, read-only, no approval) returns the body of `SKILL.md`
  without the front matter plus the list of the folder's other files, or with `file` one of them
  (confined to the Skill folder). An unknown name lists the enabled Skills; a disabled Skill is
  refused. In history the result is stubbed (`[skill x: applied earlier; call it again if needed]`).
- R-83 `/skill <name> [text]` (and `/<name>` for a loaded Skill when no built-in command has that
  name) starts a Turn whose first Step is a synthetic `skill` ToolCall, then the model answers the
  text ("Apply this skill." when none). Like a /task goal it never joins a running Turn.
- R-84 `skills.disabled` in the config and `/skills on|off <name>` in the app switch a Skill off: out
  of the list, refused by the Tool; `/skills` lists them. Above 20 enabled Skills a warning at start.
- R-85 MCP Servers come from `mcp.servers.<name>` (`command`+`args`+`env` for stdio, `url` for
  Streamable HTTP (R-91), `disabled`, `trust`, `tools`, `timeout_sec`); `env` values pass through
  `os.ExpandEnv`, so a secret is named, never written. Every enabled server starts at swarg start,
  in parallel, each bounded by 10 s (handshake `initialize` + `notifications/initialized`,
  `tools/list` with `nextCursor`); an unknown protocol version in the answer is a failure (T8 M).
- R-86 MCP Tools are offered as `mcp__<server>__<tool>` with the description cut at 200 characters
  and the server's `inputSchema` as is; the arguments go to `tools/call` unchanged. The text content
  blocks are the ToolResult; `isError` makes it an error result the model sees; no answer within
  `timeout_sec` (default 60 s) is an error naming the limit and leaves the server running; a server
  that exited answers `mcp server "x" exited; /mcp restart x` and its Tools stay known until the
  restart. Servers that gave `instructions` are quoted in the `mcp` prompt block (cut at 200).
- R-87 An MCP Tool asks for approval like `shell`, unless the server is `trust: true` AND the tool
  carries `annotations.readOnlyHint: true` (then free and parallel like `read_file`), or
  `mcp.confirm` is false (frees them all). The hint alone never frees a tool.
- R-88 `tools: [...]` on a server is the allowlist offered to the model (`/mcp <server>` still lists
  every tool); `/mcp on|off <server>` hides or shows a server's Tools for this run, `/mcp restart
  <server>` starts it again (also from failed or crashed), `/mcp` lists state and tool counts.
- R-89 Skills and MCP are counted outside the fixed prompt budget of the built-ins (1200 tokens):
  the Skill list, the `skill` Tool, the MCP Tool specs and the `mcp` block have their own ceiling of
  1500 tokens, measured at start; above it one warning names the three biggest items and nothing
  is dropped (the owner trims with `mcp.servers.<name>.tools`, `skills.disabled`, shorter descriptions).
- R-91 An MCP Server reached by `url` works like a stdio one above the Transport: the same tool
  names, `trust`, `tools`, approvals, `timeout_sec`, `/mcp`. Exactly one of `command` and `url` is
  required (a clear config error otherwise); `headers` (only with `url`) are sent on every request
  and their values pass through `os.ExpandEnv`, so a bearer token is `"Authorization": "Bearer
  ${TOKEN}"`, never a literal (T9).
- R-92 Streamable HTTP (MCP 2025-11-25): every JSON-RPC message is one POST to the url with
  `Accept: application/json, text/event-stream`; a request's answer is one JSON object or an SSE
  stream whose `data` events are messages (the response, possibly server notifications before it);
  a notification is answered 202. An `Mcp-Session-Id` from the `initialize` response is sent back on
  every later request and on the DELETE that ends the Session at close (405 ignored); every request
  after `initialize` carries `MCP-Protocol-Version` with the negotiated version. swarg opens no GET
  stream: it accepts no server-initiated requests (a request inside a stream gets `method not found`).
- R-93 HTTP failures are ToolResults or start warnings the user can act on: a status is named
  (`HTTP 500: <body head>`), 401 says `the server needs authentication; set headers.Authorization`,
  an unreachable url says `cannot reach <url>: <cause>`, a wrong content type `bad answer: <type>`.
  A 404 to a request carrying a Session id means the Session expired: swarg re-initializes once (new
  Session, fresh tool list) and repeats the call once; a second expiry is the error `session
  expired`. A hung exchange ends with the server's timeout like a stdio call. The server's state
  (`/mcp`) and `/mcp restart` work as for stdio (I-31).
- R-94 A url server is reached safely: plain `http://` only when the host is `localhost` or an
  address in a loopback, private or link-local range (a host name is resolved at start), else
  `https://` is required (the server fails to start with `use https`); a redirect to another scheme
  or host is refused; header values stay inside the Transport (I-32).
- R-95 The configuration is layered: Defaults <- the global `config.json` <- the project's
  `.swarg/config.json` <- `.swarg/config.local.json`. Files merge as JSON objects key by key (a
  project entry of `mcp.servers.x` or `providers.p` changes only the keys it names), lists and
  scalars replace; an unknown field names its file. A project without `.swarg/` loads exactly
  as before (T10).
- R-96 A project file never lowers safety: `tools.auto_approve: true`, `web.confirm: false`,
  `mcp.confirm: false` and `trust: true` on an MCP Server are kept at the global value with a
  warning at start; saving such a value into the project scope is refused with the same text (T10).
- R-97 An MCP Server defined by a project file runs only after the user approved it once for
  that project (`/mcp approve <server>`, or the menu); until then its state is `needs approval`,
  nothing of it runs and no Tool is offered. Approvals live in `<config dir>/approvals.json`
  keyed by project directory and the hash of the server entry: a changed entry asks again.
  Global servers need no approval (T10).
- R-98 Switches are saved: `/skills on|off <name> [scope]` and `/mcp on|off <server> [scope]`
  apply at once and write `skills.disabled` / `mcp.servers.<name>.disabled` into the scope's
  file; the default scope is `project` when the project has `.swarg/`, else `global`. `/config`
  saves every change the same way (model, thinking and auto-approve also apply at once, the rest
  at the next start). A switch whose write fails stays applied for the run and says why (T10).
- R-99 One menu engine serves every list of the app: rows with a cursor (arrows, j/k, PgUp/PgDn,
  Home/End), a typed filter, hover and click on rows and on their right label, the wheel, Esc (q)
  closes or returns to the parent menu, Tab cycles the save scope, a confirmation in the footer
  (`y` / buttons). A menu opens during a Turn; an approval prompt closes it. `/skills` (Enter
  applies, Space switches), `/mcp` (Enter shows the tools, Space switches, r restarts, a approves),
  `/model`, `/think`, `/config`; the commands with arguments keep working (T10).
- R-100 The Source is switched at run time (`/server <name>`, the menu, the panel's provider item):
  the next request built from then on goes to the new server with its model, presets, limits and
  idle timeout; a request in flight is never touched (I-36). The model is the Source's last used one,
  else its configured default; the ContextLimit is re-read; when the history no longer fits the new
  limit a Compaction runs before the first request. A Source that is not defined, invalid or without
  its key (`no key: set <VAR>`) is refused and nothing changes. `/server` lists every configured
  Source with type, address and state (`ok`, `down: <cause>`, `no key: set <VAR>`, `bad config:
  <reason>`), the active one marked (T11).
- R-101 `/model` lists only the active Source's models, its Recent models first; every model used on a
  Source is remembered per Source in the user config dir (`recent.json`). The SessionFile records the
  Source (header and switch lines); a resumed Session continues on its last Source with its last
  model; a file without a Source (before T11) resumes on the configured one. The Bottom panel shows
  `provider/model` (T11).
- R-102 The `openrouter` Provider type is OpenAI-compatible with the key from `OPENROUTER_API_KEY`
  (preset `providers.openrouter`, `https://openrouter.ai/api/v1`, no local sampling in the body).
  Its Catalog is filtered to tool-capable models (`supported_parameters` has `tools`); rows show
  `free` or the price per million tokens; typing searches id and name; the ContextLimit of a model
  is the Catalog's `context_length` (no hand table); the Thinking mode sends `reasoning: {effort:
  high}` (T11).
- R-103 A failed request has a clear text: 401 `refused the API key`, 402 `no credits`, 404 `no such
  model ... /model lists the available ones`, 429 `rate limited` (one wait for a `Retry-After` of at
  most 30 s, then one retry, never a loop), 5xx `failed upstream`; every text carries the status and
  the body snippet, never the key (T11).
- R-104 A Source that is not local (https, or a public address) sees the conversation and the files
  the tools read: the first switch to one per project shows a notice once (kept in `approvals.json`).
  The key is never stored in a config file (R-105); a configured active Source without its key is a
  start error naming the variable and the Keys file (T11).
- R-105 A provider's key is the environment variable named by `api_key_env`, else the Key of that
  name in the Keys file (R-106). The env var wins.
- R-106 Keys are written only in one global Keys file, `<config dir>/keys.env`, by the user in an
  editor: lines `NAME=value`, blank lines and `#` comments, one pair of surrounding quotes stripped
  from the value, `NAME=` with no value is no Key. A name is `^[A-Z][A-Z0-9_]{0,63}$`. The file must
  be private (no group or other permission bit; Windows has none to check): otherwise it is ignored
  and a warning names the file and `chmod 600`. A line that is not `NAME=value` is skipped with a
  warning that names the line NUMBER only, never its text. The lookup order is: the environment, the
  Keys file, the Old Key directory. The file is read again on every lookup, so an edit applies at
  once. Swarg never writes a Key value anywhere and never moves or deletes an Old Key file; when
  Old Key files exist, a one-line start hint suggests moving them into the Keys file. The store is
  global: no project scope, never in `config.json`. `/keys` and the `API keys` row of `/config` show
  every name the config uses (providers' `api_key_env`, `$VAR` / `${VAR}` in MCP `headers` and `env`)
  plus every Key found in the files: who uses it, the state `set (env)` / `set (env, wins)` /
  `set (keys.env)` / `set (old keys dir)` / `missing`, the value masked as the first 6 + `…` + the
  last 4 characters when it has 16 or more, else `set`. The menu is view only; its footer shows the
  file and `r reload` (T12).
- R-107 A Key edited in the Keys file is used at once, without a restart: the provider client reads
  its key on every request (`Client.Key`), `/server` and a switch re-check it, an MCP `$VAR` is
  resolved when the server is dialled (`/mcp restart` re-reads it). `$VAR` in MCP `headers` and `env`
  resolves the environment first, then the Keys file, then the Old Key directory (same rule as
  R-105) (T12).
- R-108 No Key is typed or pasted into swarg: there is no input for one (no `/keys set`, no hidden
  field), and a `/keys` line with a value or any unknown word is refused with a usage text that does
  not echo the line (T12).
- R-109 A chat message that looks like an API key (`sk-` + 20 or more of `A-Za-z0-9_-`, or a GitHub
  token `ghp_`/`gho_`/`ghu_`/`ghs_` + 30 or more) is not sent on the first Enter: it stays in the
  input and a notice (without the text) points at the Keys file (`/keys` shows where). The same text sent again goes through;
  an edited text warns again. `/keys ...` lines are exempt (T12).
- R-110 The Keys file template is made only on request (`/keys init`, or `i` in the keys menu), never
  by itself: a commented file, mode 0600, with a `# NAME=` line for every Key name the config uses,
  sorted. It never overwrites an existing file (the request is refused with a notice). The config
  dir is created 0700 when missing (T12).
- R-111 Spend: the cost of each response (`usage.cost` in USD, read when the server sends it; OpenRouter
  does so for every response, no request field is needed) is added to the Session's Spend, in total and
  per model id (the model of the request, Task and Compaction requests included). A server that sends
  no cost, a zero, a negative or a malformed one adds nothing. Every cost is a `spend` line in the
  SessionFile; resuming or opening a Session restores the Spend (T13).
- R-112 The Bottom panel shows the Spend (`$0.0012`: 4 decimals under a cent, 3 under a dollar, 2 from a
  dollar) right after the context meter, only while it is above 0 (T13).
- R-113 `/cost` prints the Session Spend with the models, dearest first; for a Source of type
  `openrouter` it then asks `GET /key` and prints the key's label, usage today / week / month / all
  time and the limit with what remains (or `no limit`). Another Source, or a failed query, leaves the
  Session part alone; a failure adds one line with the first line of the error (T13).
- R-114 `cost.warn_usd` (USD, 0 = off, default off): when the Spend first reaches it in a Session, one
  warning is shown (`spend $X reached the warning level $Y`); a Session resumed already over the level
  is not warned again. There is no hard limit and the warning never stops, refuses or delays a
  request or a Turn (owner decision 2026-10-31) (T13).
- R-90 An unknown Tool name in a ToolCall is an error ToolResult that names the offered Tools
  sharing a word (3+ letters, split on `_`/`-`, `mcp` ignored) with it: `unknown tool save_tool;
  did you mean mcp__demo__save?` (at most three). T8 eval: the 9B called `save_tool` for an MCP tool.

- R-115 The SystemPrompt has three Prompt Profiles, cumulative: `compact` is the base Block alone (byte-identical
  to the prompt before T13), `standard` adds the standard layer, `full` adds the full layer on top of
  it. The layers are Blocks between the base and the tool Blocks; a layer is in the Set (`/blocks`,
  `List`) only while its Profile is in force. A Profile change applies from the next request (T13 B).
- R-116 The Profile a Session starts with: the most specific `prompt.profiles` model pattern that matches
  the model (longest pattern), else `providers.<p>.profile`, else `prompt.profile`, else `compact`. It
  follows a Model or Source change until the user chooses a Profile or a resumed Session carries one. An
  unknown name in the config is a config error naming the known ones (T13 B).
- R-117 `/prompt` shows the Profiles with their size and marks the one in force; `/prompt <name>` (or
  Enter in the menu) applies one to this Session; with `global` or `project` (key `s` in the menu) it is
  also written as `prompt.profile`; the `/config` row `prompt profile` steps through the Profiles. An
  unknown name is refused with the known ones and changes nothing (T13 B).
- R-118 A Profile the user chose is recorded in the SessionFile (`{"prompt":"<name>"}`, the last one
  wins) and holds for the Session: a resumed Session, or one opened in the app, continues with it
  instead of the config's Profile; a Session without the record leaves the Profile to the config
  (T13 B).
- R-119 A file `profile-standard.md`, `profile-full.md` or `subagents.md` in the blocks folder replaces
  that layer's text (like R-49; the warning is printed at load). `subagents` is the sub-agent policy
  layer: filled by main with the enabled agent types (R-123; empty when none), on in `standard` and `full` only, and an empty layer leaves
  no gap in the SystemPrompt (T13 B).

- R-120 An Agent starts with an empty history and only the goal (framed as a job of the main agent); it
  uses the Source and model of its type (`server`, `model`; empty or `inherit` = the Session's), the
  system prompt built for its type (the type's profile layers, the Tool lines it may use, its body, the
  project file, the env block with its own model; never the sub-agent policy), and its own step limit
  (`max_steps`, else `tasks.max_steps`). Its Messages never enter the Session history or the Store (R-37
  and I-14 hold) (T13 C).
- R-121 An Agent type allows tools by an allowlist: names, `*` (all) and `readonly` (every read-only
  tool); empty = all. A call outside it is an error ToolResult and does not run. Built-in types:
  `general` (all), `explore` (readonly), `plan` (readonly, returns a plan), `review` (readonly and
  `shell` for tests and read-only git). Types live in `<config dir>/agents/<name>.md` and
  `<project>/.swarg/agents/<name>.md`; project beats global beats built-in; front matter `description`
  is required. `/agents` lists the types, switches one (saved as `agents.disabled` in a scope) and
  `new <name>` writes a template file (T13 C).
- R-122 Several `agent` calls of one step run at once. An Agent needs a Slot of its Source
  (`providers.<p>.max_parallel`, default 1 for `openai`/`ollama`, 4 for `openrouter`; 1..4) and one of the
  overall Slots (`agents.max_parallel`, default and maximum 4); without one it waits, holding nothing,
  and a wait ends with Esc. Agents are not on the Chain: Interrupt and Abort walk the running Agents too
  (T13 C).
- R-123 The `agent` Tool is offered only to the Session (never inside an Agent or a Task) and only while
  agent types are on offer. Under the `compact` Profile none are, so compact never offers or describes it
  (I-40); `standard` and `full` fill the `subagents` layer with the enabled types and the rules for
  delegating, and a switch in `/agents` rewrites it (T13 C).
- R-124 Two Agents never write one file at the same time: the Run of a call that changes a file holds a
  lock on its path; a second simultaneous call on that path is an error ToolResult naming the holder, and
  nothing is written. The AgentResult lists the files the Agent changed (R-38) (T13 C).
- R-125 A call of an Agent that needs the user's Approval asks through the same prompt as the Session's,
  one prompt at a time; the others wait, and Esc ends the wait. Two denied calls end the Agent
  (`stopped: tool calls were denied`) (T13 C).
- R-126 Each running Agent is one block of the Session list: its `agent` call, labelled `<type>: <goal>`,
  with live lines `model <id>` and `step N/M <call>`; the text, tools and meter of an Agent never reach
  the list. Esc interrupts all Agents (the second Esc aborts them); the cost of their requests adds to the
  Session spend (R-111) (T13 C).
- R-127 An AgentResult has the TaskResult format (R-38). An `agent` call with an empty goal, an unknown
  type, the goal of an Agent already running, or past the per-Turn limit (shared with Tasks,
  `tasks.max_per_turn`) is an error ToolResult without a model request (T13 C).
- R-128 The chosen Agent types are kept globally: `/agents` (menu Space / label, `/agents on|off <name>`)
  saves `agents.disabled` to the GLOBAL scope by default, and `/agents new <name>` writes the type file
  into the global agents folder `<config dir>/agents/`, even when the project has a `.swarg` folder
  (unlike `/skills`, which defaults to the project then). Only an explicit `project` argument, or Tab in
  the menu, uses the project (`.swarg/config.json`, `.swarg/agents/`). With no Scopes the switch is for
  this run only (T13 global agents).
- R-129 An Agent type comes from, strongest first, a project file, a global file, the built-in of that
  name. `/agents edit <name>` (key `e` in the menu) copies a built-in's full definition (front matter and
  body) into `<config dir>/agents/<name>.md` unless that file exists (never overwritten) and reports the
  path; for a type that already has a file it reports that file. A changed, added or removed agent file
  applies without a restart: the folders are re-read when `/agents` is opened, after `new` / `edit`, and
  at the next agent request (the `subagents` prompt layer is rewritten). Which types are off is kept.
  Each Source has a default prompt Profile: the `openrouter` preset is `standard` (eval B4:
  compact 15/16, standard 16/16, full 14/16), local presets stay `compact` (T13 global agents).

## Invariants

Invariants are numbered `I-n`; each one is a test target.

- I-1 At most one Turn runs at a time; the Session's own Provider requests never overlap, and none runs
  while its Agents run. Agent requests overlap only within the Slots (R-122), at most 4 at once.
- I-2 Every stream ends with exactly one done event.
- I-3 Every assistant ToolCall in the history is followed by exactly one tool Message with its id.
- I-4 Tools never touch files outside the project folder (`shell` is the one exception and asks for
  Approval).
- I-5 Only the running detachable ToolCall can be moved to the background by the user.
- I-6 The SessionFile replayed by Load equals the live history at every Turn end.
- I-7 A failure to write the Trace or the SessionFile never fails a Turn; it is reported once as a Warning.
  After a SessionFile error nothing more is written to it; a Load (resume or a switch in the app) tries
  the Store again.
- I-8 A text sent from the UI is shown exactly once in the conversation (first waiting, then taken),
  never lost and never doubled, in send order.
- I-9 Every model request names the model chosen when it was built; replaying the SessionFile gives
  the same last model as the live agent.
- I-30 Every thinking switch is recorded in the SessionFile; a resumed Session continues in the last
  recorded mode, a file with no thinking line (older swarg) resumes with thinking off.
- I-31 No MCP Server failure stops or crashes swarg: a server that does not start is a warning, one
  that exits later makes its Tools answer with the state, a hung call ends with the timeout; the
  built-in Tools and the other servers keep working (T8 M). A url server down, refusing (4xx/5xx)
  or forgetting its Session is the same kind of failure (T9).
- I-33 A project file never lowers safety: whatever it says, auto-approve, the web and MCP
  confirmations and the trust of a server stay at the global values (R-96) (T10).
- I-34 An MCP Server defined by a project runs nothing before the user's approval for that
  project and that entry (R-97) (T10).
- I-35 An API key never leaves the HTTP client: it is not in a config value, a SessionFile, a Trace,
  a notice or any error text (the status texts carry the status and the body snippet only) (T11).
- I-39 Spend only informs: a missing, zero or malformed cost never fails a request or a stream, no
  Spend or warning step can stop or delay a Turn, and the API key is in no `/cost` text (key queries
  report errors by status only) (T13).
- I-36 A Source switch never changes a request in flight: the running stream ends on the old server;
  only requests built afterwards use the new one (T11).
- I-37 A Key value exists in exactly one file, the Keys file (or the Old Key directory until it is
  moved), and in memory in the HTTP client and the MCP transport: not in `config.json`,
  `approvals.json`, `recent.json`, a SessionFile, a Trace, a notice, a warning, a menu row (only the
  mask), or any error text (extends I-35 and I-32) (T12).
- I-38 Swarg has no input that takes a Key value: a Key never passes the chat input, the Waiting
  Messages, the history sent to the model or a notice through the keys UI (R-108); a key-like chat
  message is held back (R-109) (T12).
- I-32 The values of `mcp.servers.<name>.headers` never leave the HTTP Transport: not in a trace, a
  SessionFile, a log, a warning, a ToolResult, `/mcp <server>` or any error text (header names may
  be named, values never) (T9).
- I-10 Moving the pointer changes nothing but the look: no scroll, no selection, no toggle, nothing
  reaches the agent (T3 ui).
- I-12 The SessionFile replays to the parent history with the TaskResults and without the Tasks'
  Messages (I-6 holds with Tasks); the Trace has every request and ToolCall of every Task, tagged
  with the Task id and depth (T4 task).
- I-13 Every model request is measured against the ContextLimit of the model it names: the meter
  limit of the Session and of every Task equals `LimitFor(model)` at the time of the request (T4 task).
- I-14 A Task's Messages never reach the Store (T4 task).
- I-11 A Name never changes the history: a SessionFile replays to the same Messages with or without
  its Name records (T3 ui).
- I-15 A model-chosen URL never makes a request to a private address: the check runs on the address
  that is dialed, for every redirect hop (T4 tools).

- I-40 The `compact` SystemPrompt never changes with a Profile: no layer text, no sub-agent policy and
  no Block or Profile name ever reaches it (and no internal name reaches any Profile) (T13 B).

- I-41 An Agent has no `agent` and no `task` Tool: Agents never nest (T13 C).
- I-42 Nothing of an Agent reaches the Session but its AgentResult and its cost: not its Messages, its
  streamed text, its tool output or its meter (T13 C).

## Open questions

- Q-1 The Hook list of mods (to be designed in a later task). Decided in T8 M: a mod that offers Tools is an MCP Server (`mcp.servers`); only hooks still need a protocol.

## Out of scope

- Remote/cloud model providers beyond the OpenAI-compatible API.
