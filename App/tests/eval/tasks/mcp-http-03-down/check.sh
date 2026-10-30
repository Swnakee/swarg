#!/bin/sh
grep -q 'mcp: server "orders" failed to start: .*cannot reach' .eval/output.txt || { echo "FAIL: no start warning for the unreachable server"; exit 1; }
[ -f answer.txt ] || { echo "FAIL: answer.txt missing"; exit 1; }
grep -qi 'packed\|courier' answer.txt || { echo "FAIL: answer.txt: $(head -c 200 answer.txt)"; exit 1; }
echo "PASS: warned about the unreachable server, answered from the file"
