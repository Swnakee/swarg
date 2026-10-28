#!/bin/sh
sed '/^=== turn/d' .eval/output.txt | awk 'length($0) > 3' > /tmp/lines.txt
[ -s /tmp/lines.txt ] || { echo "FAIL: no output at all"; exit 1; }
rep=$(sort /tmp/lines.txt | uniq -c | sort -rn | head -n1)
n=$(echo "$rep" | awk '{print $1}')
[ "$n" -lt 4 ] || { echo "FAIL: a line repeats $n times: $(echo "$rep" | cut -c1-100)"; exit 1; }
echo "PASS: no line repeated 4+ times (max $n)"
