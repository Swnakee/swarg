import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from logstats import dedupe, rolling_unique  # noqa: E402


class T(unittest.TestCase):
    def test_dedupe(self):
        ev = [(0, "a", "x"), (5, "a", "x"), (10, "a", "x"), (11, "b", "x")]
        self.assertEqual(dedupe(ev, 10), [(0, "a", "x"), (10, "a", "x"), (11, "b", "x")])

    def test_rolling(self):
        ev = [(0, "a", "x"), (1, "b", "x"), (2, "a", "x"), (10, "c", "x")]
        self.assertEqual(rolling_unique(ev, 5), [1, 2, 2, 1])


if __name__ == "__main__":
    unittest.main()
