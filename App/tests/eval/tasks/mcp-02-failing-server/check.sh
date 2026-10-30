#!/bin/sh
grep -q 'mcp: server "demo" failed to start' .eval/output.txt || { echo "FAIL: no start warning for the failing server"; exit 1; }
[ -f answer.txt ] || { echo "FAIL: answer.txt missing"; exit 1; }
grep -qi 'packed\|courier' answer.txt || { echo "FAIL: answer.txt: $(head -c 200 answer.txt)"; exit 1; }
echo "PASS: warned about the server, answered from the file"
