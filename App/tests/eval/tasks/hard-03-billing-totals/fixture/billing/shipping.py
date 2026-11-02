from decimal import Decimal

FLAT = Decimal("7.50")
FREE_FROM = Decimal("100.00")


def shipping_for(sub, discount):
    if sub >= FREE_FROM:
        return Decimal("0.00")
    return FLAT
