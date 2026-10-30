#!/bin/sh
F=CHECKLIST.md
[ -f "$F" ] || { echo "FAIL: $F missing"; exit 1; }
n=$(grep -c '^- \[ \]' "$F")
[ "$n" = 5 ] || { echo "FAIL: $n checklist lines, want 5"; exit 1; }
grep -q 'tag created' "$F" && grep -q 'release published' "$F" || { echo "FAIL: items missing"; exit 1; }
echo "PASS: CHECKLIST.md with the five items"
