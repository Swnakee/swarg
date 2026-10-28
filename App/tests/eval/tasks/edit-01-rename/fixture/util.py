def calc_total(items):
    total = 0
    for price, qty in items:
        total += price * qty
    return total
