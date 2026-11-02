from dataclasses import dataclass
from decimal import Decimal

from .discount import discount_for
from .pricing import subtotal, taxable_subtotal
from .shipping import shipping_for
from .tax import tax_for

ZERO = Decimal("0.00")


@dataclass
class Invoice:
    subtotal: Decimal
    discount: Decimal
    tax: Decimal
    shipping: Decimal
    total: Decimal


def build_invoice(order):
    sub = subtotal(order)
    if sub == 0:
        return Invoice(ZERO, ZERO, ZERO, ZERO, ZERO)
    disc = discount_for(order, sub)
    tax = tax_for(order, taxable_subtotal(order), disc, sub)
    ship = shipping_for(sub, disc)
    return Invoice(sub, disc, tax, ship, sub - disc + tax + ship)
