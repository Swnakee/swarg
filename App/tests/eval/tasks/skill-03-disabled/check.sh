#!/bin/sh
# BusyBox grep (Alpine) has no --include: filter the file list instead.
if grep -q '"kind":"tool_start".*"name":"skill"' .eval/traces/*.jsonl 2>/dev/null; then echo "FAIL: the disabled skill was offered or applied"; exit 1; fi
f=$(grep -rl '1\.4\.0' . 2>/dev/null | grep '\.md$' | grep -v '^\./\.swarg/' | head -n 1)
[ -n "$f" ] || { echo "FAIL: no markdown file mentions 1.4.0"; exit 1; }
echo "PASS: notes written in $f without the skill"
