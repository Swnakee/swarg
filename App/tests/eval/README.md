# Swarg agent evaluation suite (T2_4 W5)

A repeatable benchmark of how well the agent works with a real local model. Every task runs in
the e2e sandbox (`../sandbox/run.sh`: disposable container, no internet, model only through the
proxy), is verified by a script that runs the code, and is reported with trace statistics.

## Run

    App/tests/eval/run.sh -l                      # list tasks
    App/tests/eval/run.sh                         # all tasks, default model Qwen3.5-2B-4bit
    App/tests/eval/run.sh -m mlx-community/Qwen3.5-4B-4bit 'edit-*' 'shell-01*'

Needs: Colima/Docker running, an OpenAI-compatible model server on 127.0.0.1:8080 (the runner does a 1-token
health check and stops with exit 3 if it is down), the `swarg` build with sessions (`--continue`)
for multi-turn tasks. Tasks run sequentially (single-lane server). If another e2e run holds the
lock the runner waits (up to `SWARG_EVAL_LOCK_WAIT`, default 1800 s). Ctrl+C finishes the current
task, then stops. `SWARG_RUN_TIMEOUT` (default 1800 s) is the per-task cap of the sandbox.

Output: `Export/eval/<timestamp>-<model>/`

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

Categories: `edit`, `multistep`, `shell`, `context`, `long`, `robust`, `language`
(the prefix before the first `-` is the category).

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
