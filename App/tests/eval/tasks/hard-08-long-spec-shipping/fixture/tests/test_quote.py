import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from shipping import quote  # noqa: E402


class Example(unittest.TestCase):
    def test_small_parcel(self):
        p = {"weight_kg": 2.0, "l": 20, "w": 20, "h": 20, "declared_value": 0, "hazmat": False}
        q = quote(p, "10115", "40000")
        self.assertEqual(q["base"], 1100)
        self.assertEqual(q["fuel"], 91)
        self.assertEqual(q["total"], 1191)


if __name__ == "__main__":
    unittest.main()
