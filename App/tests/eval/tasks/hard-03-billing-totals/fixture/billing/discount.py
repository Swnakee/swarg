from decimal import Decimal

from .rounding import money

TIERS = [(Decimal("2000.00"), 15), (Decimal("500.00"), 10), (Decimal("100.00"), 5)]
LOYALTY_POINTS = 5
MAX_PERCENT = 18


def volume_percent(sub):
    for threshold, pct in TIERS:
        if sub > threshold:
            return pct
    return 0


def discount_for(order, sub):
    pct = volume_percent(sub)
    if order.loyalty:
        pct += LOYALTY_POINTS
    return money(sub * pct / 100)
