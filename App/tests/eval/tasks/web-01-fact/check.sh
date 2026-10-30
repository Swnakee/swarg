#!/bin/sh
A=$(tail -n 40 .eval/output.txt)
echo "$A" | grep -q "Ilsa Varga" || { echo "FAIL: founder (Ilsa Varga) missing"; exit 1; }
echo "$A" | grep -q 2017 || { echo "FAIL: year 2017 missing"; exit 1; }
echo "PASS: Ilsa Varga, 2017"
