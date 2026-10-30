#!/bin/sh
if grep -q '"name":"mcp__' .eval/traces/*.jsonl 2>/dev/null; then echo "FAIL: a tool of the disabled server was called"; exit 1; fi
[ -f answer.txt ] || { echo "FAIL: answer.txt missing"; exit 1; }
grep -qi 'packed\|courier' answer.txt || { echo "FAIL: answer.txt: $(head -c 200 answer.txt)"; exit 1; }
echo "PASS: answered from the file, no MCP call"
