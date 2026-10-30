#!/bin/sh
n=$(cat tools/*.py | grep -c "^def parse_kv")
[ "$n" = 1 ] || { echo "FAIL: parse_kv is defined $n times, want 1"; exit 1; }
grep -q "^def parse_kv" tools/common.py || { echo "FAIL: parse_kv is not in tools/common.py"; exit 1; }
for s in a b c; do
  grep -q "parse_kv" tools/$s.py || { echo "FAIL: tools/$s.py no longer uses parse_kv"; exit 1; }
  grep -q "common" tools/$s.py || { echo "FAIL: tools/$s.py does not import from common"; exit 1; }
done
[ "$(python3 tools/a.py 2>&1)" = "db1 5432" ] || { echo "FAIL: a.py output changed"; exit 1; }
[ "$(python3 tools/b.py 2>&1)" = "DEBUG 2" ] || { echo "FAIL: b.py output changed"; exit 1; }
[ "$(python3 tools/c.py 2>&1)" = "6" ] || { echo "FAIL: c.py output changed"; exit 1; }
echo "PASS: one parse_kv in common.py, three importers, same output"
