import os

from . import storage
from .billing import invoice_for_customer
from .customer import new_customer
from .orders import place_order
from .repo import CustomerRepo
from .reports import customer_report
from .utils import fmt_money, parse_money


def main(argv, db_path):
    repo = storage.load(db_path) if os.path.exists(db_path) else CustomerRepo()
    cmd, args = argv[0], argv[1:]
    if cmd == "add-customer":
        c = new_customer(repo.next_customer_id(), args[0], args[1])
        repo.add_customer(c)
        storage.save(repo, db_path)
        return "added customer #%d\n" % c.id
    if cmd == "customers":
        return "".join("%d %s <%s>\n" % (c.id, c.name, c.email) for c in repo.customers()) or "no customers\n"
    if cmd == "order":
        o = place_order(repo, int(args[0]), parse_money(args[1]))
        storage.save(repo, db_path)
        return "order %d for customer #%s: %s\n" % (o.id, args[0], fmt_money(o.amount_cents))
    if cmd == "invoice":
        return invoice_for_customer(repo, int(args[0]))
    if cmd == "report":
        return customer_report(repo)
    raise SystemExit("unknown command: %s" % cmd)
