# Module skills (`App/internal/skills`)

Purpose: load Skills (a folder with `SKILL.md`: front matter `name`/`description`, body of
instructions, optional files beside it), list them for the system prompt and apply one through
the `skill` Tool (R-80..R-84). Depends on `tools` (Tool, Cap, DecodeFor, ClipRunes) and the stdlib.

API:
- `Load(globalDir, projectDir string, disabled []string) (*Set, []string)` - `<globalDir>/<name>/SKILL.md` and `<projectDir>/.swarg/skills/<name>/SKILL.md`; a project Skill replaces a global one of the same name; a missing dir means none. Warnings (prefixed `skills: `): a folder name not matching `^[a-z0-9_-]{1,32}$`, no `SKILL.md`, no `description` in the front matter, an unknown name in `disabled`, more than 20 enabled Skills. The folder name is the Skill's name even when the front matter says otherwise.
- `Set.Len()`, `Set.List() []Info{Name, Description, Source, Enabled}` (name order), `Set.SetEnabled(name, on) error` (unknown name lists the known), `Set.Block() string` (the list for the prompt: `Skills (apply one with the skill tool when the task matches):` + `- name: description` per enabled Skill, descriptions cut at 160 runes; `""` when none enabled), `Set.Tool() tools.Tool`.
- `Tool` (`skill`, read-only, no approval): `{name, file?}`. Result = the body of `SKILL.md` without the front matter plus `files: a.md, sub/b.txt` when the folder has other files; with `file` the content of that file, confined to the Skill folder by `os.Root` (`file is outside the skill folder`; a missing file lists the files). Unknown name: `unknown skill "x"; enabled skills: a, b`; disabled: `skill "x" is disabled`. Output goes through `Cap`. Stub (context economy): `[skill x: applied earlier; call it again if needed]`.

Invariants:
- Loaded once at start, like prompt blocks; `SetEnabled` changes only this run.
- A Skill body is model input; nothing of a Skill is executed by swarg.
- CRLF normalized to LF on read (SKILL.md and files).

Gotchas:
- The front matter parser is `key: value` lines between `---` lines, quotes stripped; no YAML
  lists or nesting (a Claude Code SKILL.md with only name and description parses as is).
- Main adds the `skill` Tool and the `skills` prompt block only when `Set.Len() > 0`, so a
  project without Skills pays no prompt tokens (R-89).

T10: `/skills` is a menu (ui `skillsMenu`); a switch is persisted by main's `skillsView.SetEnabled(name, on, scope)` as the whole `skills.disabled` list into the scope's config file (R-98); the package itself is unchanged.
