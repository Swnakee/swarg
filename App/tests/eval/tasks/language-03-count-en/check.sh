#!/bin/sh
[ "$(tr -d ' \n\r' < count.txt 2>/dev/null)" = "7" ] || { echo "FAIL: count.txt is '$(head -c 60 count.txt 2>/dev/null)', expected 7"; exit 1; }
echo "PASS: count 7"
