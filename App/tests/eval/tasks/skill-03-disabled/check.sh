#!/bin/sh
# BusyBox grep (Alpine) has no --include: filter the file list instead.
# A call of the skill tool that swarg refused is fine (the model may find the folder and try);
# only a skill call that succeeded means the disabled skill was applied.
if grep -h '"kind":"tool_end".*"name":"skill"' .eval/traces/*.jsonl 2>/dev/null | grep -qv '"error":'; then echo "FAIL: the disabled skill was applied"; exit 1; fi
f=$(grep -rl '1\.4\.0' . 2>/dev/null | grep '\.md$' | grep -v '^\./\.swarg/' | head -n 1)
[ -n "$f" ] || { echo "FAIL: no markdown file mentions 1.4.0"; exit 1; }
echo "PASS: notes written in $f without the skill"
