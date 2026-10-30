#!/bin/sh
python3 tests.py > /tmp/t.out 2>&1 || { echo "FAIL: tests.py still fails: $(tail -n1 /tmp/t.out)"; exit 1; }
grep -q "test_checkout_coupon_before_tax" tests.py || { echo "FAIL: tests.py was changed"; exit 1; }
python3 -c "
from shop.cart import Cart; from shop.checkout import checkout
c = Cart(); c.add('a', 100, 1); assert checkout(c, 10, 0.2) == 108.0
c = Cart(); c.add('a', 20, 2); c.add('b', 20, 1); assert checkout(c, 15, 0.1) == 49.5
" 2>&1 || { echo "FAIL: checkout still applies the coupon after tax"; exit 1; }
echo "PASS: suite green, coupon before tax"
