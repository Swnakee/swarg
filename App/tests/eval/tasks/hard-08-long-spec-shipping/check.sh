#!/bin/sh
export PYTHONDONTWRITEBYTECODE=1
cat > /tmp/_ship_hidden.py <<'PY'
import math, random, sys, unittest
from fractions import Fraction as F
sys.path.insert(0, ".")
from shipping import quote, Prohibited, ParcelTooHeavy

RATES = {1: (500, 650, 900, 1300, 2000, 90), 2: (600, 780, 1050, 1500, 2300, 100), 3: (700, 900, 1200, 1750, 2700, 120),
         4: (850, 1100, 1500, 2100, 3300, 150), 5: (1000, 1300, 1800, 2600, 4000, 180), 6: (1200, 1600, 2200, 3200, 5000, 220)}
def hup(x): return int(math.floor(F(x) + F(1, 2)))
def oracle(p, o, d, sat=False):
    for pc in (o, d):
        if not (isinstance(pc, str) and len(pc) == 5 and pc.isascii() and pc.isdigit()): raise ValueError(pc)
    if p["hazmat"]: raise Prohibited()
    if F(str(p["weight_kg"])) > 70: raise ParcelTooHeavy()
    dim = F(str(p["l"])) * F(str(p["w"])) * F(str(p["h"])) / 5000
    b = max(F(str(p["weight_kg"])), dim)
    b = F(math.ceil(b * 2), 2)
    b = max(b, F(1))
    zone = min(6, 1 + abs(int(o[0]) - int(d[0])))
    r = RATES[zone]
    for lim, price in zip((1, 2, 5, 10, 20), r[:5]):
        if b <= lim: base = price; break
    else:
        base = r[4] + math.ceil(b - 20) * r[5]
    sides = sorted([p["l"], p["w"], p["h"]])
    over = 1500 if (sides[2] > 100 or sides[2] + 2 * (sides[0] + sides[1]) > 250) else 0
    remote = 800 if d[:2] in ("99", "98", "00") else 0
    if sat and remote: raise ValueError("saturday")
    satf = 500 if sat else 0
    fuel = hup(F(base + over + remote + satf) * F(825, 10000))
    dv = p["declared_value"]
    ins = max(200, min(5000, hup(F(dv, 100)))) if dv > 10000 else 0
    out = dict(base=base, oversize=over, remote=remote, saturday=satf, fuel=fuel, insurance=ins)
    out["total"] = sum(out.values())
    return out

def P(w=1, l=10, wd=10, h=10, dv=0, hz=False): return {"weight_kg": w, "l": l, "w": wd, "h": h, "declared_value": dv, "hazmat": hz}

class T(unittest.TestCase):
    def test_literals(self):
        q = quote(P(2.0, 20, 20, 20), "10115", "40000")
        self.assertEqual(q, dict(base=1100, oversize=0, remote=0, saturday=0, fuel=91, insurance=0, total=1191))
        q = quote(P(25.2, 130, 30, 30, 20000), "10115", "99000")
        self.assertEqual(q, dict(base=6320, oversize=1500, remote=800, saturday=0, fuel=711, insurance=200, total=9531))
        q = quote(P(0.1, 5, 5, 5), "10115", "10115", True)
        self.assertEqual((q["base"], q["saturday"], q["fuel"], q["total"]), (500, 500, 83, 1083))
    def test_edges(self):
        for args in [(P(2.0, 10, 10, 10), "10000", "10000"), (P(2.01, 10, 10, 10), "10000", "10000"),
                     (P(0.2, 10, 10, 10), "10000", "20000"), (P(20.0, 10, 10, 10), "10000", "30000"),
                     (P(20.01, 10, 10, 10), "10000", "30000"), (P(21.0, 10, 10, 10), "10000", "30000"),
                     (P(70, 10, 10, 10), "10000", "30000"), (P(1, 100, 10, 10), "00000", "98001"),
                     (P(1, 101, 10, 10), "00000", "98001"), (P(1, 100, 75, 50), "00000", "00001"),
                     (P(1, 100, 75, 50.5), "00000", "00001"), (P(1, 50, 50, 50), "50000", "50000"),
                     (P(1, 100, 100, 100), "50000", "50000"), (P(3, 10, 10, 10, 10000), "1", "1") if False else (P(3, 10, 10, 10, 10000), "10000", "10000"),
                     (P(3, 10, 10, 10, 10001), "10000", "10000"), (P(3, 10, 10, 10, 1000000), "10000", "10000"),
                     (P(3, 10, 10, 10, 12350), "10000", "10000"), (P(3, 10, 10, 10, 20050), "10000", "10000"),
                     (P(10, 50, 40, 25), "90000", "10000"), (P(1, 10, 10, 10), "99000", "99000")]:
            self.assertEqual(quote(*args), oracle(*args), args)
    def test_saturday(self):
        for d in ("99001", "98001", "00100"):
            with self.assertRaises(ValueError): quote(P(), "10000", d, True)
        self.assertEqual(quote(P(), "10000", "97000", True), oracle(P(), "10000", "97000", True))
        self.assertEqual(quote(P(), "10000", "99001"), oracle(P(), "10000", "99001"))
    def test_errors(self):
        for bad in ("1234", "123456", "12a45", "", "٣٣٣٣٣"):
            with self.assertRaises(ValueError): quote(P(), bad, "10000")
            with self.assertRaises(ValueError): quote(P(), "10000", bad)
        with self.assertRaises(ValueError): quote(P(hz=True), "1", "10000")
        with self.assertRaises(Prohibited): quote(P(hz=True), "10000", "10000")
        with self.assertRaises(Prohibited): quote(P(w=80, hz=True), "10000", "10000")
        with self.assertRaises(ParcelTooHeavy): quote(P(w=70.5), "10000", "10000")
        quote(P(w=70), "10000", "10000")
        quote(P(w=1, l=200, wd=100, h=100), "10000", "10000")  # heavy dimensional weight is allowed
    def test_random(self):
        rng = random.Random(5)
        for _ in range(1500):
            p = P(rng.choice([rng.randint(1, 700) / 10, rng.randint(1, 30) / 2]), rng.randint(5, 140), rng.randint(5, 90), rng.randint(5, 90),
                  rng.choice([0, 500, 10000, 10001, rng.randint(0, 1000000)]))
            o = "%05d" % rng.choice([0, 10115, 45000, 99000, 98100, rng.randint(0, 99999)])
            d = "%05d" % rng.choice([0, 10115, 45000, 99000, 98100, rng.randint(0, 99999)])
            sat = rng.random() < .3
            try: want = oracle(p, o, d, sat)
            except ValueError:
                with self.assertRaises(ValueError): quote(p, o, d, sat)
                continue
            self.assertEqual(quote(p, o, d, sat), want, (p, o, d, sat))

unittest.main(argv=["x"], verbosity=0)
PY
out=$(python3 /tmp/_ship_hidden.py 2>&1) || { echo "FAIL: quote mismatch: $(echo "$out" | grep -E 'Error|!=|FAIL:' | head -n2 | cut -c1-240 | tr '\n' ' ')"; exit 1; }
out=$(python3 tests/test_quote.py 2>&1) || { echo "FAIL: tests/test_quote.py fails"; exit 1; }
echo "PASS: Ground quote follows spec sections and amendments"
