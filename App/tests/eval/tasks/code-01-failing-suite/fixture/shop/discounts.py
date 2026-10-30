def fixed_off(amount, off):
    """Take a fixed coupon amount off, never below zero."""
    return max(0.0, amount - off)
