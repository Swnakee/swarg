#!/bin/sh
export PYTHONDONTWRITEBYTECODE=1
cat > /tmp/_nt_hidden.py <<'PY'
import csv, json, os, random, shutil, subprocess, sys, tempfile
from decimal import Decimal as D, ROUND_HALF_UP
W = os.getcwd()

def oracle(rows, rates):
    out, skipped = [], 0
    for sku, price, cur, disc in rows:
        c = cur.strip().upper()
        r = D(1) if c == "EUR" else D(str(rates.get(c, 0))) if str(rates.get(c, "0")) else D(0)
        if r <= 0: skipped += 1; continue
        d = D(disc.strip()) if disc.strip() else D(0)
        cents = (D(price.strip()) * (100 - d) / 100 / r * 100).quantize(D(1), rounding=ROUND_HALF_UP)
        out.append((sku.strip(), int(cents)))
    out.sort()
    return out, skipped

def run(name, rows, rates):
    d = tempfile.mkdtemp(); data = os.path.join(d, "data"); os.makedirs(data)
    with open(os.path.join(data, "products.csv"), "w", newline="") as f:
        w = csv.writer(f); w.writerow(["sku", "price", "currency", "discount_pct"]); w.writerows(rows)
    json.dump(rates, open(os.path.join(data, "rates.json"), "w"))
    work = os.path.join(d, "w"); shutil.copytree(W, work, ignore=shutil.ignore_patterns(".eval", "out", "__pycache__"))
    p = subprocess.run([sys.executable, "-m", "jobs.nightly", data, os.path.join(d, "o")], cwd=work, capture_output=True, text=True, timeout=60)
    if p.returncode: sys.exit("%s: job crashed: %s" % (name, p.stderr.strip().splitlines()[-1]))
    want, sk = oracle(rows, rates)
    got = [(r[0], int(r[1])) for r in list(csv.reader(open(os.path.join(d, "o", "prices.csv"))))[1:]]
    if got != want:
        for a, b in zip(got, want):
            if a != b: sys.exit("%s: row differs: got %r want %r" % (name, a, b))
        sys.exit("%s: row count %d != %d" % (name, len(got), len(want)))
    line = "done: priced=%d skipped=%d" % (len(want), sk)
    if p.stdout.strip() != line: sys.exit("%s: stdout %r != %r" % (name, p.stdout.strip(), line))
    log = open(os.path.join(work, "logs", "nightly.log")).read() if os.path.exists(os.path.join(work, "logs", "nightly.log")) else ""
    bad = {c.strip().upper() for _, _, c, _ in rows if c.strip().upper() != "EUR" and not (D(str(rates.get(c.strip().upper(), 0))) > 0)}
    for sku, _, c, _ in rows:
        cu = c.strip().upper()
        if cu in bad and "WARN skip %s: no rate for %s" % (sku.strip(), cu) not in log:
            sys.exit("%s: missing log line for %s" % (name, sku))

# 1: the real data, exactly as shipped (read from the working copy)
rows = list(csv.reader(open(os.path.join(W, "data", "products.csv"))))[1:]
run("shipped", rows, json.load(open(os.path.join(W, "data", "rates.json"))))
# 2: random hidden data
rng = random.Random(21)
for k in range(3):
    rows = []
    for i in range(80):
        cur = rng.choice(["EUR", "usd", " USD ", "Gbp", "PLN", "pln ", "NOK", "ZZZ", "eur", "JPY", "CAD"])
        price = rng.choice(["%d.%02d" % (rng.randint(0, 500), rng.randint(0, 99)), "0.10", "0.05", "2.50", "100", "0.01", "9.99"])
        disc = rng.choice(["", "0", "10", "50", "12.5", "33", " 7 ", "100"])
        rows.append(["S-%03d" % rng.randint(0, 999), price, cur, disc])
    rates = {"USD": "1.0850", "GBP": "0.8600", "PLN": 4.3, "JPY": "160.5", "NOK": "0", "CAD": "1.5", "XXX": "2"}
    if k == 1: rates = {"USD": "2", "GBP": "0.5", "PLN": "4", "JPY": "-3", "CAD": "3"}
    run("hidden%d" % k, rows, rates)
PY
out=$(python3 /tmp/_nt_hidden.py 2>&1) || { echo "FAIL: $(echo "$out" | tail -n1 | cut -c1-240)"; exit 1; }
echo "PASS: nightly job normalises currencies, skips missing rates and rounds half up"
