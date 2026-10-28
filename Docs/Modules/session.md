# Module session (`App/internal/session`)

Purpose: the SessionFile - an append-only JSONL record of one Session, written as it happens and replayed exactly on resume. Depends only on `chat`.

Files: `dto.go` (versioned records, `toDTO/fromDTO`), `writer.go`, `store.go` (adapter for `agent.Store`), `load.go` (replay), `list.go` (List, Prune), `dir.go` (Dir, TracePath).

API:
- `Dir(base, cwd) string` - `base/sessions/<slug>-<8 hex sha256>`; cwd normalized by EvalSymlinks + Clean, lowercased on darwin/windows.
- `TracePath(sessionPath) string` - `<dir>/traces/<file stem>.trace.jsonl`.
- `Create(dir, Header) (*Writer, error)` - dir 0700, file 0600 `YYYYMMDD-HHMMSS-<id6>.jsonl`, header line written; `Open(path)` continues an existing file (cuts a torn tail, continues seq).
- `(*Writer).Message(m) (seq, err)` - append; `seq` is `m.Seq` when it is above every seq written so far (the agent numbers its history), else the next free one (monotonic per file, from 1). `Compact` and `Repair` follow the same rule.
- `NewStore(w) *Store` - `agent.Store` adapter: the same calls with error-only results (`Message`, `Patch`, `Clear`, `Compact`, `Repair`, `Model`, `Flush`); no state of its own.
- `Patch = chat.Patch{Seq, Stub, Args}` (alias) - `Patch([]Patch)` - stub batch / arg shrink; `Args` maps tool-call id to shrunk raw JSON.
- `Clear()`, `Compact(fromSeq, toSeq, summary) (seq, err)`, `Repair(msgs) (seqs, err)`, `Model(name) error` (a Model choice line `{"model":"<id>"}`; blank is an error), `Flush()` (fsync), `Close()`, `Path()`, `ID()`.
- `Name(name) error` (R-32, T3 ui) - records the Session's Name as one line (`strings.Fields` joined by one space); a blank Name is `ErrBlankName` and writes nothing; the last Name wins; `Clear` does not touch it; the history is never changed by it (I-11).
- `Load(path) (Loaded, error)` - `Loaded{Header, Name, Entries []Entry{Seq, Msg}, Dangling []Dangling{Seq, Call}}`, `Messages()` (each message has `Seq`, `Stub`, `Brief`, `Label`, `Failed`, `TurnStart`, `Injected`, `Summary` set), `MaxSeq()` (highest seq ever used, also by cleared or compacted messages; pass it to `agent.WithMaxSeq`), `Model()` (the last model line, else `Header.Model`: a resumed Session continues with it, R-30). `Name` is "" for a file never named.
- `List(dir) ([]Info, error)` - newest first; `Info{ID, Path, Started, Model (the last chosen, `Loaded.Model()`), Title (60 runes), Messages, Err}`; `Title` is what lists show: the Name when set, else the first user message; a broken file has `Err` and sorts last.
- `Prune(dir, keep) error` - removes files beyond the newest `keep` with their traces; `keep <= 0` keeps all; files modified in the last hour are spared.
- `Delete(dir, id) error` - removes the one SessionFile of `dir` whose id is `id` (as List/the picker show it, parsed from the file name so broken files delete too) and its Trace; a missing Trace is fine; an unknown or ambiguous id is an error. Used by the `swarg -r` picker (Ctrl+D) so a looping or unwanted Session can be erased and never reloaded.

Format (v1), one record per line: header `{v,id,cwd,model,started}` first, then `msg`, `patch`, `clear`, `compact {from_seq,to_seq,summary}`, `repair [msgs]`, `name "<Name>"`. Records are DTOs with JSON tags, not `chat.Message`. Unknown keys are ignored, so an older swarg reads a newer file (minus what it does not know) and a file without the newer records reads as before.

Invariants:
- Each record is one `write` on an `O_APPEND` file; the file only grows.
- Replay equals the live history (proved by `agent.TestStore_SessionFileReplaysToTheLiveHistoryAtEveryTurnEnd`): `clear` empties (seq numbering continues), `compact` replaces the history positions of `from_seq`..`to_seq` by the summary entry (addressed by seq, never by line or index), `patch` sets `Stub` / replaces call args.
- An unterminated last line is a torn write and is skipped; a bad line anywhere else, an unknown patch/compact target, a missing header or `v != 1` is an error naming file and line. Unknown record keys are ignored (forward compatibility): an older swarg skips a model line (it decodes to an empty record). A model line is a non-header line with `model` set (`record.Model` is shared with the header).
- `Load` never invents messages: calls without results are returned in `Dangling`; the agent appends cancelled results through `Repair` so file and live history match.
- Writer errors are returned, never swallowed; the caller shows one Warning. After `Close` every method returns `ErrClosed`.

Gotchas:
- `msgRecord.reasoning` (omitempty) stores `Message.Reasoning`; `TestLoad_RoundTrip` covers it.
- A new `chat.Message` field needs one line each in `msgRecord`, `toDTO` and `fromDTO` or the replay (I-6) loses it; `agent/store_roundtrip_test.go` and `TestLoad_RoundTrip` catch that.
- Sessions are keyed by the normalized cwd; a renamed directory is a new project.
- Prune cannot know a file is in use; it relies on the one-hour modification guard.
- Session files contain every message including tool output (see trace.md on privacy).
