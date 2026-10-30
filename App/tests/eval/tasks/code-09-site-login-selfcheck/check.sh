#!/bin/sh
for f in site/index.html site/login.html site/style.css; do
  [ -f "$f" ] || { echo "FAIL: $f missing$( [ -f "$(basename $f)" ] && echo " (written outside site/ as $(basename $f))")"; exit 1; }
done
grep -qi 'type="password"' site/login.html || { echo "FAIL: no password field"; exit 1; }
grep -qi 'name="user\|id="user\|username' site/login.html || { echo "FAIL: no username field"; exit 1; }
grep -qi 'style.css' site/index.html && grep -qi 'style.css' site/login.html || { echo "FAIL: a page does not link style.css"; exit 1; }
grep -qi 'login.html' site/index.html || { echo "FAIL: index does not link login.html"; exit 1; }
grep -qi '<script' site/login.html || { echo "FAIL: login.html has no inline script"; exit 1; }
grep -qi 'secret' site/login.html && grep -qi 'Welcome' site/login.html || { echo "FAIL: the script does not check admin/secret -> Welcome"; exit 1; }
[ -f tests/check_site.py ] || { echo "FAIL: tests/check_site.py missing"; exit 1; }
python3 tests/check_site.py >/tmp/t.out 2>&1 || { echo "FAIL: own check fails: $(tail -n1 /tmp/t.out)"; exit 1; }
echo "PASS: site/ complete, login script present, own check passes"
