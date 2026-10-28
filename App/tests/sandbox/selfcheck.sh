#!/usr/bin/env bash
# Stand-in for swarg until the agent exists: proves the sandbox works.
# Checks: model reachable, internet blocked, host unreachable, /work writable, swarg runs,
# swarg saves a Session (the config dir must be writable: the root filesystem is not).
set -u
fail=0
check() { if eval "$2"; then echo "ok   $1"; else echo "FAIL $1"; fail=1; fi; }

body=$(printf '{"model":"%s","messages":[{"role":"user","content":"hi"}],"max_tokens":1}' "$SWARG_MODEL")
check "model answers a 1-token request" \
  '[ "$(curl -s -m 120 -o /dev/null -w "%{http_code}" -H "Content-Type: application/json" -d "$body" "$SWARG_BASE_URL/chat/completions")" = 200 ]'
check "internet is blocked"       '! curl -s -m 5 -o /dev/null https://example.com/'
check "host is not reachable"     '! curl -s -m 5 -o /dev/null http://host.lima.internal:8080/'
check "/work is writable"         'echo "written by selfcheck" > /work/selfcheck.txt'
check "root filesystem read-only" '! touch /usr/selfcheck 2>/dev/null'
check "swarg binary runs"         'swarg >/dev/null'
check "swarg saves a session" \
  'echo "say ok" | swarg --base-url "$SWARG_BASE_URL" --model "$SWARG_MODEL" >/dev/null 2>/tmp/err && ! grep -q "not saved" /tmp/err && swarg --sessions | grep -q "say ok"'
exit $fail
