from decimal import Decimal

from .invoice import build_invoice


def customer_totals(orders):
    """Return [(customer, total)] sorted by total descending."""
    totals = {}
    for o in orders:
        key = o.customer_name
        totals[key] = totals.get(key, Decimal("0.00")) + build_invoice(o).total
    return sorted(totals.items(), key=lambda kv: -kv[1])
