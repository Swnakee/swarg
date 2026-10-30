#!/bin/sh
rm -f inventory.json
python3 -m inv add apple 3 >/dev/null 2>&1 || { echo "FAIL: add broken"; exit 1; }
python3 -m inv add pear 1 >/dev/null 2>&1
out=$(python3 -m inv remove apple 2>&1); rc=$?
[ "$rc" = 0 ] && echo "$out" | grep -q "removed apple" || { echo "FAIL: remove apple -> rc=$rc '$(echo "$out" | head -n1)'"; exit 1; }
python3 -m inv list | grep -q "^apple" && { echo "FAIL: apple still listed"; exit 1; }
python3 -m inv list | grep -q "^pear 1" || { echo "FAIL: pear lost after remove"; exit 1; }
out=$(python3 -m inv remove kiwi 2>&1); rc=$?
[ "$rc" = 1 ] && echo "$out" | grep -q "not found: kiwi" || { echo "FAIL: missing item -> rc=$rc '$out'"; exit 1; }
python3 -m inv --help | grep -q "remove" || { echo "FAIL: help does not mention remove"; exit 1; }
grep -qi "remove" README.md || { echo "FAIL: README not updated"; exit 1; }
echo "PASS: remove works across model, storage, cli, docs"
