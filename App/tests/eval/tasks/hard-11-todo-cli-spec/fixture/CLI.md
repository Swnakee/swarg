# todo.py

`python3 todo.py [--db PATH] COMMAND [ARGS]`

State is a JSON file, `--db PATH` (default `todo.json` in the current directory). A missing file is
an empty database; it is created on the first change. The environment variable `TODO_TODAY`
(`YYYY-MM-DD`) overrides today's date everywhere a date is needed (tests use it).

## Exit codes

| code | meaning |
|---|---|
| 0 | success |
| 2 | usage error: unknown command, missing/invalid arguments, empty text, bad priority, bad date, no command (argparse's own errors also use 2) |
| 3 | `done` on a task that is already done |
| 4 | no task with that id |
| 5 | the database file exists but is not valid JSON of the expected shape |

Errors are written to stderr as `error: <message>` (argparse usage errors may print its usual text);
stdout stays empty on errors. Nothing is changed in the database when a command fails.

## Commands

### add TEXT [--prio N] [--due YYYY-MM-DD] [--tag TAG ...]
* TEXT is one argument; leading/trailing whitespace is trimmed; empty after trimming is a usage error (2).
* `--prio` is 1, 2 or 3 (1 = most urgent), default 2.
* `--due` must be a real calendar date, otherwise 2. `--tag` may be repeated; tags are lowercased,
  trimmed, empty tags are ignored (not an error), duplicates removed, order of first appearance kept.
* Ids start at 1 and are never reused, even after `rm`.
* Prints `added #<id>`.

### list [--all] [--tag TAG] [--sort id|prio|due]
* Without `--all` only open (not done) tasks are listed. `--tag` keeps tasks having that tag
  (compared lowercased).
* Sort: `id` (default) ascending id; `prio` ascending priority number then id; `due` ascending date, tasks
  without a due date last, ties by id.
* One line per task: the id right-aligned to width 3, then ` [x]` if done or ` [ ]` if open,
  then ` p<prio>`, then ` <due>` (or ` ----------` without a due date), then ` <text>`, then
  ` #<tag>` for every tag. Example: `  7 [ ] p1 2024-05-01 Pay rent #home #money`.
* If nothing matches it prints `no tasks`. Exit code 0.

### done ID
Marks the task done, prints `done #<id>`. Already done: error `task #<id> is already done`, exit 3.
Unknown id: `no such task #<id>`, exit 4. A non-integer id is a usage error (2).

### rm ID
Removes the task, prints `removed #<id>`. Unknown id: exit 4 as above.

### stats
Prints exactly four lines: `total: N`, `open: N`, `done: N`, `overdue: N`. Overdue = open tasks whose
due date is strictly before today.

### export --format csv|json [-o FILE]
All tasks (done too) ordered by id. Output goes to stdout, or to FILE (then stdout stays empty).
* json: a JSON list of objects `{"id", "text", "prio", "due", "done", "tags"}` (`due` is null when
  absent, `done` is a boolean, `tags` a list).
* csv: header `id,text,prio,due,done,tags`, then one row per task; `due` empty when absent,
  `done` is `yes` or `no`, `tags` joined with `;`. Standard CSV quoting (use the csv module).
* `--format` is required; any other value is a usage error (2).
