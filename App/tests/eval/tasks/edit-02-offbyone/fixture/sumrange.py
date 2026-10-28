def sum_to(n):
    """Return 1 + 2 + ... + n (0 for n = 0)."""
    total = 0
    for i in range(n):
        total += i
    return total
