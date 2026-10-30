#!/bin/sh
[ -f tools/wordcount_tool.py ] || { echo "FAIL: tools/wordcount_tool.py missing"; exit 1; }
printf '%s\n' '{"id": 1, "name": "word_count", "args": {"text": "a bb ccc"}}' 'not json' '{"id": 2, "name": "nope", "args": {}}' '{"id": 3, "name": "word_count", "args": {"text": ""}}' \
  | python3 tools/wordcount_tool.py > /tmp/t.out 2>/tmp/t.err; rc=$?
[ "$rc" = 0 ] || { echo "FAIL: exit $rc: $(tail -n1 /tmp/t.err)"; exit 1; }
python3 - <<'PY' /tmp/t.out || exit 1
import json, sys
lines = [l for l in open(sys.argv[1]).read().splitlines() if l.strip()]
if len(lines) != 4:
    print("FAIL: %d output lines for 4 requests" % len(lines)); sys.exit(1)
try:
    r = [json.loads(l) for l in lines]
except ValueError as e:
    print("FAIL: output is not JSON lines: %s" % e); sys.exit(1)
if r[0].get("id") != 1 or r[0].get("result") != {"words": 3, "chars": 8}:
    print("FAIL: word_count result %r" % (r[0],)); sys.exit(1)
if "error" not in r[1] or r[1].get("error") != "bad request":
    print("FAIL: bad line response %r" % (r[1],)); sys.exit(1)
if r[2].get("id") != 2 or r[2].get("error") != "unknown tool: nope":
    print("FAIL: unknown tool response %r" % (r[2],)); sys.exit(1)
if r[3].get("id") != 3 or r[3].get("result") != {"words": 0, "chars": 0}:
    print("FAIL: empty text result %r" % (r[3],)); sys.exit(1)
PY
echo "PASS: protocol followed for ok, bad line, unknown tool, empty text"
