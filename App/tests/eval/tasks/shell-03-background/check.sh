#!/bin/sh
[ "$(tr -d ' \n\r' < answer.txt 2>/dev/null)" = "546" ] || { echo "FAIL: answer.txt is '$(head -c 80 answer.txt 2>/dev/null)', expected 546"; exit 1; }
echo "PASS: answer 546"
