#!/bin/sh
F=NOTES/RELEASE.md
[ -f "$F" ] || { echo "FAIL: $F missing (the skill prescribes it)"; exit 1; }
[ "$(head -n 1 "$F")" = "# Release notes" ] || { echo "FAIL: first line is '$(head -n 1 "$F")'"; exit 1; }
grep -q 'Version: 1.4.0' "$F" || { echo "FAIL: no 'Version: 1.4.0'"; exit 1; }
grep -q '^## Added' "$F" && grep -q '^## Fixed' "$F" || { echo "FAIL: sections missing"; exit 1; }
grep -q '"kind":"tool_start".*"name":"skill"' .eval/traces/*.jsonl 2>/dev/null || { echo "FAIL: the skill tool was not called"; exit 1; }
echo "PASS: release notes in the skill's layout, skill applied"
