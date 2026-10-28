#!/bin/sh
python3 -c "
from mathx import clamp
from textx import word_count
assert clamp(5,0,10)==5 and clamp(-3,0,10)==0 and clamp(99,0,10)==10 and clamp(0,0,0)==0
assert word_count('a b  c')==3 and word_count('')==0 and word_count('  one ')==1 and word_count('x\ty\nz')==3
" 2>&1 || { echo "FAIL: functions wrong or not implemented"; exit 1; }
echo "PASS: both functions implemented"
