# Module config (`App/internal/config`)

Purpose: load and validate the swarg config file (`config.json` in `Dir()`). Stdlib only.

API:
- `Provider{Type, BaseURL, Model, ContextLimit, ContextLimits, MaxTokens, APIKeyEnv, ExtraBody}` - one model server (JSON tags as in the file).
- `Provider.ContextLimits map[string]int` (JSON `context_limits`, omitted when empty) - per-model context limit, model id -> tokens, each value > 0 (checked for the active provider). Not set in `Defaults()`.
- `(Provider) LimitFor(model string) int` - the limit of one model: the file's `context_limits` entry, else the built-in table `defaultLimits` (the Qwen3.5 ids `0.8B`/`2B`/`4B`/`9B` `-4bit` and the fine-tunes `Huihui-Qwen3.5-4B-...-4bit`, `MiMo-V2.6-Distill-Qwen-9B-OptiQ-4bit` -> 32768), else `ContextLimit`.
- `Tasks{MaxDepth, MaxSteps, MaxPerTurn}` (JSON `tasks`) - the Task mode limits (nested sub-tasks of the agent): defaults 3, 15, 8. `max_depth` 0..8 (0 = Tasks off), `max_steps` 1..200, `max_per_turn` 1..50. A partial object keeps the other defaults.
- `Prompt{WarnShare}` - prompt settings, default 0.25.
- `Tools{Disabled, MaxSteps, ShellTimeoutSec, AutoApprove}` - tool settings (JSON `tools`), defaults `[]`, 25, 120, false.
- `Web{Confirm, SearxURL, AllowPrivate, TimeoutSec}` (JSON `web`) - web tools: defaults true, "", false, 15; `timeout_sec` 1..120, `searx_url` empty or an http(s) URL; `confirm: false` frees `web_search`/`web_fetch` from approval (R-45); `allow_private` lets `web_fetch` reach private addresses (R-47).
- `Session{Save, Keep, Trace}` (JSON `session`) - defaults true, 50, true. `Context{AutoCompact}` (JSON `context`) - default 70; 0 = off, otherwise 50..95.
- `Config{Provider, Providers, Prompt, Tools, Web, Session, Context, Tasks}` - whole file; `Provider` names the active entry.
- `Dir() (string, error)` - `os.UserConfigDir()/swarg`.
- `Defaults() Config` - provider `local` (openai, `http://127.0.0.1:8080/v1`, `mlx-community/Qwen3.5-4B-4bit`, 16384 context, 2048 max tokens, `extra_body` with `enable_thinking` false and the Qwen3.5 non-thinking sampling: `temperature` 0.7, `top_p` 0.8, `top_k` 20, `min_p` 0, `presence_penalty` 1.5, `repetition_penalty` 1.0).
- `Load(path string, explicit bool) (Config, error)` - read, decode, validate.
- `(Config) Validate() error` - checks the active provider (including its `context_limits` values), `prompt.warn_share`, `tools.max_steps` (1..200), `tools.shell_timeout_sec` (1..3600), `session.keep` (>= 1), `context.auto_compact` (0 or 50..95) and the `tasks` ranges.
- `tools.background_grace_sec` (default 10, 0..600; 0 = background at once) and `tools.output_wait_sec` (default 10, 0..600; how long shell_output waits for a job end).
- `(Config) Active() Provider` - the active provider (zero value if undefined).
- `(*Config) Override(model, baseURL string)` - sets non-empty values on the active provider; call `Validate` afterwards.
  The cmd applies `opts.providerOverride()`: the `--model`/`--base-url` flags, else the `SWARG_MODEL`/`SWARG_BASE_URL` environment variables (flags win), so plain `swarg` inside the Docker sandbox reaches `http://model:8080/v1` without flags.
- `(Provider) APIKey() string` - value of the env var named by `api_key_env`, `""` if unset.

Invariants:
- Missing file: `Defaults()` when not explicit; error naming the path when explicit.
- Unknown fields and trailing data after the JSON value are errors.
- A provider entry in the file replaces the default entry of the same name as a whole (no field merge).
- A `tools` object in the file is decoded onto the defaults: missing keys keep their defaults; a file without `tools` keeps all of them.
- Validation errors start with `config: `.

Gotchas:
- `Validate` checks only the active provider, not the other entries.
- `Override` does nothing when the active provider is missing; `Validate` then reports it.
- `APIKey` never reads the key from the file; only the env var name is stored.
- A user `extra_body` replaces the whole default map, sampling keys included; set them again in the file if they are still wanted.
- The built-in context table (`defaultLimits`) beats an explicit `context_limit` for its model ids: `context_limit: 4096` with model `mlx-community/Qwen3.5-4B-4bit` still gives 32768. To use less for such a model, set `context_limits` for that id in the file.
- The table is deliberately NOT in `Defaults()`: a file that defines `providers.local` replaces the whole entry, which would silently drop a table stored there. `LimitFor` reads it from the package var, so it survives such a file.

## cmd/swarg (main, flags, wiring)

Files: `main.go` (run, wiring), `flags.go` (`parseArgs`), `sessions.go` (session picking, `saving` = the files being written + `ui.SessionsAPI`, `cleanups`), `tracecmd.go` (`swarg trace`), `jobs.go` (jobs adapter, `killOnSignal`), `tools.go`.

Flags: `--config`, `--model`, `--base-url`, `--yes`, `--version`, plus
- `--continue` resume the newest readable session of this project (none: notice, new session);
- `-c` short for `--continue`;
- `--resume <id>` / `-r <id>` / `--resume=<id>` resume that session; an id prefix is enough; ambiguous or unknown id is an error (exit 1) listing the candidates; not combinable with `--continue`;
- `-r` / `--resume` WITHOUT an id (last arg, or followed by a `-flag`; `scanPick` pre-scans because Go's flag package has no optional values) = pick mode: on a terminal the `ui.PickSession` full-screen picker lists this folder's sessions (cancel: exit 0, nothing starts; chosen id then continues like `--resume <id>`); no sessions: `no saved sessions in this folder`, exit 0; without a terminal the list goes to stdout, hint `use swarg -r <id>` to stderr, exit 2 (`choosePick`);
- `--sessions` print `id  started  model  messages  title` of this project, newest first, exit 0;
- `--no-save` no SessionFile (and no Trace); `--trace-off` no Trace.
Subcommand `swarg trace [id]` (os.Args[1] == "trace", before flag parsing): prints `trace.Timeline` of the session (id prefix) or the newest of this project; no trace file is exit 1; extra arguments are exit 2.
Exit codes: 0 ok, 1 runtime error (also unknown session id), 2 config or flag error.

Model at start (`startModel`, R-30): `--model` > the resumed session's last model (`session.Loaded.Model`) > config/`SWARG_MODEL`. The agent starts with the session's own model and main then calls `SetModel(model)`, so a `--model` over a resumed session is recorded. `onModel` wraps `OnEvent`: an `EvModel` first rewrites the prompt env block (`prompt.Set.SetModel`). `Health` checks `a.Model()`. `ui.Deps.Models = modelsView{client}` (`openai.Client.Models`).

Wiring: project session dir = `session.Dir(config.Dir(), cwd)`. Unless `--no-save` / `session.save: false`: `session.Prune(dir, keep)`, then `session.Create` (or `session.Open` of the resumed file, whose history goes in through `agent.Load(..., WithMaxSeq)` and `ui.Deps.History`) -> `agent.Deps.Store`; Trace via `trace.Open(session.TracePath)` unless `--trace-off` / `session.trace: false` -> `agent.Deps.Trace`. Plain mode saves too. A problem creating the files is a stderr warning and the run goes on unsaved. Files are closed by `cleanups` on every exit path including SIGHUP/SIGTERM (`killOnSignal`); a new session that got no message is deleted again. After the UI ends, `session saved: swarg --continue` goes to stderr when the session has at least one message.

`saving` (T3 ui, R-33): the `*session.Writer` and `*trace.Writer` of the moment behind one mutex; the agent's Store (`swStore`) and Trace (`swTrace`) forward every call to them. It is `ui.Deps.Sessions` (`ui.SessionsAPI`): `List` (readable sessions with Messages), `Current` (id of the file being written), `Open(id)` (an id prefix is enough: closes the current files - a new file without a message is removed -, reopens the chosen SessionFile and its Trace with `session.Open` / `trace.Open`, returns `ui.Opened{History, MaxSeq, Model, Title}`; opening the current session or any session while saving is off only returns the history; a file that cannot be opened for writing leaves the current one in place), `Delete(id)` (`session.Delete`; the current session is refused, R-34), `Rename(name)` (`Writer.Name`; "sessions are not saved" without a writer). A model change should be recorded through the writer of the moment (`saving.w` under `saving.mu`), never through one captured at start.

Gotchas:
- Opening a session in the UI SWITCHES the file: what happens next is appended to the opened SessionFile (a true continuation). `session saved: swarg --continue` still names `--continue`, which resumes the NEWEST file by start time - after a switch to an older session use `swarg -r <id>` (the picker shows it).
- A new file that got no message is removed on exit or switch, unless it was renamed: a Name counts as content (`Rename` clears `fresh`).
- Resuming appends to the same SessionFile and Trace (the Trace `ms` counter restarts at 0; also after every switch).
- `agent.Deps.AutoCompact` is not wired yet (`TODO(W2.1b)` in `main.go`).
