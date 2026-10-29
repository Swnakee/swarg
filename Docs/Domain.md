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
| Provider | A model backend the core streams completions from (local mlx, later OpenRouter); reached only through the `agent.Provider` interface | backend, client, API | `Provider` |
| StreamEvent | One event of a streamed completion: text, prefill progress, usage, or the final done | chunk, token | `chat.StreamEvent` |
| SystemPrompt | The first message of every request, assembled from Blocks | preamble, instructions | `prompt.Set.Build` |
| Block | One switchable piece of the SystemPrompt (base, global `NN-name.md`, project `SWARG.md`, env, tool blocks); a `<name>.md` override file replaces a built-in one (R-49) | snippet, section, part | `prompt.Block` |
| BackgroundJob | A shell command that outlives the ToolCall that started it; read and killed later by id (`bg1`, `bg2`, ...); not a Task (agent work) | daemon, task, process | `tools.Jobs` / `tools.JobInfo` |
| Detach | The user's move of the running detachable ToolCall to the background; the call returns at once and its command becomes a BackgroundJob | suspend, push to bg | `ToolHooks.Detach`, `Agent.Background` |
| Mod | An external process that extends swarg through the JSON protocol | plugin, extension | `Mod` |
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
| Thinking mode | On or off: with it on every request asks the model to think (`enable_thinking`), uses the thinking sampling and a larger output limit; off by default; switched with `/think` | reasoning switch, think flag | `Agent.SetThinking`, `agent.EvThinking`, `config.Provider.Thinking` |
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
- R-48 The note in a cut tool result names what gets the rest back: for command output "rerun with a
  filter (grep, head, tail)", for files and other results "use read_file offset/limit or grep"
  (T4 tools).
- R-49 A file `<name>.md` in the global blocks folder, named after a built-in Block (`base` or a
  `tool-<name>` block), replaces that Block's text (global `NN-name` Blocks are not replaceable); the Block keeps its order and on/off state, and
  the replacement is reported as a warning at start. `project`, `env` and unknown names are not
  replaced (warning). This is how prompt variants are compared on one build (T5 prompts).

## Invariants

Invariants are numbered `I-n`; each one is a test target.

- I-1 At most one Turn runs at a time; Provider requests never overlap.
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

## Open questions

- Q-1 The Hook list and the Mod protocol (to be designed in a later task).

## Out of scope

- Remote/cloud model providers beyond the OpenAI-compatible API.
