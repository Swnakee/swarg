#!/bin/sh
A=$(tail -n 40 .eval/output.txt)
echo "$A" | grep -q '4\.12\.3' || { echo "FAIL: 4.12.3 not in the answer"; exit 1; }
echo "PASS: 4.12.3"
