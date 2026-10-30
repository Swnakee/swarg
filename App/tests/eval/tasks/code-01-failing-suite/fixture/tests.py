"""Plain test runner: python3 tests.py -> exit 0 when every test passes."""
import sys

from shop.cart import Cart
from shop.checkout import checkout
from shop.discounts import fixed_off
from shop.pricing import apply_tax


def cart(*items):
    c = Cart()
    for name, price, qty in items:
        c.add(name, price, qty)
    return c


def test_subtotal():
    assert cart(("a", 10, 2), ("b", 5, 1)).subtotal() == 25


def test_tax():
    assert apply_tax(100, 0.2) == 120.0


def test_fixed_off_floor():
    assert fixed_off(5, 10) == 0.0


def test_checkout_no_coupon():
    assert checkout(cart(("a", 100, 1)), 0, 0.2) == 120.0


def test_checkout_coupon_before_tax():
    # 100 - 10 coupon = 90, then 20% tax = 108
    assert checkout(cart(("a", 100, 1)), 10, 0.2) == 108.0


def test_checkout_coupon_two_items():
    # 40 + 20 = 60, minus 15 = 45, plus 10% = 49.5
    assert checkout(cart(("a", 20, 2), ("b", 20, 1)), 15, 0.1) == 49.5


if __name__ == "__main__":
    failed = 0
    for name, fn in sorted(globals().items()):
        if name.startswith("test_") and callable(fn):
            try:
                fn()
                print("ok  ", name)
            except AssertionError:
                failed += 1
                print("FAIL", name)
    print("%d failed" % failed)
    sys.exit(1 if failed else 0)
