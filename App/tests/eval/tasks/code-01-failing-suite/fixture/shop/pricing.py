def apply_tax(amount, rate):
    """Add the tax rate (0.2 = 20%) and round to cents."""
    return round(amount * (1 + rate), 2)
