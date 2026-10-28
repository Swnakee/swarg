#!/bin/sh
tail -n 40 .eval/output.txt | grep -qiE "ZeroDivision|division by zero|divide by zero|деление на ноль|на ноль" \
    || { echo "FAIL: the answer does not name the division by zero error"; exit 1; }
echo "PASS: failure reported"
