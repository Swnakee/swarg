#!/bin/sh
A=$(tail -n 40 .eval/output.txt)
echo "$A" | grep -q 'zentryctl convert' || { echo "FAIL: command zentryctl convert missing"; exit 1; }
echo "PASS: command found"
