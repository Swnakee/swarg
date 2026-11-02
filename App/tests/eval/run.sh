#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# swarg agent evaluation: run tasks one by one in the e2e sandbox, check, report.
#
# Usage: run.sh [-m model] [-p prompt-dir] [-l] [task-glob ...]
#   -m model   model id (default: $SWARG_MODEL or mlx-community/Qwen3.5-2B-4bit)
#   -p dir     prompt variant: block files (NN-name.md adds, <block>.md replaces, R-49) installed
#              as the blocks dir inside the sandbox (default: $SWARG_PROMPT_DIR); the run dir
#              name gets "-<dirname>"; see prompts/
#   -l         list the tasks and exit
#   task-glob  shell patterns matched against task directory names (edit-*, shell-01*);
#              no patterns = all tasks
#
# Builds on ../sandbox/run.sh (one disposable container per task, sequential).
# Result: Export/eval/<timestamp>-<model>/{report.md,results.csv,results.jsonl,runs/<task>/}
# Exit: 0 all passed, 1 some failed, 3 model down, 4 docker down, 2 usage.
# ---------------------------------------------------------------------------
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"
SANDBOX="$HERE/../sandbox"
MODEL="${SWARG_MODEL:-mlx-community/Qwen3.5-2B-4bit}"
MODEL_HOST_URL="${SWARG_MODEL_HOST_URL:-http://127.0.0.1:8080}"
LOCK_WAIT="${SWARG_EVAL_LOCK_WAIT:-1800}"   # seconds to wait for another e2e run to finish
LIST=0

while [ $# -gt 0 ]; do
    case "$1" in
        -m) MODEL="${2:?-m needs a model}"; shift 2 ;;
        -p) SWARG_PROMPT_DIR="${2:?-p needs a folder}"; shift 2 ;;
        -l) LIST=1; shift ;;
        -*) echo "unknown option $1" >&2; exit 2 ;;
        *) break ;;
    esac
done
GLOBS=("$@")

say() { printf '[eval] %s\n' "$*"; }

# ---------------------------------------------------------------------------
# Select tasks
# ---------------------------------------------------------------------------

TASKS=()
for d in "$HERE"/tasks/*/; do
    name="$(basename "$d")"
    [ -f "$d/task.md" ] && [ -f "$d/check.sh" ] || continue
    if [ ${#GLOBS[@]} -eq 0 ]; then TASKS+=("$name"); continue; fi
    for g in "${GLOBS[@]}"; do
        # shellcheck disable=SC2254
        case "$name" in $g) TASKS+=("$name"); break ;; esac
    done
done
if [ ${#TASKS[@]} -eq 0 ]; then echo "no tasks match" >&2; exit 2; fi
if [ "$LIST" = 1 ]; then
    for t in "${TASKS[@]}"; do printf '%-34s %s\n' "$t" "$(head -n1 "$HERE/tasks/$t/task.md")"; done
    exit 0
fi

# ---------------------------------------------------------------------------
# Health check: a 1-token POST (down or wedged = stop and report, no retry)
# ---------------------------------------------------------------------------

if [ "${SWARG_NO_LOCAL:-0}" = "1" ]; then
    say "cloud run (SWARG_NO_LOCAL=1): no local server check; model $MODEL; ${#TASKS[@]} task(s)"
elif ! curl -s -m 5 -o /dev/null "$MODEL_HOST_URL/v1/models"; then
    say "model server is down (nothing on $MODEL_HOST_URL); start it: llm start qwen"
    exit 3
else
BODY=$(printf '{"model":"%s","messages":[{"role":"user","content":"hi"}],"max_tokens":1}' "$MODEL")
CODE=$(curl -s -m 120 -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
    -d "$BODY" "$MODEL_HOST_URL/v1/chat/completions")
if [ "$CODE" != "200" ]; then
    say "1-token request for $MODEL returned http=$CODE: server wedged or model missing; recover with: llm stop && llm start qwen"
    exit 3
fi
say "model ok: $MODEL; ${#TASKS[@]} task(s)"
fi

# ---------------------------------------------------------------------------
# Run
# ---------------------------------------------------------------------------

SLUG="$(printf '%s' "${MODEL##*/}" | tr -c 'A-Za-z0-9._-' '_')"
if [ -n "${SWARG_PROMPT_DIR:-}" ]; then
    [ -d "$SWARG_PROMPT_DIR" ] || { echo "prompt dir not found: $SWARG_PROMPT_DIR" >&2; exit 2; }
    SWARG_PROMPT_DIR="$(cd "$SWARG_PROMPT_DIR" && pwd)"; export SWARG_PROMPT_DIR
    SLUG="$SLUG-$(basename "$SWARG_PROMPT_DIR")"
fi
RUN="$ROOT/Export/eval/$(date +%Y%m%d-%H%M%S)-$SLUG"
mkdir -p "$RUN/runs"
: > "$RUN/results.jsonl"
export SWARG_MODEL="$MODEL"

TMP="$(mktemp -d)"
LINK=""
STOP=0
cleanup() { [ -n "$LINK" ] && rm -f "$LINK"; rm -rf "$TMP"; }
trap cleanup EXIT
# Ctrl+C: finish the current task (the sandbox never kills a client mid-request), then stop.
trap 'STOP=1; say "stop requested: finishing the current task"' INT TERM

for t in "${TASKS[@]}"; do
    [ "$STOP" = 1 ] && break
    TD="$HERE/tasks/$t"
    FX="$TMP/$t"
    rm -rf "$FX"
    turns="$(python3 "$HERE/eval.py" prep "$TD" "$FX")" || { say "prep failed: $t"; continue; }

    # sandbox/run.sh takes a fixture NAME under sandbox/fixtures: link ours there for the run
    LINK="$SANDBOX/fixtures/_eval${SWARG_LANE:+-$SWARG_LANE}-$t"
    ln -sfn "$FX" "$LINK"

    say "task $t ($turns turn(s))"
    LOG="$RUN/runs/$t.log"
    waited=0
    while :; do
        case "$t" in web-*|combo-*) export SWARG_WEB_DIR="$HERE/web" ;; *) unset SWARG_WEB_DIR ;; esac
        case "$t" in mcp-http-*) export SWARG_MCP_DIR="$HERE/mcp" ;; *) unset SWARG_MCP_DIR ;; esac
        bash "$SANDBOX/run.sh" "_eval${SWARG_LANE:+-$SWARG_LANE}-$t" -- sh /work/.eval/driver.sh > "$LOG" 2>&1
        rc=$?
        [ "$rc" = 5 ] || break
        if [ "$waited" -ge "$LOCK_WAIT" ]; then say "another e2e run still holds the lock after ${LOCK_WAIT}s"; break; fi
        say "another e2e run in progress; waiting"
        sleep 20; waited=$((waited + 20))
    done
    rm -f "$LINK"; LINK=""

    if [ "$rc" = 3 ] || [ "$rc" = 4 ]; then
        say "sandbox says: $(tail -n1 "$LOG"); aborting"
        python3 "$HERE/eval.py" report "$RUN" "$MODEL"
        exit "$rc"
    fi
    OUT="$(sed -n 's/^\[e2e\] exit=.*results: //p' "$LOG" | tail -n1)"
    if [ -z "$OUT" ] || [ ! -d "$OUT" ]; then
        say "no results dir for $t (rc=$rc)"
        OUT="$RUN/runs/$t.missing"; mkdir -p "$OUT"
    else
        rm -rf "$RUN/runs/$t"; mv "$OUT" "$RUN/runs/$t"; OUT="$RUN/runs/$t"
    fi
    python3 "$HERE/eval.py" collect "$t" "$OUT" "$RUN/results.jsonl" | sed 's/^/[eval]   /'
done

python3 "$HERE/eval.py" report "$RUN" "$MODEL"
say "report: $RUN/report.md"
grep -q '"status": "PASS"' "$RUN/results.jsonl" || true
if grep -qv '"status": "PASS"' "$RUN/results.jsonl"; then exit 1; fi
exit 0
