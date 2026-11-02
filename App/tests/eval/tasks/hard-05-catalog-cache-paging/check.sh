#!/bin/sh
export PYTHONDONTWRITEBYTECODE=1
cat > /tmp/_cat_hidden.py <<'PY'
import sys, unittest
sys.path.insert(0, ".")
from catalog.service import Service
from catalog.cache import Cache
from catalog.pagination import paginate

def items(n=7):
    return [{"id": i, "name": "n%d" % ((i * 3) % 5), "category": "a" if i % 2 else "b", "price": 10 + (i % 3)} for i in range(1, n + 1)]
def ids(p): return [i["id"] for i in p.items]
class Clock:
    def __init__(self): self.t = 0.0
    def __call__(self): return self.t

class T(unittest.TestCase):
    def test_pagination(self):
        rows = list(range(7))
        p = paginate(rows, 1, 3); self.assertEqual((p.items, p.total, p.total_pages, p.has_next), ([0, 1, 2], 7, 3, True))
        p = paginate(rows, 3, 3); self.assertEqual((p.items, p.has_next), ([6], False))
        p = paginate(rows, 4, 3); self.assertEqual((p.items, p.has_next, p.total_pages), ([], False, 3))
        p = paginate(rows, 3, 7 - 1 + 1); self.assertEqual((p.items, p.total_pages), ([], 1))
        p = paginate([], 1, 5); self.assertEqual((p.items, p.total_pages, p.has_next), ([], 0, False))
        p = paginate(list(range(6)), 2, 3); self.assertEqual((p.items, p.total_pages, p.has_next), ([3, 4, 5], 2, False))
        for a in ((0, 3), (1, 0), (-1, 5)):
            with self.assertRaises(ValueError): paginate(rows, *a)
    def test_sort_ties(self):
        s = Service(items())
        self.assertEqual(ids(s.list_items(sort="price", page_size=20)), [3, 6, 1, 4, 7, 2, 5])
        self.assertEqual(ids(s.list_items(sort="price", desc=True, page_size=20)), [2, 5, 1, 4, 7, 3, 6])
        s2 = Service(list(reversed(items())))
        self.assertEqual(ids(s2.list_items(sort="price", page_size=20)), [3, 6, 1, 4, 7, 2, 5])
        self.assertEqual(ids(s2.list_items(category="b", page_size=20)), [2, 4, 6])
    def test_key_includes_all_args(self):
        s = Service(items())
        a = s.list_items(page=1, page_size=2); b = s.list_items(page=1, page_size=3)
        self.assertEqual((len(a.items), len(b.items)), (2, 3))
        c = s.list_items(sort="price"); d = s.list_items(sort="name"); e = s.list_items(sort="price", desc=True)
        self.assertNotEqual((ids(c), ids(e)), (ids(d), ids(d)))
        self.assertEqual(ids(c)[0], 3); self.assertEqual(ids(e)[0], 2)
        f = s.list_items(category="a"); g = s.list_items(category=None)
        self.assertEqual((f.total, g.total), (4, 7))
    def test_cached_and_isolated(self):
        s = Service(items())
        p = s.list_items(page_size=3); n = s.store.scans
        p.items.clear(); p.total = 99
        q = s.list_items(page_size=3)
        self.assertEqual((ids(q), q.total), ([1, 2, 3], 7))
        self.assertEqual(s.store.scans, n)
        q.items[0]["name"] = "HACKED"
        self.assertNotEqual(s.list_items(page_size=3).items[0]["name"], "HACKED")
        it = s.get_item(1); it["price"] = 1
        self.assertNotEqual(s.get_item(1)["price"], 1)
        s.get_item(2); n = s.store.scans
        for _ in range(3): s.list_items(page_size=3); s.get_item(2)
        self.assertEqual(s.store.scans, n)
    def test_invalidation(self):
        s = Service(items())
        s.list_items(page_size=3); s.list_items(category="a", page_size=2); s.get_item(1)
        s.update_item(1, name="zzz", category="b")
        self.assertEqual(s.get_item(1)["name"], "zzz")
        self.assertEqual(s.list_items(page_size=3).items[0]["name"], "zzz")
        self.assertEqual(s.list_items(category="a", page_size=2).total, 3)
        s.add_item({"id": 20, "name": "new", "category": "a", "price": 1})
        self.assertEqual(s.list_items(category="a", page_size=2).total, 4)
        self.assertEqual(s.list_items(page_size=3, page=3).total, 8)
        s.delete_item(2)
        self.assertEqual(s.list_items(page_size=3).total, 7)
        self.assertNotIn(2, ids(s.list_items(page_size=50)))
        with self.assertRaises(KeyError): s.get_item(2)
        s.get_item(3); s.delete_item(3)
        with self.assertRaises(KeyError): s.get_item(3)
        with self.assertRaises(KeyError): s.update_item(3, name="x")
        with self.assertRaises(KeyError): s.delete_item(3)
        with self.assertRaises(ValueError): s.add_item({"id": 1, "name": "d", "category": "a", "price": 1})
    def test_ttl(self):
        c = Clock(); s = Service(items(), clock=c, ttl=30)
        s.list_items(); n = s.store.scans
        c.t = 29.9; s.list_items(); self.assertEqual(s.store.scans, n)
        c.t = 30.0; s.list_items(); self.assertEqual(s.store.scans, n + 1)
    def test_lru(self):
        c = Clock(); k = Cache(2, 100, c)
        k.put("a", 1); k.put("b", 2)
        self.assertEqual(k.get("a"), 1)
        k.put("c", 3)
        self.assertEqual((k.get("a"), k.get("b"), k.get("c")), (1, None, 3))
        k.put("a", 10); k.put("d", 4)
        self.assertEqual((k.get("a"), k.get("c"), k.get("d")), (10, None, 4))

unittest.main(argv=["x"], verbosity=0)
PY
out=$(python3 /tmp/_cat_hidden.py 2>&1) || { echo "FAIL: catalog behaviour: $(echo "$out" | grep -E 'Error|!=|FAIL:' | head -n2 | cut -c1-200 | tr '\n' ' ')"; exit 1; }
out=$(python3 tests/test_catalog.py 2>&1) || { echo "FAIL: tests/test_catalog.py fails"; exit 1; }
echo "PASS: paging, cache keys, invalidation, TTL and LRU are correct"
