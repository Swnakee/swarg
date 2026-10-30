#!/bin/sh
out=$(python3 run.py 2>&1) || { echo "FAIL: run.py crashes: $(echo "$out" | tail -n1)"; exit 1; }
echo "$out" | grep -q "Report generated 2026, revision 6" || { echo "FAIL: footer is '$(echo "$out" | tail -n1)'"; exit 1; }
echo "$out" | grep -q "20266" && { echo "FAIL: 20266 still printed"; exit 1; }
echo "$out" | grep -q "^total   4240.00 EUR" || { echo "FAIL: body changed"; exit 1; }
[ "$(md5sum < core/app.py | cut -c1-32)" = "3dad49d75f2761310587c94feb53dda0" ] || { echo "FAIL: core/app.py was changed"; exit 1; }
[ "$(md5sum < core/settings.py | cut -c1-32)" = "90c387b7c5f8ba2033e3a985c8fbd761" ] || { echo "FAIL: core/settings.py was changed"; exit 1; }
[ "$(md5sum < report/header.py | cut -c1-32)" = "bd370393711edc148959425f1ef545c8" ] || { echo "FAIL: report/header.py was changed"; exit 1; }
echo "PASS: footer fixed in report/footer.py, nothing else touched"
