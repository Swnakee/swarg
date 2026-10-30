#!/bin/sh
A=$(tail -n 40 .eval/output.txt)
echo "$A" | grep -qE '(^|[^0-9])17([^0-9]|$)' || { echo "FAIL: default 17 not in the answer"; exit 1; }
echo "PASS: 17"
