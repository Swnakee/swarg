#!/bin/sh
python3 -c "
import greet
assert greet.greet('Bob') == 'Hello, Bob!'
" 2>&1 || { echo "FAIL: greet() wrong or greet.py missing"; exit 1; }
[ "$(python3 greet.py 2>&1)" = "Hello, World!" ] || { echo "FAIL: script output: $(python3 greet.py 2>&1 | head -c 100)"; exit 1; }
echo "PASS: greet.py works"
