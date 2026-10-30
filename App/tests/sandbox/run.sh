#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# swarg e2e runner: one isolated, disposable container per run.
#
# Usage: run.sh [-i] [fixture] [-- command ...]
#   -i       interactive: allocate a terminal (TTY) for a live session, e.g.
#            run.sh -i hello -- swarg   (used for live UI tests in a second terminal tab)
#   fixture  folder under fixtures/ copied into /work (default: hello)
#   command  what to run inside the container (default: selfcheck.sh,
#            the stand-in until the agent exists)
#
# Isolation: internal Docker network (no internet), the host model server is
# reachable only through a proxy container under the name "model", non-root,
# read-only root filesystem, CPU/memory/pid limits, no host directories mounted.
#
# Model server rules (mlx_lm.server): single lane, so runs are sequential (lock);
# never kill a client mid-request, so the runner waits for the container to
# finish instead of killing it; health check with a 1-token POST first.
#
# Exit codes: 0 pass, 1 run failed, 2 usage, 3 model server down or wedged,
# 4 container runtime not available, 5 another run in progress.
# ---------------------------------------------------------------------------
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
APP="$(cd "$HERE/../.." && pwd)"
ROOT="$(cd "$APP/.." && pwd)"

MODEL_HOST_URL="${SWARG_MODEL_HOST_URL:-http://127.0.0.1:8080}"
MODEL_PORT="${MODEL_HOST_URL##*:}"
# The proxy reaches a model on this Mac as host.lima.internal, any other host as is
# (e.g. an Ollama server on another machine in the LAN: SWARG_MODEL_HOST_URL=http://<ip>:11434).
MODEL_HOST="${MODEL_HOST_URL#*://}"; MODEL_HOST="${MODEL_HOST%:*}"
case "$MODEL_HOST" in 127.0.0.1|localhost) MODEL_HOST="host.lima.internal" ;; esac
RUN_TIMEOUT="${SWARG_RUN_TIMEOUT:-1800}"    # seconds; generous on purpose
CPUS="${SWARG_CPUS:-1}"
MEMORY="${SWARG_MEMORY:-1g}"

NET="swarg-e2e-net"
PROXY="swarg-e2e-proxy"
WEB="swarg-e2e-web"
WEB_DIR="${SWARG_WEB_DIR:-}"   # fake web for web-* evals: a dir with server.py and site/<host>/
MCP="swarg-e2e-mcp"
MCP_DIR="${SWARG_MCP_DIR:-}"   # fake HTTP MCP server for mcp-http-* evals: a dir with mcp_http.py and mcp_demo.py
MCP_TOKEN="t9-eval-token"      # the bearer the fake needs; the agent gets it as MCP_TOKEN (never in a config file)
IMAGE="swarg-e2e:latest"
LOCK="${TMPDIR:-/tmp}/swarg-e2e.lock"

TTY=()
if [ "${1:-}" = "-i" ]; then TTY=(-it); shift; fi
FIXTURE="hello"
if [ $# -gt 0 ] && [ "$1" != "--" ]; then FIXTURE="$1"; shift; fi
if [ $# -gt 0 ] && [ "$1" = "--" ]; then shift; fi
if [ $# -gt 0 ]; then CMD=("$@"); else CMD=(selfcheck.sh); fi

FIXTURE_DIR="$HERE/fixtures/$FIXTURE"
[ -d "$FIXTURE_DIR" ] || { echo "no fixture: $FIXTURE_DIR" >&2; exit 2; }

say() { printf '[e2e] %s\n' "$*"; }

# ---------------------------------------------------------------------------
# Lock: one run at a time (the model server is a single lane)
# ---------------------------------------------------------------------------

if ! mkdir "$LOCK" 2>/dev/null; then
    say "another e2e run is in progress ($LOCK); runs are sequential"
    exit 5
fi

CID=""
BUILD=""
cleanup() {
    if [ -n "$CID" ]; then docker rm -f -v "$CID" >/dev/null 2>&1; fi
    docker rm -f "$PROXY" >/dev/null 2>&1
    [ -n "$WEB_DIR" ] && docker rm -f -v "$WEB" >/dev/null 2>&1
    [ -n "$MCP_DIR" ] && docker rm -f -v "$MCP" >/dev/null 2>&1
    docker network rm "$NET" >/dev/null 2>&1
    [ -n "$BUILD" ] && rm -rf "$BUILD"
    rmdir "$LOCK" 2>/dev/null
}
trap cleanup EXIT

# Ctrl+C must not cut a request in half: wait for the container to finish.
on_interrupt() {
    if [ -n "$CID" ]; then
        say "interrupt: waiting for the current run to finish (never kill mid-request)..."
        docker wait "$CID" >/dev/null 2>&1
    fi
    exit 1
}
trap on_interrupt INT TERM

# ---------------------------------------------------------------------------
# Preconditions: container runtime and a healthy model server
# ---------------------------------------------------------------------------

if ! docker info >/dev/null 2>&1; then
    say "docker is not reachable; start it with: colima start"
    exit 4
fi

if ! curl -s -m 5 -o /dev/null "$MODEL_HOST_URL/v1/models"; then
    say "model server is down (nothing on $MODEL_HOST_URL); start it: llm start qwen"
    exit 3
fi
# Test runs use the lightest local model that calls tools (Qwen3.5-2B; 0.8B does not). mlx_lm.server loads the
# model named in each request, so no restart is needed. Override with SWARG_MODEL.
MODEL_ID="${SWARG_MODEL:-mlx-community/Qwen3.5-2B-4bit}"
BODY=$(printf '{"model":"%s","messages":[{"role":"user","content":"hi"}],"max_tokens":1}' "$MODEL_ID")
CODE=$(curl -s -m 120 -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
    -d "$BODY" "$MODEL_HOST_URL/v1/chat/completions")
if [ "$CODE" != "200" ]; then
    say "model server answers /v1/models but a 1-token request returned http=$CODE"
    say "it is probably wedged; recover with: llm stop && llm start qwen"
    exit 3
fi
say "model ok: $MODEL_ID"

# ---------------------------------------------------------------------------
# Build: linux binary + image
# ---------------------------------------------------------------------------

BUILD="$(mktemp -d)"
ARCH="$(docker info --format '{{.Architecture}}')"
case "$ARCH" in aarch64|arm64) GOARCH=arm64 ;; x86_64|amd64) GOARCH=amd64 ;; *) GOARCH="$ARCH" ;; esac
( cd "$APP" && CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -o "$BUILD/swarg" ./cmd/swarg ) || exit 1
cp "$HERE/Dockerfile" "$HERE/selfcheck.sh" "$HERE/entry.sh" "$BUILD/"
docker build -q -t "$IMAGE" "$BUILD" >/dev/null || { say "image build failed"; exit 1; }

# ---------------------------------------------------------------------------
# Network: internal (no internet) + proxy to the host model server
# ---------------------------------------------------------------------------

docker rm -f "$PROXY" >/dev/null 2>&1
docker network rm "$NET" >/dev/null 2>&1
docker network create --internal "$NET" >/dev/null || exit 1
docker run -d --name "$PROXY" --network bridge --memory 64m --cpus 0.25 \
    alpine/socat "TCP-LISTEN:8080,fork,reuseaddr" "TCP:$MODEL_HOST:$MODEL_PORT" >/dev/null || exit 1
docker network connect --alias model "$NET" "$PROXY" || exit 1

# Fake web (SWARG_WEB_DIR): one more container on the internal network, answering for
# search.test and every host folder under site/; no internet involved.
if [ -n "$WEB_DIR" ]; then
    [ -f "$WEB_DIR/server.py" ] && [ -d "$WEB_DIR/site" ] || { say "SWARG_WEB_DIR has no server.py and site/: $WEB_DIR"; exit 2; }
    ALIASES=(--network-alias search.test)
    for h in "$WEB_DIR"/site/*/; do ALIASES+=(--network-alias "$(basename "$h")"); done
    docker rm -f -v "$WEB" >/dev/null 2>&1
    docker create --name "$WEB" --network "$NET" "${ALIASES[@]}" --cpus 0.25 --memory 64m --pids-limit 64 \
        --read-only --cap-drop ALL --security-opt no-new-privileges -v /srv \
        "$IMAGE" python3 /srv/server.py /srv/site 80 >/dev/null || exit 1
    docker cp "$WEB_DIR/server.py" "$WEB:/srv/" >/dev/null && docker cp "$WEB_DIR/site" "$WEB:/srv/" >/dev/null || exit 1
    docker start "$WEB" >/dev/null || exit 1
fi

# Fake HTTP MCP server (SWARG_MCP_DIR): one more container on the internal network, reached as
# http://mcp.test/mcp (a bearer token is required: the agent gets it through MCP_TOKEN).
if [ -n "$MCP_DIR" ]; then
    [ -f "$MCP_DIR/mcp_http.py" ] && [ -f "$MCP_DIR/mcp_demo.py" ] || { say "SWARG_MCP_DIR has no mcp_http.py and mcp_demo.py: $MCP_DIR"; exit 2; }
    docker rm -f -v "$MCP" >/dev/null 2>&1
    docker create --name "$MCP" --network "$NET" --network-alias mcp.test --cpus 0.25 --memory 64m --pids-limit 64 \
        --read-only --cap-drop ALL --security-opt no-new-privileges -v /srv \
        "$IMAGE" python3 /srv/mcp_http.py 80 --token "$MCP_TOKEN" >/dev/null || exit 1
    docker cp "$MCP_DIR/mcp_http.py" "$MCP:/srv/" >/dev/null && docker cp "$MCP_DIR/mcp_demo.py" "$MCP:/srv/" >/dev/null || exit 1
    docker start "$MCP" >/dev/null || exit 1
fi

# ---------------------------------------------------------------------------
# Run
# ---------------------------------------------------------------------------

# Inside the sandbox every ToolCall is auto-approved by default: entry.sh copies a baked config
# with tools.auto_approve into the writable $HOME (a tmpfs, so Sessions can be saved).
# SWARG_ASK=1 keeps the confirmations (e.g. the denial e2e run).
CFG_ENV=(-e SWARG_ASK="${SWARG_ASK:-0}")
[ -n "$MCP_DIR" ] && CFG_ENV+=(-e MCP_TOKEN="$MCP_TOKEN")

# A live session gets the host terminal's colour support: without TERM/COLORTERM swarg
# falls back to 16 colours. The image has no terminfo for xterm-ghostty, so TERM is the
# generic xterm-256color; COLORTERM (truecolor in Ghostty) is passed through.
TERM_ENV=()
if [ ${#TTY[@]} -gt 0 ]; then
    TERM_ENV=(-e TERM=xterm-256color)
    [ -n "${COLORTERM:-}" ] && TERM_ENV+=(-e COLORTERM="$COLORTERM")
fi

OUT="$ROOT/Export/e2e/$(date +%Y%m%d-%H%M%S)-$FIXTURE"
mkdir -p "$OUT"

CID=$(docker create ${TTY[@]+"${TTY[@]}"} --network "$NET" \
    --cpus "$CPUS" --memory "$MEMORY" --pids-limit 256 \
    --read-only --tmpfs /tmp:rw,size=64m --tmpfs /home/agent:rw,size=64m,uid=1000,gid=1000 -v /work \
    --cap-drop ALL --security-opt no-new-privileges \
    -e SWARG_BASE_URL=http://model:8080/v1 -e SWARG_MODEL="$MODEL_ID" \
    ${CFG_ENV[@]+"${CFG_ENV[@]}"} ${TERM_ENV[@]+"${TERM_ENV[@]}"} \
    "$IMAGE" entry.sh timeout "$RUN_TIMEOUT" "${CMD[@]}") || exit 1
docker cp "$FIXTURE_DIR/." "$CID:/work/" >/dev/null || exit 1
# Prompt variant (T5): SWARG_PROMPT_DIR holds override/extra block files; entry.sh moves them
# into the config dir before swarg starts, so the model never sees them in /work.
# docker cp keeps the host owner, so a world-writable copy goes in (entry.sh must remove it).
if [ -n "${SWARG_PROMPT_DIR:-}" ]; then
    [ -d "$SWARG_PROMPT_DIR" ] || { say "SWARG_PROMPT_DIR is not a folder: $SWARG_PROMPT_DIR"; exit 2; }
    BLOCKS="$(mktemp -d)/.swarg-blocks"
    cp -R "$SWARG_PROMPT_DIR/." "$BLOCKS" && chmod -R a+rwX "$BLOCKS" || exit 1
    docker cp "$BLOCKS" "$CID:/work/" >/dev/null || exit 1
    rm -rf "$(dirname "$BLOCKS")"
fi

say "running: ${CMD[*]} (fixture $FIXTURE)"
if [ ${#TTY[@]} -gt 0 ]; then
    docker start -ai "$CID"            # live session: the terminal is the container's TTY
else
    docker start -a "$CID" 2>&1 | tee "$OUT/output.log"
fi
STATUS=$(docker wait "$CID")

# ---------------------------------------------------------------------------
# Collect: resulting /work and its diff against the fixture
# ---------------------------------------------------------------------------

docker cp "$CID:/work/." "$OUT/work/" >/dev/null
( cd "$OUT" && diff -ruN "$FIXTURE_DIR" work > diff.patch )
say "exit=$STATUS, results: $OUT"
[ "$STATUS" = "0" ] && exit 0 || exit 1
