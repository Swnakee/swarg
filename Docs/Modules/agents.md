# Module agents (`App/internal/agents`)

Purpose: load sub-agent TYPES (T13 part C): markdown files with front matter whose body is the
agent's own instructions; list them for the system prompt layer `subagents`. It only describes
types; running an agent is the job of `agent`. Depends on `skills` (`FrontMatter`), `tools`
(`ClipRunes`) and the stdlib.

API:
- `Load(globalDir, projectDir string, disabled []string) (*Set, []string)` - built-ins, then `<globalDir>/*.md`, then `<projectDir>/.swarg/agents/*.md` (`ProjectDir`); a later one replaces an earlier one of the same name (project > global > builtin); a missing dir means none. Warnings (prefixed `agents: `): bad file stem, no `description`, bad `profile`, bad `max_steps`, unreadable file, unknown name in `disabled`.
- `Type{Name, Description, Server, Model, Profile, Tools, MaxSteps, Body, Source, Path, Enabled}`; `Info` is the UI view (no body).
- `Set.List() []Info`, `Set.Enabled() []Type` (copies), `Set.Get(name) (Type, bool)` (enabled or not), `Set.SetEnabled(name, on) error` (unknown name lists the known), `Set.Block() string` (prompt text, `""` when none enabled).
- `Set.Reload() (changed bool, warnings []string)` - re-reads the folders only when a file name, size or mtime changed since the last read (fingerprint); keeps which types are off by name, a new type starts on (R-129).
- `ExportBuiltin(dir, name) (path, created, err)` - writes a built-in's full definition (front matter and body) to `dir/<name>.md`; an existing file is kept (`created` false); a non-built-in name is an error (R-129).
- `NewFile(dir, name) (path, error)` - writes `dir/<name>.md` from a template, creating `dir`; refuses an invalid name or an existing file.
- `ValidName(name) bool` - `^[a-z0-9_-]{1,32}$`.

File format: `---` front matter (`key: value` lines, parsed by `skills.FrontMatter`) then the body.
Keys: `description` (required), `server`, `model` (`""`/`inherit` = the Session's), `profile`
(`""`/`inherit`/`compact`/`standard`/`full`), `tools` (comma list; `*` = all, `readonly` = every
read-only Tool; empty means `*`), `max_steps` (1..200). Other keys are ignored.

Built-ins (compiled in): `general` (all tools), `explore` and `plan` (readonly), `review` (readonly + shell).

Invariants:
- The file stem is the name; a front-matter `name` is ignored.
- `Enabled()` and `Get()` return copies; callers cannot change the Set through them.
- Safe for concurrent use (one mutex). `Set` remembers its two folders (`Load` arguments) for `Reload`.
- CRLF is normalized to LF on read.

Gotchas:
- `Reload` compares mtime and size: two edits in the same tick with the same size can be missed until the next change.
- A global file that fails to parse is skipped with a warning, so the built-in stays in force.
- A file with an invalid `profile` or `max_steps` is skipped, not loaded with defaults.
- The `NewFile` template carries a `TODO:` description: it loads, so the owner must edit it.
- `Block()` clips each description to 160 runes and collapses whitespace into single spaces.
