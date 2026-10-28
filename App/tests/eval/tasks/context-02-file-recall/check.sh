#!/bin/sh
grep -q "MARIGOLD-31" answer.txt 2>/dev/null || { echo "FAIL: answer.txt: '$(head -c 80 answer.txt 2>/dev/null)'"; exit 1; }
[ "$(tail -n1 a.py)" = "# checked" ] && [ "$(tail -n1 b.py)" = "# checked" ] || { echo "FAIL: a.py/b.py edits missing"; exit 1; }
echo "PASS: codeword recalled, edits present"
