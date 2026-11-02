import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from shop import cli, storage  # noqa: E402
from shop.customer import new_customer  # noqa: E402
from shop.orders import total_for_customer  # noqa: E402
from shop.utils import fmt_money, parse_money  # noqa: E402

DATA = os.path.join(os.path.dirname(__file__), "..", "data", "shop.json")


class Shop(unittest.TestCase):
    def test_money(self):
        self.assertEqual(fmt_money(-123456), "-$1,234.56")
        self.assertEqual(parse_money("$1,234.56"), 123456)

    def test_load_and_totals(self):
        repo = storage.load(DATA)
        self.assertEqual(total_for_customer(repo, 1), 250000)
        self.assertEqual(total_for_customer(repo, 2), 4500)

    def test_new_customer_validates(self):
        with self.assertRaises(ValueError):
            new_customer(1, "X", "not-an-email")

    def test_cli_roundtrip(self):
        path = os.path.join(tempfile.mkdtemp(), "db.json")
        self.assertEqual(cli.main(["add-customer", "Linus T", "l@example.org"], path), "added customer #1\n")
        self.assertIn("Linus T", cli.main(["customers"], path))


if __name__ == "__main__":
    unittest.main()
