# Swarg agent evaluation suite (T2_4 W5)

A repeatable benchmark of how well the agent works with a real local model. Every task runs in
the e2e sandbox (`../sandbox/run.sh`: disposable container, no internet, model only through the
proxy), is verified by a script that runs the code, and is reported with trace statistics.

## Run

    App/tests/eval/run.sh -l                      # list tasks
    App/tests/eval/run.sh                         # all tasks, default model Qwen3.5-2B-4bit
    App/tests/eval/run.sh -m mlx-community/Qwen3.5-4B-4bit 'edit-*' 'shell-01*'
    App/tests/eval/run.sh -p <blocks-dir>   # prompt variant: a blocks dir (R-49), run dir gets its name as a suffix
    python3 App/tests/eval/eval.py compare Export/eval/<run A> Export/eval/<run B>   # per-task A -> B table (status, failed calls, steps, completion tokens, wall; totals over the tasks in both runs)

Needs: Colima/Docker running, an OpenAI-compatible model server on 127.0.0.1:8080 or at
`SWARG_MODEL_HOST_URL` (e.g. an Ollama server on another machine; the sandbox proxy forwards to it) (the runner does a 1-token
health check and stops with exit 3 if it is down), the `swarg` build with sessions (`--continue`)
for multi-turn tasks. Tasks run sequentially (single-lane server). If another e2e run holds the
lock the runner waits (up to `SWARG_EVAL_LOCK_WAIT`, default 1800 s). Ctrl+C finishes the current
task, then stops. `SWARG_EVAL_CONFIG=<file>` replaces swarg's config.json in every task
(experiments, e.g. sampling or thinking; keep `"tools":{"auto_approve":true}` in it). `SWARG_RUN_TIMEOUT` (default 1800 s) is the per-task cap of the sandbox.

`-p <dir>` (or `SWARG_PROMPT_DIR`) installs the folder as the swarg blocks dir inside the sandbox
(`sandbox/run.sh` copies it to `/work/.swarg-blocks`, `entry.sh` copies it to the config dir and removes it from `/work` before
swarg starts); the run dir name gets `-<dirname>`.

Output: `Export/eval/<timestamp>-<model>[-<variant>]/`

| File | Content |
|---|---|
| `report.md` | summary per category, table per category, totals, failure reasons |
| `results.csv`, `results.jsonl` | one row per task |
| `runs/<task>/` | the sandbox run: `output.log`, `work/` (resulting project; `work/.eval/` has `output.txt`, `trace.txt`, `traces/*.trace.jsonl`, `result.txt`, `turns.txt`), `diff.patch` |

## Task layout

    tasks/<category>-<nn>-<name>/
        task.md      line 1: one-line description; the rest: user turns separated by a line `---`
        fixture/     files copied to /work (optional)
        check.sh     POSIX sh, runs in /work after swarg ends; prints "PASS: reason" or "FAIL: reason"; exit 0/1
        config.json  optional: the task's own swarg settings (skills, mcp servers), merged over the base config

Categories: `edit`, `multistep`, `shell`, `context`, `long`, `robust`, `language`, `code`,
`web`, `skill`, `mcp`, `mcp-http`, `combo` (the category is the prefix before the first `-nn-`). `skill` (T8 S): the
model picks a project Skill by itself, applies one on `/skill <name>`, cannot apply a disabled one.
`mcp` (T8 M): a fact known only to the demo MCP server (`mcp/mcp_demo.py`, reached from the fixture
through a symlink `tools/mcp_demo.py`), a server that fails to start, a disabled server. `mcp-http`
(T9): the same tools behind `mcp/mcp_http.py` (Streamable HTTP, bearer token) in its own container
`mcp.test` on the sandbox network (`SWARG_MCP_DIR`, set by `run.sh` for these tasks; the token reaches
the agent as `MCP_TOKEN`): a working server by URL, a server without the token (401), a URL nobody
answers. `combo`:
web + skill + MCP in one job. `code` (T7) holds realistic jobs: a failing
suite, a feature across files, a refactor, a CLI with own tests, a stack trace, a 25-file repo, a
250-line CSS, a git flow, a site with login and self-written checks, a JSON-lines tool process.

How a run works (`driver.sh` is the container command):
1. `eval.py prep` copies `fixture/` and puts `check.sh`, `driver.sh` and `turn-NN.txt` into `/work/.eval/`.
2. The driver moves them to `/tmp/eval` (the model cannot see the answer key), sets
   `XDG_CONFIG_HOME=/tmp/home/.config` (copying the sandbox auto-approve config) so sessions can be saved.
3. Turn 1: `swarg < turn-01.txt`; every next turn: `swarg --continue < turn-NN.txt` (same session).
4. `swarg trace` goes to `trace.txt`; the raw `*.trace.jsonl` is copied; the transcript is
   `/work/.eval/output.txt` (turns are separated by `=== turn N ===`); then `check.sh` runs.
5. `eval.py collect` parses `work/.eval/*`; `eval.py report` writes the report.

## Add a task

1. `mkdir tasks/<category>-<nn>-<name>/fixture`, put the starting files there.
2. Write `task.md` (description line, then turns). Write the prompt as a user would; small model, 16k window.
3. Write `check.sh` that verifies by running code or inspecting files, NEVER by the model's words
   alone (the model may edit test files: re-run your own assertions inside check.sh). It may read
   `.eval/output.txt` for transcript heuristics (robust/language tasks do).
4. Prove the check can fail: run it on the untouched fixture (`sh -n` and a dry run), then `run.sh '<task>'`.

## What is measured (per task)

pass/fail + reason; user turns; steps (`request` records); tool calls (`tool_start`); failed calls
(`tool_end` with `error`); denied calls (`approval` with `allow:false`); prompt/completion tokens and
cache % (`usage` records: `cached/prompt`); compactions (`compact`); loop-guard hits (`loop_guard`);
wall time of the agent turns + trace. Without a trace (build older than sessions) counters are 0.

## Known limitations

- `sandbox/run.sh` only accepts a fixture NAME under `sandbox/fixtures/`; the runner puts a temporary
  symlink `_eval-<task>` there for the duration of the run and removes it. A cleaner fix is a
  `SWARG_FIXTURE_DIR` override in `sandbox/run.sh`.
- Every task rebuilds the binary and the image (cached, cheap) because the sandbox runner does so.
- Multi-turn and `long` tasks need `--continue`. `/compact` is a UI command, so context/long tasks rely on auto-compaction.
- A denial scenario needs `SWARG_ASK=1` and an interactive answer; not covered (plain mode stdin is the prompt).
- Language/robust checks are keyword heuristics; read the transcript of a failing task before blaming the model.
- Results vary run to run (sampling); compare models on several runs.

## Fake web (web-* tasks)

The sandbox has no internet, so `web-*` tasks run against a deterministic fake web in
`web/`: `server.py` (Python stdlib) serves `site/<host>/<path>` by the Host header and a
SearXNG-compatible `http://search.test/search?q=..&format=json` (pages ranked by how many query
words they contain, at least half; a page with `noindex` in its first 400 bytes is reachable only
through links). The sites are fictional (Zentry, Lumora Labs, OrbitKV) so the model cannot answer
from memory.

Wiring: `run.sh` exports `SWARG_WEB_DIR=web` for `web-*` and `combo-*` tasks; `sandbox/run.sh` then starts one
more container `swarg-e2e-web` (same image, read-only, no capabilities) on the internal network
with the aliases `search.test` and every folder name under `site/`. `eval.py prep` merges
`web/config.json` (`searx_url`, `allow_private`, `confirm: false`) over the task config (the
`SWARG_EVAL_CONFIG` file or the baked auto-approve one), then the task's `config.json` over that.
A new site = a new folder under `site/`. A `check.sh` may read the raw trace
(`.eval/traces/*.jsonl`, one JSON record per line, `"kind":"tool_start"` with `d.name`) to assert
that a tool was or was not called.
Test the server alone: `python3 web/server.py web/site 18931` and
`curl -H 'Host: search.test' 'http://127.0.0.1:18931/search?q=zentry&format=json'`.
