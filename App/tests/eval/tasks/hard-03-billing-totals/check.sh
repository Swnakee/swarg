#!/bin/sh
export PYTHONDONTWRITEBYTECODE=1
cat > /tmp/_bill_hidden.py <<'PY'
import random, sys, unittest
from decimal import Decimal as D, ROUND_HALF_UP
sys.path.insert(0, ".")
from billing.invoice import build_invoice
from billing.models import Line, Order
from billing.report import customer_totals

def r(x): return x.quantize(D("0.01"), rounding=ROUND_HALF_UP)

def oracle(o):
    lt = [r(l.qty * l.unit_price) for l in o.lines]
    sub = sum(lt, D("0.00"))
    if sub == 0: return [D("0.00")] * 5
    pct = 15 if sub >= 2000 else 10 if sub >= 500 else 5 if sub >= 100 else 0
    if o.loyalty: pct += 5
    pct = min(pct, 18)
    disc = r(sub * pct / 100)
    tsub = sum((t for t, l in zip(lt, o.lines) if l.taxable), D("0.00"))
    base = tsub - r(disc * tsub / sub)
    rate = {"NY": D("0.08875"), "CA": D("0.0725"), "TX": D("0.0625"), "OR": D("0")}[o.region]
    tax = r(base * rate)
    ship = D("0.00") if sub - disc >= 100 else D("7.50")
    return [sub, disc, tax, ship, sub - disc + tax + ship]

def make(rng, i):
    lines = [Line("s%d" % k, rng.randint(1, 40), D(rng.randint(1, 30000)) / 1000 if rng.random() < .5 else D(rng.randint(1, 600)) / 20,
                  rng.random() < .7) for k in range(rng.randint(0, 5))]
    cid = "c%d" % rng.randint(1, 6)
    return Order("o%d" % i, cid, rng.choice(["Acme", "ACME", "acme corp", "Zed"]), rng.choice(["NY", "CA", "TX", "OR"]),
                 rng.random() < .4, lines)

class T(unittest.TestCase):
    def test_random_orders(self):
        rng = random.Random(7)
        for i in range(400):
            o = make(rng, i)
            inv = build_invoice(o)
            got = [inv.subtotal, inv.discount, inv.tax, inv.shipping, inv.total]
            self.assertEqual(got, oracle(o), (i, [(l.qty, str(l.unit_price), l.taxable) for l in o.lines], o.region, o.loyalty))
    def test_boundaries(self):
        for sub, pct in [("99.99", 0), ("100.00", 5), ("499.99", 5), ("500.00", 10), ("1999.99", 10), ("2000.00", 15)]:
            for loy in (False, True):
                o = Order("x", "c", "n", "OR", loy, [Line("a", 1, D(sub))])
                inv = build_invoice(o)
                p = min(pct + (5 if loy else 0), 18)
                self.assertEqual(inv.discount, r(D(sub) * p / 100), (sub, loy))
        o = Order("x", "c", "n", "OR", False, [Line("a", 1, D("120.00"))])
        o2 = Order("x", "c", "n", "OR", False, [Line("a", 1, D("105.00"))])
        self.assertEqual(build_invoice(o).shipping, D("0.00"))
        self.assertEqual(build_invoice(o2).shipping, D("7.50"))
        self.assertEqual(build_invoice(Order("x", "c", "n", "NY", True, [])).total, D("0.00"))
        with self.assertRaises(ValueError):
            build_invoice(Order("x", "c", "n", "ZZ", False, [Line("a", 1, D("1"))]))
    def test_report(self):
        rng = random.Random(11)
        orders = [make(rng, i) for i in range(60)]
        want = {}
        for o in orders:
            want[o.customer_id] = want.get(o.customer_id, D("0.00")) + oracle(o)[4]
        exp = sorted(want.items(), key=lambda kv: (-kv[1], kv[0]))
        self.assertEqual([(c, t) for c, t in customer_totals(orders)], exp)
    def test_report_ties(self):
        a = Order("1", "c9", "Z", "OR", False, [Line("a", 1, D("60.00"))])
        b = Order("2", "c2", "Y", "OR", False, [Line("a", 1, D("60.00"))])
        self.assertEqual([c for c, _ in customer_totals([a, b])], ["c2", "c9"])

unittest.main(argv=["x"], verbosity=0)
PY
out=$(python3 /tmp/_bill_hidden.py 2>&1) || { echo "FAIL: billing mismatch: $(echo "$out" | grep -E 'Error|Mismatch|!=' | head -n2 | cut -c1-200 | tr '\n' ' ')"; exit 1; }
out=$(python3 tests/test_billing.py 2>&1) || { echo "FAIL: tests/test_billing.py fails"; exit 1; }
echo "PASS: invoices and customer report follow RULES.md"
