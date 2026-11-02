# Module prompt (`App/internal/prompt`)

Purpose: assemble the system prompt from named Blocks that can be switched on and off. Depends only on the standard library.

API:
- `Load(globalDir, cwd string, env Env, extra ...Block) (*Set, []string, error)` - builds the set; the second value is warnings. Extra blocks are added as given (Enabled forced true); the caller passes tool blocks this way.
- `Block{Name, Source, Order, Text, Enabled}`; `Source` is `SourceBase|SourceGlobal|SourceProject|SourceEnv|SourceTool`.
- `Env{OS, Shell, Dir, Date, Model, Git}` - renders the env block lines `OS:`, `Shell:`, `Working directory:`, `Git repo: yes|no`, `Date:`, `Model:`. `InGitRepo(dir)` (R-136) walks up from `dir` for a `.git` entry (directory or file) without running the git program; main sets `Env.Git` with it.
- Override files (R-49): `<name>.md` in `globalDir` (no `NN-` prefix) whose stem is the name of a block `Load` already has (`base`, `tool-read_file`, `tool-task`, ...) replaces that block's Text; Order, Source and Enabled stay. Each replacement is a warning `prompt: block "base" replaced by <path>` so a stray file in the real config dir is visible. `project.md`, `env.md`, the name of a global `NN-name` block and a stem that matches no block are skipped with a warning (`tool-task.md` with Tasks off is such a case). This is the vehicle of prompt experiments (T5): variants are compared on one build.
- `(*Set).Build() string` - enabled block texts in order, joined by a blank line. Block names are internal and never appear in the prompt (a name like `tool-read_file` made the 2B call a non-existent tool).
- `(*Set).SetEnabled(name string, on bool) error` - unknown name returns an error listing the known names.
- `(*Set).SetText(name, text string) error` - replaces the text of a run-time block (the `skills` list, the `mcp` instructions); an empty text makes `Build` skip the block without a blank gap. Unknown name is an error.
- `(*Set).SetModel(model string)` - rewrites the env block's `Model:` line (main calls it on `agent.EvModel`); the block keeps its on/off state; Build shows it from the next call.
- `(*Set).List() []BlockInfo` - `{Name, Source, Enabled, Tokens}` per block, in build order.
- Token sizes use `chat.EstimateTokens` (the only estimator in the project).

- `(*Set).BuildAgent(p Profile, model, body string, tool func(name string) bool) string` (R-123) - the SystemPrompt of a sub-agent: base and the layers of `p` (never `subagents`), the Tool Blocks `tool-<name>` whose name `tool` allows (the `skills`, `mcp` and `tool-task` Blocks never), the type's `body` just before the project file, then project and env with `model`. Switched-off Blocks stay out, an unknown profile builds as low, an old name (compact, standard, full) as its new one; the Set is not changed.
- Prompt Profiles (`profile.go`, R-115, R-119, I-40): `Profile` = `Low|Medium|High` (renamed 2026-11-01 from compact/standard/full), `Profiles()`, `ParseProfile(s)` (an old name gives its new Profile, R-134; the error lists the new names; `SetProfile` and `BuildAgent` go through it, so they store the new name). `(*Set).SetProfile(p)` puts the layers of the Profile into the Set and takes the others out (so `List` and `Build` show exactly what is sent; `Load` starts in `Low` with the same Blocks as before); `Profile()`; `ProfileTokens(p)` = estimated size of base + layers (for `/prompt`). Layers (embedded `medium.md`, `high.md`, plus the empty `subagents`): `profile-medium` (Order 50), `profile-high` (Order 60), `subagents` (Order 70, medium and high only; T13 part C fills it with `SetText`, which also survives profile switches). Override files `profile-medium.md`, `profile-high.md`, `subagents.md` (the old `profile-standard.md` and `profile-full.md` still work; the new name wins when both exist) replace a layer's text (warning at load whatever the Profile); the three names are reserved for global `NN-name` Blocks. A layer switched off with `SetEnabled` is on again after a Profile change.

Blocks:
- `base` (Order 0) - embedded `base.md`. It carries rule C1: "The user already sees tool output. Do not repeat it; summarize in 1-2 sentences." (`TestBase_RuleC1_NoRepeatingToolOutput`). Fixed prompt budget (base + the tool blocks + env + tool schemas of `tools.All()`) is <= 1230 tokens (1217 now), guarded by `tools.TestBlocks_TokenCeiling` (tools imports prompt, so the test lives there). The agent's `task` Tool adds its spec and the `tool-task` block (`agent.TaskBlock`, Order 599, added by main when `tasks.max_depth > 0`): under 110 tokens, guarded by `agent.TestTask_PromptCost`, so the whole fixed prompt with Tasks on is ~1230 tokens (under 4% of the 32k default of the Qwen3.5 models).
- `NN-name` (Order NN) - `NN-name.md` files in `globalDir`; a missing dir means none.
- `project` (Order 1000) - the first of `SWARG.md`, `AGENTS.md`, `CLAUDE.md` found in `cwd` (R-135), never from parent or child folders; the others are ignored. No size cap exists.
- `env` (Order 2000) - generated from `Env`.
- extra blocks - given by the caller, e.g. `tool-<name>` with `SourceTool` and Order 500+i.

Invariants:
- All blocks are enabled after `Load`.
- Blocks are sorted by Order, then Name.
- Names `base`, `project` and `env` are reserved: a global file with one of these names is an error, even when `SWARG.md` is absent.
- Global files and extra blocks share one duplicate check: a repeated name or a reserved name is an error.
- A global file name that does not match `^\d{2}-[a-z0-9_-]+\.md$` is skipped with a warning (directories too).
- `Set` is safe for concurrent use (one mutex).

Gotchas:
- The layer texts are plain instructions for swarg's real tools; they have no JSON tool-call examples (notes 10, 11: examples act as templates) and no HTML comments (`TestLayers_NoInternalNamesOrToolJSON`). Fixed prompt with the tool schemas: low 1217, medium 3487, high 6159 tokens (`tools.TestProfiles_TokenBudget`, ceilings 1230 / 3700 / 6500; tuning 2026-11-01, note 21). The medium text says "the project rules file", not SWARG.md, because the file may be AGENTS.md or CLAUDE.md.
- `Build` trims trailing whitespace of each text and CRLF is normalized to LF on read.
- `Tokens` counts the text as `Build` emits it, without the header line.
- `.DS_Store` in the global dir produces a warning, not an error.
- An override keeps the block's Order: `base.md` cannot move the base below the tool blocks. `tools.exampleOf` reads the built-in `Block()`, so the missing-argument hint keeps its example even when an override drops the `Example:` line.
- Switching off a `tool-*` block (`SetEnabled`) only hides its context text from `Build()`; the tool itself stays callable.
