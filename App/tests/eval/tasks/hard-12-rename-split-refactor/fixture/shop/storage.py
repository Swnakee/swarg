import json

from .customer import Customer
from .orders import Order
from .repo import CustomerRepo


def load(path):
    with open(path, encoding="utf-8") as f:
        raw = json.load(f)
    customers = [Customer(c["customer_id"], c["name"], c["email"], c.get("balance_cents", 0), c.get("tier", "basic"))
                 for c in raw.get("customers", [])]
    orders = [Order(o["order_id"], o["customer_id"], o["amount_cents"], o.get("status", "open"))
              for o in raw.get("orders", [])]
    return CustomerRepo(customers, orders)


def save(repo, path):
    raw = {
        "customers": [{"customer_id": c.id, "name": c.name, "email": c.email,
                       "balance_cents": c.balance_cents, "tier": c.tier} for c in repo.customers()],
        "orders": [{"order_id": o.id, "customer_id": o.customer_id, "amount_cents": o.amount_cents,
                    "status": o.status} for o in repo.orders()],
    }
    with open(path, "w", encoding="utf-8") as f:
        json.dump(raw, f, indent=1, sort_keys=True)
        f.write("\n")
