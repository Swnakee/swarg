#!/bin/sh
export PYTHONDONTWRITEBYTECODE=1
ORIG=$(pwd)
[ -f migrate.py ] || { echo "FAIL: migrate.py does not exist"; exit 1; }
rm -rf /tmp/_mig; mkdir -p /tmp/_mig && cd /tmp/_mig || exit 1
cat > gen.py <<'PY'
import csv, random

def gen_rows(n, seed):
    rng = random.Random(seed)
    def pick(good, bad, p=0.1):
        return rng.choice(bad) if rng.random() < p else rng.choice(good)
    rows = []
    for i in range(n):
        cid = str(1000 + i)
        if rows and rng.random() < .04:
            cid = rows[rng.randrange(len(rows))][0]
        elif rng.random() < .04:
            cid = rng.choice(["x%d" % i, "0", "-5", "", "12a"])
        elif rng.random() < .2:
            cid = " %s " % cid
        name = pick(["Anna  Lee", " bob  smith ", "Carla Díaz", "dmitri ivanov", "Eve O'Neil", "Frank  Müller"], ["", "   "], .05)
        user = "u%d" % i
        if rows and rng.random() < .04:
            user = "u%d" % rng.randrange(i)
        dom = rng.choice(["example.com", "Example.COM", "mail.example.org", "test.example.net"])
        email = pick([user + "@" + dom, " " + user.upper() + "@" + dom + " "], [user + "@@x.com", "nodomain", user + "@host", "sp ace@x.com", "", user + "@a.b.c."], .1)
        ph = pick(["", "(555) 123-4567", "555-123-4567", "+44 20 7946 0958", "0044 20 7946 0958", "1 555 123 4567", "+1 (555) 000-1111", "+49 30 1234567", "00 49 30 1234567", "+1-555-123-4567 ", "555.123.4567"], ["12345", "abc", "5551234", "+123456789012345678", "+1234567", "555-123-456x"], .12)
        country = pick(["US", "us", " gb ", "USA", "United States", "U.S.", "UK", "Great Britain", "Germany", "Deutschland", "DE", "FR", "PL", "UA", "CA"], ["RU", "XX", "", "Canada"], .08)
        y = rng.randint(1990, 2025); m = rng.randint(1, 12); d = rng.randint(1, 28)
        signup = pick(["%04d-%02d-%02d" % (y, m, d), "%02d.%02d.%04d" % (d, m, y), "%02d/%02d/%04d" % (m, d, y), "%04d/%02d/%02d" % (y, m, d), "2024-02-29", "31.12.1990", "01/31/2021"],
                      ["2020-02-30", "31.04.2021", "13/01/2021", "2023-02-29", "", "yesterday", "2020-1-5", "1989-12-31", "2026-01-01", "31/12/2020"], .1)
        bal = pick(["", "0", "12", "$12", "1,234.50", "-3.5", "(10.00)", "12.345", "12.355", "-0.005", "€ 1,000", " 7.1 ", "$(5.25)", "0.004", "999999999.99", "0.005", "-12.345", "(0.015)", "1,000"], ["1.2.3", "abc", ".5", "5.", "1,2,3.4", "1,23", "--5", "12-"], .1)
        tags = rng.choice(["", "vip;Beta", "a|b|A", " x ; y ;; ", "Z;z;Z", "one", "Mix|ed;Case"])
        rows.append([cid, name, email, ph, country, signup, bal, tags])
    for r in rows:
        if rng.random() < .015:
            del r[rng.randrange(len(r))]
        elif rng.random() < .01:
            r.append("extra")
    return rows

def write_csv(path, rows, bom=False):
    with open(path, "w", newline="", encoding="utf-8-sig" if bom else "utf-8") as f:
        w = csv.writer(f)
        w.writerow(["id", "name", "email", "phone", "country", "signup", "balance", "tags"])
        w.writerows(rows)

PY
cat > oracle.py <<'PY'
"""Migrate legacy_customers.csv to the schema in SCHEMA.md.

Usage: python3 migrate.py <input.csv> <customers.json> <rejects.json>
"""
import csv
import datetime
import json
import re
import sys
from decimal import ROUND_HALF_UP, Decimal, InvalidOperation

COUNTRIES = {"US", "GB", "DE", "FR", "PL", "UA", "CA"}
ALIASES = {"USA": "US", "UNITED STATES": "US", "U.S.": "US", "UK": "GB", "UNITED KINGDOM": "GB",
           "GREAT BRITAIN": "GB", "GERMANY": "DE", "DEUTSCHLAND": "DE"}
EMAIL = re.compile(r"^[^@\s]+@[^@\s]+\.[^@\s.]+$")


class Reject(Exception):
    pass


def clean_phone(raw):
    raw = raw.strip()
    if not raw:
        return None
    if re.search(r"[^0-9 ()+.\-]", raw):
        raise Reject("bad_phone")
    if "+" in raw[1:]:
        raise Reject("bad_phone")
    plus = raw.startswith("+")
    digits = re.sub(r"\D", "", raw)
    if plus:
        out = "+" + digits
    elif digits.startswith("00"):
        out = "+" + digits[2:]
    elif len(digits) == 10:
        out = "+1" + digits
    elif len(digits) == 11 and digits.startswith("1"):
        out = "+" + digits
    else:
        raise Reject("bad_phone")
    if not re.fullmatch(r"\+[0-9]{8,15}", out):
        raise Reject("bad_phone")
    return out


def clean_country(raw):
    c = raw.strip().upper()
    c = ALIASES.get(c, c)
    if c not in COUNTRIES:
        raise Reject("bad_country")
    return c


def clean_date(raw):
    s = raw.strip()
    m = re.fullmatch(r"(\d{4})-(\d{2})-(\d{2})", s)
    if m:
        y, mo, d = map(int, m.groups())
    else:
        m = re.fullmatch(r"(\d{2})\.(\d{2})\.(\d{4})", s)
        if m:
            d, mo, y = map(int, m.groups())
        else:
            m = re.fullmatch(r"(\d{2})/(\d{2})/(\d{4})", s)
            if m:
                mo, d, y = map(int, m.groups())
            else:
                m = re.fullmatch(r"(\d{4})/(\d{2})/(\d{2})", s)
                if not m:
                    raise Reject("bad_date")
                y, mo, d = map(int, m.groups())
    try:
        dt = datetime.date(y, mo, d)
    except ValueError:
        raise Reject("bad_date")
    if not 1990 <= y <= 2025:
        raise Reject("bad_date")
    return dt.isoformat()


def clean_balance(raw):
    s = raw.strip().replace(" ", "")
    if not s:
        return 0
    s = s.replace("$", "").replace("€", "")
    neg = False
    if s.startswith("(") and s.endswith(")"):
        neg, s = True, s[1:-1]
    if s.startswith("-"):
        neg, s = True, s[1:]
    if not re.fullmatch(r"(\d{1,3}(,\d{3})+|\d+)(\.\d+)?", s):
        raise Reject("bad_balance")
    val = Decimal(s.replace(",", "")).quantize(Decimal("0.01"), rounding=ROUND_HALF_UP)
    cents = int(val * 100)
    return -cents if neg else cents


def migrate_row(fields, seen_ids, seen_emails):
    if len(fields) != 8:
        raise Reject("bad_columns")
    rid, name, email, phone, country, signup, balance, tags = fields
    rid = rid.strip()
    if not re.fullmatch(r"\d+", rid) or int(rid) <= 0:
        raise Reject("bad_id")
    cid = int(rid)
    if cid in seen_ids:
        raise Reject("duplicate_id")
    name = " ".join(name.split())
    if not name:
        raise Reject("missing_name")
    email = email.strip().lower()
    if not EMAIL.match(email):
        raise Reject("bad_email")
    if email in seen_emails:
        raise Reject("duplicate_email")
    rec = {"id": cid, "name": name, "email": email, "phone": clean_phone(phone),
           "country": clean_country(country), "signup": clean_date(signup),
           "balance_cents": clean_balance(balance),
           "tags": sorted({t.strip().lower() for t in re.split(r"[;|]", tags) if t.strip()})}
    seen_ids.add(cid)
    seen_emails.add(email)
    return rec


def main(src, out_ok, out_bad):
    ok, bad = [], []
    seen_ids, seen_emails = set(), set()
    with open(src, newline="", encoding="utf-8-sig") as f:
        rows = [r for r in csv.reader(f) if r]
    for n, fields in enumerate(rows[1:], 1):
        try:
            ok.append(migrate_row(fields, seen_ids, seen_emails))
        except Reject as e:
            bad.append({"row": n, "id": fields[0] if fields else "", "reason": str(e)})
    ok.sort(key=lambda r: r["id"])
    with open(out_ok, "w", encoding="utf-8") as f:
        json.dump(ok, f, indent=2, ensure_ascii=False)
    with open(out_bad, "w", encoding="utf-8") as f:
        json.dump(bad, f, indent=2, ensure_ascii=False)
    print("accepted %d rejected %d" % (len(ok), len(bad)))


if __name__ == "__main__":
    main(*sys.argv[1:4])

PY
cat > run.py <<'PY'
import json, os, subprocess, sys
import gen, oracle
W = os.environ["WORKDIR"]
def case(name, rows, bom=True, blank=False):
    src = "in_" + name + ".csv"
    gen.write_csv(src, rows, bom=bom)
    if blank:
        txt = open(src, newline="", encoding="utf-8").read().split("\r\n")
        txt.insert(5, ""); txt.insert(9, "")
        open(src, "w", newline="", encoding="utf-8").write("\r\n".join(txt) + "\r\n\r\n")
    oracle.main(src, "exp_ok_" + name + ".json", "exp_bad_" + name + ".json")
    got_ok, got_bad = "got_ok_" + name + ".json", "got_bad_" + name + ".json"
    p = subprocess.run([sys.executable, os.path.join(W, "migrate.py"), os.path.abspath(src), os.path.abspath(got_ok), os.path.abspath(got_bad)],
                       capture_output=True, text=True, timeout=120, cwd=W)
    if p.returncode:
        sys.exit("migrate.py exit %d: %s" % (p.returncode, p.stderr.strip().splitlines()[-1:]))
    L = lambda f: json.load(open(f, encoding="utf-8"))
    ok, eok = L(got_ok), L("exp_ok_" + name + ".json")
    bad, ebad = L(got_bad), L("exp_bad_" + name + ".json")
    if ok != eok:
        for a, b in zip(ok, eok):
            if a != b: sys.exit("%s: record differs: got %r want %r" % (name, a, b))
        sys.exit("%s: accepted count %d != %d" % (name, len(ok), len(eok)))
    if bad != ebad:
        for a, b in zip(bad, ebad):
            if a != b: sys.exit("%s: reject differs: got %r want %r" % (name, a, b))
        sys.exit("%s: rejects count %d != %d" % (name, len(bad), len(ebad)))
    line = "accepted %d rejected %d" % (len(eok), len(ebad))
    if p.stdout.strip() != line: sys.exit("%s: stdout %r != %r" % (name, p.stdout.strip(), line))
case("a", gen.gen_rows(700, 99))
case("b", gen.gen_rows(150, 3), bom=False, blank=True)
case("c", [])
r = gen.gen_rows(30, 8)
case("d", r[:10] + [["1005", "dup", "d@x.example.com", "", "US", "2020-01-01", "", ""]] + r[10:])
PY
out=$(WORKDIR="$ORIG" python3 run.py 2>&1) || { echo "FAIL: $(echo "$out" | tail -n1 | cut -c1-240)"; exit 1; }
echo "PASS: migration output and rejects match the schema rules"
