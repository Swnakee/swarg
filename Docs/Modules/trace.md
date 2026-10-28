# Module trace (`App/internal/trace`)

Purpose: the debug Trace - a JSONL log of everything the agent did in a Session, for `swarg trace [id]`. No dependencies on other swarg packages; the agent sees it as `Record(kind string, v any)`.

API:
- `Open(path) (*Writer, error)` - parent dir 0700, file 0600, append. Path from `session.TracePath`.
- `(*Writer).Record(kind, v)` - one line `{"t":RFC3339,"ms":elapsed since Open,"kind":...,"d":payload}`, written at once (crash-safe, no buffering). Nil-safe, concurrent-safe, no return value.
- `Flush() error` (fsync), `Err() error` (first write error), `Close() error`. All nil-safe.
- `Timeline(r io.Reader, w io.Writer) error` - readable timeline, one line per record: `+12.3s request    step=2 msgs=+3 total=17`.
- `MaxString` (256 KB), `MaxFile` (20 MB).

Payload names the timeline understands (anything else is printed as `key=value`):
- `request`: `step`, `msgs` (total), `added` (list of the appended messages, or a count), `model`.
- `delta`: `text`, `calls` (the agent writes one per model reply, with `finish`). `usage`: `prompt`, `completion`, `cached` (prints `cache=N%`).
- `tool_start`: `name`, `args`. `tool_end`: `name`, `brief`, `ms`, `output`, `error` (prints size and the first 3 non-blank output lines).
- `stub_batch`, `compact`, `approval`, `interrupt`, `abort`, `warning`, `error`: free fields. The agent's exact payloads (including `loop_guard`, `load`, `clear`) are listed in `agent.md`, Persistence.

Invariants:
- A failed write is kept in `Err()` and every later `Record` is a no-op; the Trace never fails or changes a Turn.
- Any string field over 256 KB is cut with a marker naming the original size. Once the file passes 20 MB, string fields over 512 bytes are replaced by `[omitted N bytes...]`.
- Torn or garbage lines and records without a kind are skipped by `Timeline`; unknown kinds are shown generically. A record with a `task` field (made inside a Task) is prefixed `task=<id>` in the timeline.

Gotchas:
- PRIVACY: a trace holds everything the tools saw and the model said - file contents, shell output, secrets included. It is 0600, lives under the user config dir, and is pruned with its session; never attach one to a report unreviewed. `--trace-off` / `session.trace: false` disables it.
- `ms` counts from `Open`; a resumed session appended to the same trace restarts at 0.
- Payloads are re-marshalled only when a cap applies, so key order of capped records is alphabetical.
