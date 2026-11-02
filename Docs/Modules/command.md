# Module command (`App/internal/ui/command`)

Purpose: the slash commands. One table drives parsing, `/help` and the command suggestions. Pure text logic: no Bubble Tea, no other swarg import. The host (`ui`, `model.submit`) switches on `Command.Kind`.

API:
- `Parse(line string) (Command, error)` - the line is trimmed; text that does not start with `/` and `//x` give `KindText`; an unknown `/x` is `unknown command /x (see /help)`; a command without arguments given any is `usage: /x`; each command has its own usage error. Commands are case-sensitive.
- `Command{Kind, Text, Name, On, N, Scope}` - what the line asked for (`Text` the message / goal / action word, `Name` the target, `On` for on|off, `N` for `/tool N`, `Scope` global|project|"").
- `Kind` and the `Kind*` constants (`KindText`, `KindExit`, ... `KindPrompt`).
- `Spec{Name, Args, Desc, Kind}` and `All() []Spec` (the table in display order; do not change it), `Lookup(name) *Spec` (nil when unknown).
- `Help() string` - the `/help` notice: every command with its arguments and description, aligned, in table order.

Invariants:
- Adding a command = one `Spec` entry (with `parse` when it takes arguments) plus its `case` in the host; `/help` and the suggestions follow the table.
- `/keys` usage errors never echo the line (a mistyped word may be the key itself).

Gotchas:
- `Spec.parse` stays unexported: only the table can bind a parser.
