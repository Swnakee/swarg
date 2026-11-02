from decimal import Decimal


def to_cents(eur):
    """Whole cents from an amount in EUR."""
    return int(round(eur * 100))
