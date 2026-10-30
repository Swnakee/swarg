from .discounts import fixed_off
from .pricing import apply_tax


def checkout(cart, coupon=0.0, tax_rate=0.0):
    """Total to pay: the coupon is taken off the subtotal, then tax is added."""
    taxed = apply_tax(cart.subtotal(), tax_rate)
    return fixed_off(taxed, coupon)
