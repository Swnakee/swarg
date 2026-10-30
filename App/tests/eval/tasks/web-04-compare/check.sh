#!/bin/sh
A=$(tail -n 40 .eval/output.txt)
echo "$A" | grep -q 7342 || { echo "FAIL: official port 7342 missing"; exit 1; }
echo "$A" | grep -q 6913 || { echo "FAIL: forum port 6913 missing"; exit 1; }
echo "PASS: both sources reported"
