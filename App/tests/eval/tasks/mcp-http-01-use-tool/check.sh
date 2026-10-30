#!/bin/sh
[ -f answer.txt ] || { echo "FAIL: answer.txt missing"; exit 1; }
grep -qi 'cancel' answer.txt || { echo "FAIL: answer.txt: $(head -c 200 answer.txt)"; exit 1; }
grep -q '"name":"mcp__orders__lookup"' .eval/traces/*.jsonl 2>/dev/null || { echo "FAIL: the MCP lookup tool was not called"; exit 1; }
echo "PASS: status from the HTTP MCP tool"
