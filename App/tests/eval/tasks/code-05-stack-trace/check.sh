#!/bin/sh
out=$(python3 -m app 2>&1) || { echo "FAIL: still crashes: $(echo "$out" | tail -n1)"; exit 1; }
[ "$out" = "prod-report: timeout=30 retries=5" ] || { echo "FAIL: output '$out'"; exit 1; }
grep -q "except" app/service.py app/__main__.py app/client.py && { echo "FAIL: a try/except hides the cause"; exit 1; }
grep -q '"timeout"' settings.json && { echo "FAIL: the setting was added to settings.json instead of a default"; exit 1; }
grep -q "timeout" app/config.py || { echo "FAIL: no default for timeout in config.py"; exit 1; }
echo "PASS: timeout defaults to 30 in the config loader"
