import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from catalog.service import Service  # noqa: E402

ITEMS = [{"id": i, "name": "n%d" % i, "category": "a" if i % 2 else "b", "price": 100 - i} for i in range(1, 8)]


class Basic(unittest.TestCase):
    def test_list_cached(self):
        s = Service(ITEMS)
        p = s.list_items(page_size=3)
        self.assertEqual([i["id"] for i in p.items], [1, 2, 3])
        s.list_items(page_size=3)
        self.assertEqual(s.store.scans, 1)

    def test_get_item(self):
        s = Service(ITEMS)
        self.assertEqual(s.get_item(2)["name"], "n2")


if __name__ == "__main__":
    unittest.main()
