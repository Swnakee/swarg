#!/bin/sh
[ -s summary.txt ] || { echo "FAIL: summary.txt missing or empty"; exit 1; }
total=$(python3 report.py | awk '{s+=$2} END{print s}')
grep -qw "$total" summary.txt || { echo "FAIL: total $total not in summary.txt: $(head -c 150 summary.txt)"; exit 1; }
echo "PASS: summary states total $total"
