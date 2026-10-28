#!/bin/sh
rm -f version.txt
out=$(sh build.sh 2>&1) || { echo "FAIL: build.sh still fails: $(echo "$out" | tail -n1)"; exit 1; }
[ "$(cat version.txt)" = "1.4.2" ] || { echo "FAIL: version.txt is '$(cat version.txt)'"; exit 1; }
[ "$(python3 gen_version.py)" = "1.4.2" ] || { echo "FAIL: gen_version.py output wrong"; exit 1; }
echo "PASS: build works, version 1.4.2"
