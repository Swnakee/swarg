import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from intervals import Interval, merge, parse, format, intersect, subtract  # noqa: E402


class Basic(unittest.TestCase):
    def test_parse_format(self):
        self.assertEqual(format(parse("[1, 5)")), "[1,5)")

    def test_merge_overlap(self):
        r = merge([parse("[1,3]"), parse("[2,6]")])
        self.assertEqual([format(i) for i in r], ["[1,6]"])

    def test_intersect(self):
        r = intersect([parse("[1,5]")], [parse("[3,9]")])
        self.assertEqual([format(i) for i in r], ["[3,5]"])


if __name__ == "__main__":
    unittest.main()
