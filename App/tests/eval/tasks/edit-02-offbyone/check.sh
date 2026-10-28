#!/bin/sh
python3 -c "
from sumrange import sum_to
for n, e in [(0,0),(1,1),(2,3),(5,15),(10,55),(100,5050)]:
    assert sum_to(n) == e, (n, sum_to(n))
" 2>&1 || { echo "FAIL: sum_to still wrong"; exit 1; }
echo "PASS: sum_to correct"
