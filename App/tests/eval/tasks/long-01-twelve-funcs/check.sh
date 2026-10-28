#!/bin/sh
python3 -c "
import funcs
for n in range(1, 13):
    f = getattr(funcs, 'f%d' % n)
    assert f(3) == 3 * n + 1 and f(0) == 1, n
" 2>&1 || { echo "FAIL: some of f1..f12 missing or wrong"; exit 1; }
python3 run_all.py 2>&1 | tail -n1 | grep -q "ALL OK (12 functions)" || { echo "FAIL: run_all.py does not end with ALL OK (12 functions)"; exit 1; }
echo "PASS: all 12 functions present, program runs"
