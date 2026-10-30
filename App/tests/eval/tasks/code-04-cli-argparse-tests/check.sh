#!/bin/sh
[ -f wc.py ] || { echo "FAIL: no wc.py"; exit 1; }
[ -f test_wc.py ] || { echo "FAIL: no test_wc.py"; exit 1; }
python3 wc.py -l a.txt 2>&1 | grep -qw 4 || { echo "FAIL: -l a.txt should give 4: $(python3 wc.py -l a.txt 2>&1)"; exit 1; }
python3 wc.py -w a.txt 2>&1 | grep -qw 6 || { echo "FAIL: -w a.txt should give 6: $(python3 wc.py -w a.txt 2>&1)"; exit 1; }
python3 wc.py -l a.txt b.txt 2>&1 | grep -qw 2 || { echo "FAIL: two files: b.txt should give 2"; exit 1; }
cat b.txt | python3 wc.py -w - 2>&1 | grep -qw 3 || { echo "FAIL: stdin -w should give 3: $(cat b.txt | python3 wc.py -w - 2>&1)"; exit 1; }
python3 test_wc.py >/tmp/t.out 2>&1 || { echo "FAIL: test_wc.py fails: $(tail -n2 /tmp/t.out)"; exit 1; }
grep -q "stdin\|'-'\|\"-\"" test_wc.py || { echo "FAIL: test_wc.py has no stdin test"; exit 1; }
echo "PASS: wc.py counts lines, words, stdin; own tests pass"
