from decimal import Decimal


def money(x):
    """Round to cents."""
    return Decimal(str(round(float(x), 2))).quantize(Decimal("0.01"))
