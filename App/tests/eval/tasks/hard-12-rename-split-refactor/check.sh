#!/bin/sh
export PYTHONDONTWRITEBYTECODE=1
for f in shop/account.py shop/money.py shop/strings.py; do [ -f "$f" ] || { echo "FAIL: $f is missing"; exit 1; }; done
for f in shop/customer.py shop/utils.py; do [ -e "$f" ] && { echo "FAIL: $f still exists"; exit 1; }; done
cat > /tmp/_shop_grep.py <<'PY'
import glob, re
bad = []
for p in glob.glob("shop/*.py"):
    s = open(p).read()
    if p.endswith("storage.py"):
        s = s.replace('"customers"', "").replace('"customer_id"', "")
    for n, line in enumerate(s.splitlines(), 1):
        if re.search("customer", line, re.I):
            bad.append("%s:%d" % (p, n))
print(" ".join(bad[:6]))
PY
out=$(python3 /tmp/_shop_grep.py)
[ -z "$out" ] || { echo "FAIL: old word still in code: $out"; exit 1; }
cat > /tmp/_shop_legacy.json <<'JSON'
{
 "customers": [
  {"customer_id": 1, "name": "Ada Lovelace", "email": "ada@example.com", "balance_cents": 125050, "tier": "gold"},
  {"customer_id": 2, "name": "Grace Hopper", "email": "grace@example.com", "balance_cents": 990, "tier": "basic"}
 ],
 "orders": [
  {"order_id": 1, "customer_id": 1, "amount_cents": 250000, "status": "paid"},
  {"order_id": 2, "customer_id": 1, "amount_cents": 1999, "status": "cancelled"},
  {"order_id": 3, "customer_id": 2, "amount_cents": 4500}
 ]
}
JSON
cat > /tmp/_shop_hidden.py <<'PY'
import json, os, sys, tempfile, unittest, importlib
sys.path.insert(0, ".")
from shop import cli, storage
from shop.account import Account, new_account
from shop.repo import AccountRepo
from shop.orders import Order, place_order, orders_for_account, total_for_account
from shop.billing import invoice_for_account, invoice_filename
from shop.notify import welcome_email, low_balance_notice
from shop.reports import account_report
from shop.money import fmt_money, parse_money
from shop.strings import slug, truncate
LEGACY = "/tmp/_shop_legacy.json"

class T(unittest.TestCase):
    def test_old_modules_gone(self):
        for m in ("shop.utils", "shop.customer"):
            with self.assertRaises(ImportError): importlib.import_module(m)
    def test_helpers(self):
        self.assertEqual(fmt_money(-123456), "-$1,234.56"); self.assertEqual(fmt_money(5), "$0.05")
        self.assertEqual(parse_money(" -$1,234.50 "), -123450); self.assertEqual(parse_money("7"), 700)
        with self.assertRaises(ValueError): parse_money("1,23")
        self.assertEqual(slug("  Ada  Lovelace!! "), "ada-lovelace"); self.assertEqual(truncate("abcdefghij", 5), "abcd~"); self.assertEqual(truncate("abc", 5), "abc")
    def test_account_and_repo(self):
        a = new_account(3, "  Zed ", "ZED@Example.com")
        self.assertEqual(a, Account(3, "Zed", "zed@example.com", 0, "basic"))
        with self.assertRaises(ValueError) as cm: new_account(1, "x", "bad")
        self.assertEqual(str(cm.exception), "invalid account email: bad")
        r = AccountRepo(); self.assertEqual(r.next_account_id(), 1)
        r.add_account(a); self.assertIs(r.get_account(3), a); self.assertEqual(r.accounts(), [a]); self.assertEqual(r.next_account_id(), 4)
        with self.assertRaises(KeyError) as cm: r.get_account(9)
        self.assertIn("account 9 not found", str(cm.exception))
    def test_legacy_load_and_orders(self):
        r = storage.load(LEGACY)
        self.assertEqual([a.name for a in r.accounts()], ["Ada Lovelace", "Grace Hopper"])
        self.assertEqual(total_for_account(r, 1), 250000); self.assertEqual(total_for_account(r, 2), 4500)
        self.assertEqual([o.id for o in orders_for_account(r, 1)], [1, 2])
        o = place_order(r, account_id=2, amount_cents=100)
        self.assertEqual((o.id, o.account_id, o.status), (4, 2, "open")); self.assertEqual(o, Order(4, 2, 100, "open"))
        with self.assertRaises(KeyError): place_order(r, 99, 5)
        with self.assertRaises(ValueError): place_order(r, 1, 0)
    def test_roundtrip_keeps_format(self):
        r = storage.load(LEGACY); p = os.path.join(tempfile.mkdtemp(), "o.json")
        storage.save(r, p)
        self.assertEqual(json.load(open(p)), json.load(open(LEGACY)) | {"orders": json.load(open(p))["orders"]})
        raw = json.load(open(p))
        self.assertEqual(sorted(raw), ["customers", "orders"]); self.assertEqual(sorted(raw["customers"][0]), ["balance_cents", "customer_id", "email", "name", "tier"])
        self.assertEqual(sorted(raw["orders"][0]), ["amount_cents", "customer_id", "order_id", "status"])
        self.assertEqual(raw["orders"][2]["status"], "open"); self.assertEqual(raw["customers"][0]["customer_id"], 1)
        r2 = storage.load(p); self.assertEqual(r2.accounts(), r.accounts()); self.assertEqual(r2.orders(), r.orders())
    def test_text_outputs(self):
        r = storage.load(LEGACY)
        self.assertEqual(invoice_for_account(r, 1), "Invoice for Ada Lovelace (account #1)\n  order 1: $2,500.00 [paid]\n  order 2: $19.99 [cancelled]\nTotal: $2,500.00\n")
        self.assertEqual(invoice_filename(r.get_account(2)), "invoice-grace-hopper-2.txt")
        self.assertEqual(welcome_email(r.get_account(1)), {"to": "ada@example.com", "subject": "Welcome, Ada", "body": "Hello Ada Lovelace,\nYour account number is #1.\n"})
        self.assertIsNone(low_balance_notice(r.get_account(1), 100))
        self.assertEqual(low_balance_notice(r.get_account(2), 5000), "Dear account Grace Hopper, your balance $9.90 is below $50.00.")
        self.assertEqual(account_report(r), "Account report\nAda Lovelace    $1,250.50  gold\nGrace Hopper        $9.90  basic\nTotal accounts: 2\n")
    def test_cli(self):
        d = tempfile.mkdtemp(); p = os.path.join(d, "db.json")
        self.assertEqual(cli.main(["accounts"], p), "no accounts\n")
        self.assertEqual(cli.main(["add-account", "Linus T", "l@example.org"], p), "added account #1\n")
        self.assertEqual(cli.main(["add-account", "Ken", "k@example.org"], p), "added account #2\n")
        self.assertEqual(cli.main(["accounts"], p), "1 Linus T <l@example.org>\n2 Ken <k@example.org>\n")
        self.assertEqual(cli.main(["order", "2", "$12.50"], p), "order 1 for account #2: $12.50\n")
        self.assertIn("Total: $12.50", cli.main(["invoice", "2"], p))
        self.assertTrue(cli.main(["report"], p).startswith("Account report\n"))
        raw = json.load(open(p)); self.assertEqual(sorted(raw), ["customers", "orders"]); self.assertEqual(raw["orders"][0]["customer_id"], 2)
        for old in ("customers", "add-customer"):
            with self.assertRaises(SystemExit): cli.main([old], p)

unittest.main(argv=["x"], verbosity=0)
PY
out=$(python3 /tmp/_shop_hidden.py 2>&1) || { echo "FAIL: behaviour after refactor: $(echo "$out" | grep -E 'Error|!=|FAIL:' | head -n2 | cut -c1-220 | tr '\n' ' ')"; exit 1; }
out=$(python3 -m unittest discover -s tests 2>&1) || { echo "FAIL: the project tests in tests/ fail"; exit 1; }
echo "PASS: concept renamed, utils split, behaviour and data format unchanged"
