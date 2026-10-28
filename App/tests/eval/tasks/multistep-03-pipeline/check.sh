#!/bin/sh
out=$(python3 main.py 2>&1)
[ "$out" = "a=1
b=2
c=3" ] || { echo "FAIL: main.py printed: $out" | head -c 200; exit 1; }
[ "$(cat data.csv)" = "a,1
b,2
c,3" ] || { echo "FAIL: data.csv was changed"; exit 1; }
echo "PASS: pipeline prints a=1 b=2 c=3"
