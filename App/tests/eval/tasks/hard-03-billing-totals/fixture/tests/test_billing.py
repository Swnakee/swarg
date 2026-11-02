import os
import sys
import unittest
from decimal import Decimal as D

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from billing.invoice import build_invoice  # noqa: E402
from billing.models import Line, Order  # noqa: E402


class Basic(unittest.TestCase):
    def test_simple(self):
        o = Order("o1", "c1", "Acme", "TX", False, [Line("a", 2, D("10.00"))])
        inv = build_invoice(o)
        self.assertEqual(inv.subtotal, D("20.00"))
        self.assertEqual(inv.discount, D("0.00"))
        self.assertEqual(inv.tax, D("1.25"))
        self.assertEqual(inv.shipping, D("7.50"))
        self.assertEqual(inv.total, D("28.75"))

    def test_big_order_discount(self):
        o = Order("o2", "c1", "Acme", "OR", False, [Line("a", 10, D("30.00"))])
        inv = build_invoice(o)
        self.assertEqual(inv.discount, D("15.00"))


if __name__ == "__main__":
    unittest.main()
