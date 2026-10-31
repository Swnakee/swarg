#!/usr/bin/env bash
# Self-test of the sandbox egress (needs docker/colima and internet; no model, no real key).
# Runs run.sh with SWARG_EGRESS=openrouter.ai SWARG_NO_LOCAL=1 and checks from inside the run container:
#   1. HTTPS to openrouter.ai through the proxy works (public /models, no key)
#   2. HTTPS to example.com is refused by the proxy
#   3. a direct connection (proxy bypassed) is impossible
#   4. plain http through the proxy is refused
#   5. the fake key is in the container env, and absent from every result file (scrubbed)
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
SANDBOX="$(cd "$HERE/.." && pwd)"
FAKE="sk-selftest-0123456789abcdef"
export SWARG_EGRESS=openrouter.ai SWARG_NO_LOCAL=1 SWARG_KEY_ENV=SELFTEST_KEY SELFTEST_KEY="$FAKE"

bash "$SANDBOX/run.sh" hello -- bash -c '
echo "T1 $(curl -s -m 20 -o /dev/null -w "%{http_code}" https://openrouter.ai/api/v1/models)"
echo "T2 $(curl -s -m 20 -o /dev/null -w "%{http_code}" https://example.com/ 2>&1; echo " rc=$?")"
echo "T3 $(env -u HTTPS_PROXY -u https_proxy curl -s -m 8 -o /dev/null -w "%{http_code}" https://openrouter.ai/ 2>&1; echo " rc=$?")"
echo "T4 $(curl -s -m 20 -o /dev/null -w "%{http_code}" http://example.com/ 2>&1; echo " rc=$?")"
echo "T5 key length ${#SELFTEST_KEY} value $SELFTEST_KEY"
' | tee /tmp/swarg-egress-selftest.out
OUT="$(ls -dt "$(cd "$SANDBOX/../.." && pwd)"/../Export/e2e/*-hello 2>/dev/null | head -1)"
fail=0
chk() { if eval "$2"; then echo "PASS $1"; else echo "FAIL $1"; fail=1; fi; }
chk "T1 openrouter.ai reachable"   "grep -q '^T1 200' /tmp/swarg-egress-selftest.out"
chk "T2 example.com refused"       "! grep -q '^T2 200' /tmp/swarg-egress-selftest.out && grep -q 'DENY example.com:443' '$OUT/egress.log'"
chk "T3 direct connection fails"   "grep -q '^T3 000' /tmp/swarg-egress-selftest.out"
chk "T4 plain http refused"        "! grep -q '^T4 200' /tmp/swarg-egress-selftest.out"
chk "T5 key reached the container" "grep -q 'T5 key length ${#FAKE}' /tmp/swarg-egress-selftest.out"
chk "key not in results"           "! grep -rq '$FAKE' '$OUT'"
rm -f /tmp/swarg-egress-selftest.out
docker ps -a --format '{{.Names}}' | grep -q '^swarg-e2e-' && { echo "FAIL leftover containers"; fail=1; }
exit $fail
