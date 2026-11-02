#!/bin/sh
export PYTHONDONTWRITEBYTECODE=1
cat > /tmp/_iv_hidden.py <<'PY'
import sys, unittest
sys.path.insert(0, ".")
from intervals import Interval, merge, parse, format, intersect, subtract
INF = float("inf")
def F(lst): return [format(i) for i in lst]
def P(*s): return [parse(x) for x in s]

class T(unittest.TestCase):
    def test_parse(self):
        self.assertEqual(parse("[1,5)"), Interval(1, 5, True, False))
        self.assertEqual(parse(" ( -inf , 3 ] "), Interval(-INF, 3, False, True))
        self.assertEqual(parse("(2,+inf)"), Interval(2, INF, False, False))
        self.assertIsInstance(parse("[0.5,2]").lo, float)
        self.assertIsInstance(parse("[1,2]").lo, int)
        self.assertEqual(parse("[5,1]").lo, 5)
        for bad in ["1,5", "[1,5", "[1 5]", "[1,2,3]", "[a,5]", "[,5]", "", "[1,]", "[1;5]"]:
            with self.assertRaises(ValueError, msg=bad):
                parse(bad)
    def test_format(self):
        self.assertEqual(format(Interval(-INF, 3, True, True)), "(-inf,3]")
        self.assertEqual(format(Interval(2, INF)), "[2,inf)")
        self.assertEqual(format(Interval(3, 3, True, False)), "empty")
        self.assertEqual(format(Interval(5, 1)), "empty")
        self.assertEqual(format(Interval(3, 3)), "[3,3]")
        self.assertEqual(format(Interval(0.5, 2)), "[0.5,2]")
    def test_merge_touching(self):
        self.assertEqual(F(merge(P("[1,2)", "[2,3]"))), ["[1,3]"])
        self.assertEqual(F(merge(P("[1,2]", "(2,3]"))), ["[1,3]"])
        self.assertEqual(F(merge(P("[1,2)", "(2,3]"))), ["[1,2)", "(2,3]"])
        self.assertEqual(F(merge(P("(2,3]", "[1,2)"))), ["[1,2)", "(2,3]"])
    def test_merge_misc(self):
        self.assertEqual(F(merge([])), [])
        self.assertEqual(F(merge(P("(3,3)", "[4,4)", "[5,1]"))), [])
        self.assertEqual(F(merge(P("[1,10]", "[2,3]", "(4,5)"))), ["[1,10]"])
        self.assertEqual(F(merge(P("[1,5)", "[1,5]"))), ["[1,5]"])
        self.assertEqual(F(merge(P("(1,5]", "[1,5)"))), ["[1,5]"])
        self.assertEqual(F(merge(P("(1,5)", "[1,3]"))), ["[1,5)"])
        self.assertEqual(F(merge(P("[7,8]", "[1,2]", "[3,3]"))), ["[1,2]", "[3,3]", "[7,8]"])
        self.assertEqual(F(merge(P("(-inf,0)", "[0,0]", "(0,inf)"))), ["(-inf,inf)"])
        self.assertEqual(F(merge(P("(-inf,0)", "(0,inf)"))), ["(-inf,0)", "(0,inf)"])
        self.assertEqual(F(merge(P("[1,3]", "[3,3]", "[3,5]"))), ["[1,5]"])
        self.assertEqual(F(merge(P("[1,4]", "[2,3)"))), ["[1,4]"])
        self.assertEqual(F(merge(P("[1,4)", "[2,4]"))), ["[1,4]"])
        self.assertEqual(F(merge(P("[1,4]", "[2,4)"))), ["[1,4]"])
        self.assertEqual(F(merge(iter(P("[2,3]", "[1,2]")))), ["[1,3]"])
        src = P("[5,6]", "[1,2]"); before = list(src); merge(src)
        self.assertEqual(src, before)
    def test_intersect(self):
        self.assertEqual(F(intersect(P("[1,5)"), P("[5,9]"))), [])
        self.assertEqual(F(intersect(P("[1,5]"), P("[5,9]"))), ["[5,5]"])
        self.assertEqual(F(intersect(P("[1,5]"), P("(1,5)"))), ["(1,5)"])
        self.assertEqual(F(intersect(P("(1,5]"), P("[1,5)"))), ["(1,5)"])
        self.assertEqual(F(intersect(P("[0,10]", "[20,30]"), P("[5,25]"))), ["[5,10]", "[20,25]"])
        self.assertEqual(F(intersect(P("[8,9]", "[0,3]", "[2,5]"), P("[1,4]", "[4,8]"))),
                         ["[1,5]", "[8,8]"])
        self.assertEqual(F(intersect(P("(-inf,inf)"), P("[1,2]"))), ["[1,2]"])
        self.assertEqual(F(intersect([], P("[1,2]"))), [])
        self.assertEqual(F(intersect(P("(-inf,3)"), P("(1,inf)"))), ["(1,3)"])
    def test_subtract(self):
        self.assertEqual(F(subtract(P("[0,10]"), P("(3,5)"))), ["[0,3]", "[5,10]"])
        self.assertEqual(F(subtract(P("[0,10]"), P("[3,5]"))), ["[0,3)", "(5,10]"])
        self.assertEqual(F(subtract(P("[0,10]"), P("[5,5]"))), ["[0,5)", "(5,10]"])
        self.assertEqual(F(subtract(P("[0,10]"), P("[0,10]"))), [])
        self.assertEqual(F(subtract(P("[0,10]"), P("(0,10)"))), ["[0,0]", "[10,10]"])
        self.assertEqual(F(subtract(P("(-inf,inf)"), P("[0,0]"))), ["(-inf,0)", "(0,inf)"])
        self.assertEqual(F(subtract(P("[0,10]"), P("[20,30]"))), ["[0,10]"])
        self.assertEqual(F(subtract(P("[0,10]"), P("[-5,2]", "[8,15]"))), ["(2,8)"])
        self.assertEqual(F(subtract(P("[0,10]", "[20,30]"), P("[5,25]"))), ["[0,5)", "(25,30]"])
        self.assertEqual(F(subtract(P("[0,5)"), P("[5,9]"))), ["[0,5)"])
        self.assertEqual(F(subtract(P("[0,5]"), P("(5,9]"))), ["[0,5]"])
        self.assertEqual(F(subtract(P("[0,10]"), P("(-inf,4)", "(6,inf)"))), ["[4,6]"])
        self.assertEqual(F(subtract([], P("[1,2]"))), [])
        self.assertEqual(F(subtract(P("[0,10]"), [])), ["[0,10]"])
        self.assertEqual(F(subtract(P("[1,3]", "[2,6]"), P("[4,4]"))), ["[1,4)", "(4,6]"])

unittest.main(argv=["x"], verbosity=0)
PY
out=$(python3 /tmp/_iv_hidden.py 2>&1) || { echo "FAIL: hidden tests: $(echo "$out" | grep -E 'Error|FAIL' | head -n2 | tr '\n' ' ')"; exit 1; }
out=$(python3 tests/test_basic.py 2>&1) || { echo "FAIL: tests/test_basic.py fails"; exit 1; }
echo "PASS: interval set operations handle open/closed ends"
