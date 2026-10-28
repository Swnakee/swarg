#!/bin/sh
if grep -q calc_total *.py; then echo "FAIL: calc_total still present"; exit 1; fi
grep -q "def compute_sum" util.py || { echo "FAIL: compute_sum not defined in util.py"; exit 1; }
out=$(python3 app.py 2>&1)
[ "$out" = "total: 32" ] || { echo "FAIL: app.py printed '$out'"; exit 1; }
python3 -c "import report; assert report.summary([(2,3)])=='items=1 total=6'" 2>&1 || { echo "FAIL: report.py broken"; exit 1; }
echo "PASS: renamed everywhere, app and report work"
