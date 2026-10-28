#!/bin/sh
[ "$(cat config.ini 2>/dev/null)" = "mode=fast" ] || { echo "FAIL: config.ini wrong or missing"; exit 1; }
rm -f out.txt
python3 gen.py >/dev/null 2>&1 || { echo "FAIL: gen.py fails"; exit 1; }
[ "$(tail -n1 STEPS.log 2>/dev/null)" = "done" ] || { echo "FAIL: STEPS.log last line is not 'done'"; exit 1; }
echo "PASS: all 3 steps done"
