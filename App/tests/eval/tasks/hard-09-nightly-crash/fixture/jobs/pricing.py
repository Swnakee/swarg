from decimal import Decimal

from .fx import get_rate
from .log import log
from .money import to_cents


def price_one(p, rates):
    """Price of one product in EUR cents, or None when there is no usable rate."""
    rate = get_rate(rates, p.currency)
    net = p.price * (100 - p.discount_pct) / 100
    return to_cents(net / rate)


def price_all(products, rates):
    rows = []
    skipped = 0
    for p in products:
        cents = price_one(p, rates)
        if cents is None:
            log("WARN", "skip %s: no rate for %s" % (p.sku, p.currency))
            skipped += 1
            continue
        rows.append((p.sku, cents))
    rows.sort()
    return rows, skipped
