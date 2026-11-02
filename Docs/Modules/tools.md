# tools

Purpose: the built-in tools the model can call, the `Set` that registers them, and the shared
helpers every tool uses. Main adapts `Set` to the agent's ToolRunner.

## API

- `Tool` - interface: Spec, ReadOnly, Confirm, Block, Summary, Run.
  Built-in tools get ReadOnly/Confirm from an embedded zero-size mixin: `readOnlyTool` (true/false: read_file, list_dir, grep, shell_output) or `askTool` (false/true: edit_file, write_file, shell, shell_kill); web_search/web_fetch define their own. Summary and Touched stay per tool.
- `Env{Root *os.Root; Dir, OS, Shell; ShellTimeout; Jobs *Jobs; Progress func(string); Detach <-chan struct{}}` - what a tool may touch. `Jobs` nil = background unsupported (`background:true` is an error, `Detach` is ignored); `Progress` gets raw output chunks while a tool runs and is never called after `Run` returns; closing `Detach` moves the running `shell` command to `Jobs`. `BackgroundGrace` (main: 10 s): a `background:true` command runs in the foreground loop for the grace (live `Progress`, Ctrl+B works, no shell timeout); ending within it returns a normal result and no job; 0 = adopt at once (tests). `OutputWait` (main: 10 s): `shell_output` waits up to it for a running job to end (the call ctx stops the wait); 0 = no wait.
- `Result{Output, Brief}` - text for the model, one-line UI summary.
- `All() []Tool` - every built-in tool (all.go). `FreeWeb(list)` returns it with `web_search`/`web_fetch` set `Free` (no approval, parallel; main does this when `web.confirm` is false).
- `webget.Getter{SearxURL, AllowPrivate, Timeout, Lookup, SearchURL}` (`Env.Web`; module `Docs/Modules/webget.md`) - web settings; `Lookup` and `SearchURL` are test seams. `Getter.Get` dials only checked addresses (`blockedIP`) for every hop, no proxy, 1 MB body limit, 5 redirects, default 15 s (I-15, R-47). Search backends are the user's/constant endpoints and may be private; `web_fetch` follows `AllowPrivate`.
- `NewSet(all, disabled)` - unknown disabled name is an error listing known names.
- `Set.Specs / Get / Blocks / Kind(name) / Label(name, args) / Run(ctx, env, name, args)`.
  `Run` recovers panics ("tool panicked: ..."), maps an `os.Root` escape (`path escapes from parent`, e.g. a symlink out) to "path is outside the project folder" and applies `Cap`; `Label` never panics.
- `Decode(args, v, required...)` - strict (unknown fields, null, trailing data are errors);
  `missing required argument "x"` (a `*missingArg`; an empty string counts as missing); empty args mean `{}`. `DecodeFor(t Tool, args, v, required...)` is what every Tool's `Run` uses: a missing argument becomes `missing argument "x"; call <name> like <example>` with the example taken from the Tool's `Block()` (`exampleOf`), R-46.
- `Rel(dir, p)` - model path to clean root-relative path, or "path is outside the project folder". An absolute path inside `dir` keeps its meaning; one outside it is read as written from the project root (volume and leading separators dropped from the RAW path, so "/strutil.py" -> "strutil.py", "/../etc/passwd" and "/etc/../../x" stay refused). Windows UNC and `\\?\` stay refused. R-25.
- `MaxOutput` = 12000. `Cap(s)` keeps the head (2/3) and the tail (1/3) of `MaxOutput`, cut on line boundaries (or UTF-8 starts inside one huge line), and puts `[... N bytes cut; use read_file offset/limit or grep for the rest]` between them (`cutNote(n, hint)`; shell output uses `hintCmd`: `rerun with a filter (grep, head, tail) for the rest`; R-48); the result may exceed `MaxOutput` by the note. The tail keeps `[exit N]` of a shell result. `cutMiddle`/`cutNote` are shared with the shell ring (`shellBodyLimit = MaxOutput-160`, room for the note and the status line).
- History stubs (stub.go, optional interfaces, `Set` dispatches by type assertion; a tool without them gives ""): `Stubber{Stub(args, Result) string}` and `Toucher{Touched(args) string}`. `Set.Stub(name, args, output, brief)` returns "" for an unknown tool, an `error:` result, a panic, or a stub not shorter than the output. `Set.Touched(name, args)` is the cleaned slash path of `edit_file`/`write_file`, else "". Policies: read_file `[read_file a.go: N lines shown earlier; read it again if needed]`; grep `[grep "x": N matches shown earlier]` (`N+` when capped; pattern cut at 40 runes); list_dir `[list_dir p: N entries shown earlier]`; shell `[shell <cmd>: exit N]` + last 3 non-empty lines (last 15 when not `exit 0`; killed keeps `killed: why`); shell_output `[shell_output bgN: status]` + last 3 lines; edit_file, write_file, shell_kill have none (their results are tiny). Every non-empty stub names the tool and the path/pattern/command/id.

- `Jobs` (jobs.go) - background process manager: `NewJobs()`, `Adopt(cmd, command, out *ring, done <-chan error) (id, error)`, `Read(id) (newOutput, JobInfo, error)`, `Kill(id) error`, `List() []JobInfo`, `Done(id) <-chan struct{}`, `KillAll()`. `JobInfo{ID, Command, Running, ExitCode, Started, Bytes}`; `ErrExited` from `Kill` of an exited job.

## Background jobs and shell lifecycle

- `shell` builds the process with `context.WithoutCancel(ctx)`; timeout, cancel and detach are handled by hand in `Run` (select on `done`, `ctx.Done()`, timer, `Detach`). One goroutine calls `cmd.Wait` and sends on `done`; `Adopt` takes the SAME channel. On a signal: cancel wins, then a process that already exited wins, then timeout kills / detach adopts. If adopting fails (8 running jobs) the command stays in the foreground.
- Ids are `bg1`, `bg2`, ... never reused. At most 8 running jobs (`Adopt` errors on the 9th); at most 16 finished jobs are kept for reading (oldest evicted). `Adopt` fails after `KillAll`.
- Job output is a `ring` keeping the LAST 1 MB with absolute offsets. `Read` returns what was added since the last `Read`, at most `MaxOutput-200` bytes, with `[... N bytes lost]` (ring overflowed before it was read) and `[... N more bytes; call shell_output again]` (the offset advances only by what was returned). `shell_output` appends `[running]`, `[exit N]` or `[killed]`, so the result fits `Cap`.
- Exit code convention: `ExitCode` is the process code; -1 when the job was killed (`Kill`, `KillAll`) or died by a signal.
- `Kill` is a no-op (`ErrExited`) for an exited job, otherwise it kills the process group and waits up to 2 s. `KillAll` kills all, waits at most 2 s, outside the lock. Residual risk: pid reuse in the instant between the OS reaping a process and the job being marked exited; swarg SIGKILL cannot run `KillAll`.
- `shell` foreground output also goes through the ring (the first-1-MB rule of the old buffer is gone: over 1 MB the oldest bytes are dropped; the `[... N more bytes]` note counts all left out).
- Progress: `sinkWriter` coalesces to at most one `Progress` call per 50 ms (timestamp check in `Write`, no timer); the pending tail is flushed and the sink silenced (under the same mutex) when `Run` returns or detaches, so no emit follows a return. Chunks are raw bytes, possibly mid-line or mid-UTF-8.
- The timeout does not apply to background jobs and a detached job survives the call context.

## Invariants

- All file access goes through `Env.Root` (`os.Root`); never `os.Open` on a model path. Root
  rejects `..`, absolute and symlink escapes atomically. Reads are confined too.
- `shell` is NOT confined; it asks for confirmation instead.
- Use `Decode`, `Rel`, `Cap` - do not write second versions.

## Adding or removing a tool

One file `<name>.go` (+ `<name>_test.go`) implementing `Tool`, one line in `All()`, and its
`Block()` text. Remove: delete the line. Users can also disable by name in config.

## Gotchas

- Pass `Rel(env.Dir, p)` to Root methods; Root itself rejects absolute paths.
- `Rel` checks lexically; symlink safety comes only from Root (the root-style mapping widens nothing: every access still goes through Root).
- `nestedHint(env, name)` (tool.go, R-66): a relative path whose first segment is `filepath.Base(env.Dir)` while that subfolder does not exist is an error naming the right path (`use "greet.py"`). write_file calls it before creating parents, edit_file only on a missing file. A path that is really meant for an existing subfolder named like the project passes.
- `shell_output` errors: no jobs at all -> "no background jobs: shell_output only reads commands started with shell background:true; to run a command use shell" (an unknown id with no jobs adds `unknown job "id";` in front); an unknown id among jobs -> `unknown job "id"; known ids: bg1, bg2`.
- Windows: drive-relative, UNC, `\\?\` and device names are refused.
- Block order is the registry order (500+i); `Kind` of an unknown name is all false.

## Built-in tools (`all.go`)

| Tool | Kind | File | Notes |
|---|---|---|---|
| `read_file` | read-only | `read_file.go` | `{path, offset?, limit?}`, `N: ` prefixes, default 200 lines (note `[... N more lines; use offset M]`), binary refused |
| `list_dir` | read-only | `list_dir.go` | `{path?, glob?}`, sorted, dirs end with `/`, max 500; with `glob` (base-name pattern, e.g. `*_test.go`) lists matching files at any depth, project-relative, via the shared `walkFiles` (grep.go; skips `.git`, `node_modules`) |
| `grep` | read-only | `grep.go` | `{pattern, path?, glob?}`, skips `.git`, `node_modules`, binaries, files > 2 MB; max 60 matches, lines cut to 160 bytes |
| `edit_file` | confirm | `edit_file.go` | `{path, old, new}`, `old` must be unique; keeps BOM, CRLF and mode; refuses symlinks |
| `write_file` | confirm | `write_file.go` | `{path, content, append?}`, atomic (temp + rename), creates parents, keeps CRLF and mode; refuses symlinks; `append: true` adds after the existing content (R-67) |
| `shell` | confirm | `shell.go` + `shell_unix.go` / `shell_windows.go` | `{command, background?}` in the project folder; process group killed on timeout/cancel; output ends with `[exit N]` or `[killed: ...]`; `background:true` or `Env.Detach` returns `bgN` |
| `shell_output` | read-only | `shell_output.go` | `{id?}` (no id = the latest job, output prefixed `bgN: `; small models drop the id); new output since the last read + `[running]` / `[exit N]` / `[killed]`; Brief `N bytes, running` / `exit N` |
| `web_search` | confirm unless Free | `web_search.go` | `{query}`; DuckDuckGo HTML (`html.duckduckgo.com/html/`, parsed from `result__a` / `result__snippet`, redirect `uddg` unwrapped, ads skipped) or SearXNG JSON (`web.searx_url`); at most 8 numbered results `N. title / url / snippet(200 bytes)` |
| `web_fetch` | confirm unless Free | `web_fetch.go` (text by `webget.HTMLToText`) | `{url, page}`; page text (scripts/styles and nav/aside/footer dropped R-71, `#` headings, `- ` items, links as `text (absolute url)` R-72, charset-aware), plain text/JSON/XML as is, other types refused; pages of 8000 bytes (`splitPages`) with a `[page N of M; ...]` note (R-70) |
| `shell_kill` | confirm | `shell_kill.go` | `{id}`; `killed bgN`, or `bgN already exited` |

Web gotchas: DuckDuckGo may change its HTML or answer with a bot check (HTTP 202/403 or a page with no `result__a`): the result is then `no results` or an HTTP error; set `web.searx_url` for a stable backend. Link urls are resolved against the requested url, not the final one after redirects. The eval `web-*` tasks use a fake web (`App/tests/eval/web/`, README "Fake web"). Tests use `httptest` and the `Lookup` seam, never the internet. A tool that asks for approval is `ReadOnly()=false` so it never runs inside a parallel batch (the contract test forbids read-only + confirm).

Shared unexported helpers (reuse, do not duplicate): `walkFiles` (grep.go), `requirePath` (write_file.go), `splitPages` (web_fetch.go), `isBinary` (read_file.go), `clipBytes`, `clipRunes`, `countLines`, `splitStatus` (stub.go), `pathOf`, `statTarget`, `writeAtomic`, `lineCount` (write_file.go), `nestedHint` (tool.go). Test helpers: `newTestEnv`, `runTool` (read_file_test.go), `listNames` (write_file_test.go); the shell helper-process `TestMain` and its modes are in shell_test.go (only one `TestMain` per package); jobs tests spawn helpers with `spawn`/`start` (jobs_test.go) and gate them through stdin instead of sleeping. Helper processes get `GORACE=atexit_sleep_ms=0`, otherwise every spawn costs 1 s under `-race`.

Known simplification: a file with ANY `\r\n` is written back all-CRLF by edit_file (no "dominant ending" rule).
- `Set.Run` maps a missing file (`fs.ErrNotExist` from os.Root) to `file not found: <path>`; the raw `openat ...: no such file or directory` confused small models (eval robust-01).
