#!/bin/sh
grep -q 'mcp: server "orders" failed to start: .*needs authentication' .eval/output.txt || { echo "FAIL: no auth warning for the server"; exit 1; }
[ -f answer.txt ] || { echo "FAIL: answer.txt missing"; exit 1; }
grep -qi 'packed\|courier' answer.txt || { echo "FAIL: answer.txt: $(head -c 200 answer.txt)"; exit 1; }
echo "PASS: warned about the auth, answered from the file"
