#!/bin/sh
export PYTHONDONTWRITEBYTECODE=1
grep -Eq '(^|[^a-zA-Z_.])(eval|exec|compile)\(|import ast|from ast' calc.py && { echo "FAIL: calc.py uses eval/exec/ast"; exit 1; }
cat > /tmp/_calc_hidden.py <<'PY'
import sys, unittest
sys.path.insert(0, ".")
from calc import evaluate, ExprError

def err(text, pos, v=None):
    try:
        evaluate(text, v)
    except ExprError as e:
        assert e.pos == pos, "%r: pos %r != %r (%s)" % (text, e.pos, pos, e)
        assert str(e), "empty message"
        return
    raise AssertionError("%r did not raise" % text)

class T(unittest.TestCase):
    def test_values(self):
        cases = {
            "1+2*3": 7, "(1+2)*3": 9, " 2 ^ 3 ^ 2 ": 512, "-2^2": -4, "2^-1": 0.5, "2^-2^2": 0.0625,
            "10/4": 2.5, "7%3": 1, "-7%3": 2, "7%-3": -2, "--3": 3, "+-+3": -3, "1-2-3": -4,
            "16/4/2": 2, "2*3%4": 2, "3.25*4": 13, "max(1,5,3)": 5, "min(4)": 4, "min(2, -1, 7)": -1,
            "abs(-3.5)": 3.5, "sqrt(16)": 4, "sqrt(2)^2": 2, "max(1,2)+min(3,4)*2": 8,
            "max(min(5,2), 1)": 2, "((((1))))": 1, "1\t+\t1": 2, "-(2+3)^2": -25, "2*-3": -6,
            "4^0.5": 2, "0.1+0.2": 0.3, "-x^2": -9, "x*y-z": 10, "abs(-x)": 3, "_a1+1": 6,
        }
        v = {"x": 3, "y": 4, "z": 2, "_a1": 5}
        for text, want in cases.items():
            got = evaluate(text, v)
            self.assertIsInstance(got, float, text)
            self.assertAlmostEqual(got, want, places=9, msg=text)
    def test_errors(self):
        err("1 +", 3); err("", 0); err("   ", 3); err("2 * (", 5); err("(1+2", 4)
        err("1 + * 2", 4); err(")", 0); err("1 + )", 4); err("1 2", 2); err("(1))", 3)
        err("3 $ 4", 2); err(".5", 0); err("5.", 1); err("1..2", 1); err("2 + #", 4)
        err("foo + 1", 0); err("1 + y", 4, {"x": 1}); err("foo(1)", 0); err("1 + bar(2)", 4)
        err("min()", 0); err("abs(1,2)", 0); err("sqrt(1,2)", 0); err("max(1,)", 6)
        err("sqrt(-1)", 0); err("2 * sqrt(1-5)", 4)
        err("1/0", 1); err("4 + 5 % (3-3)", 6); err("1 / (2-2)", 2)
        err("10^1000", 2); err("1e5", 1)
        err("f(1)", 0, {"f": 1}); err("min(1 2)", 6); err("(,)", 1)
    def test_long_chain(self):
        self.assertEqual(evaluate("+".join(["1"] * 500)), 500.0)
        self.assertEqual(evaluate("(" * 100 + "1" + ")" * 100), 1.0)

unittest.main(argv=["x"], verbosity=0)
PY
out=$(python3 /tmp/_calc_hidden.py 2>&1) || { echo "FAIL: hidden tests: $(echo "$out" | grep -E 'Error|Assertion|FAIL' | head -n2 | tr '\n' ' ')"; exit 1; }
echo "PASS: evaluator follows precedence, associativity and error positions"
