from decimal import Decimal

from .rounding import money

RATES = {"NY": Decimal("0.08875"), "CA": Decimal("0.0725"), "TX": Decimal("0.0625"), "OR": Decimal("0")}


def rate_for(region):
    if region not in RATES:
        raise ValueError("unknown region %s" % region)
    return RATES[region]


def tax_for(order, taxable_sub, discount, sub):
    rate = rate_for(order.region)
    return money(taxable_sub * rate)
