#!/bin/sh
F=NOTES/RELEASE.md
[ -f "$F" ] || { echo "FAIL: $F missing (the skill prescribes it)"; exit 1; }
grep -q '4\.12\.3' "$F" || { echo "FAIL: $F has no 4.12.3"; exit 1; }
[ -f RELEASE_COPY.md ] || { echo "FAIL: RELEASE_COPY.md missing"; exit 1; }
grep -q '4\.12\.3' RELEASE_COPY.md || { echo "FAIL: the copy has no 4.12.3"; exit 1; }
T=".eval/traces/*.jsonl"
grep -q '"name":"web_search"\|"name":"web_fetch"' $T 2>/dev/null || { echo "FAIL: no web tool call"; exit 1; }
grep -q '"kind":"tool_start".*"name":"skill"' $T 2>/dev/null || { echo "FAIL: the skill was not applied"; exit 1; }
grep -q '"name":"mcp__demo__save"' $T 2>/dev/null || { echo "FAIL: the MCP save tool was not called"; exit 1; }
echo "PASS: web + skill + MCP"
