#!/bin/sh
grep -q "ZEBRA_7" answer.txt 2>/dev/null || { echo "FAIL: answer.txt lacks ZEBRA_7: '$(head -c 80 answer.txt 2>/dev/null)'"; exit 1; }
cmp -s data1.txt copy.txt || { echo "FAIL: copy.txt differs from data1.txt (turn 3 lost)"; exit 1; }
echo "PASS: fact recalled, middle turns done"
