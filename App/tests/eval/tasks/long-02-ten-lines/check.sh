#!/bin/sh
exp=$(cat <<'EOT'
01 red
02 green
03 blue
04 cyan
05 magenta
06 yellow
07 black
08 white
09 orange
10 purple
EOT
)
got=$(grep -E '^[0-9][0-9] ' out.txt 2>/dev/null)
[ "$got" = "$exp" ] || { echo "FAIL: out.txt lines wrong; got $(echo "$got" | wc -l) matching lines"; exit 1; }
echo "PASS: all 10 lines present in order"
