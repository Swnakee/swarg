from .rounding import money


def line_total(line):
    return money(line.qty * line.unit_price)


def subtotal(order):
    return sum((line_total(l) for l in order.lines), money(0))


def taxable_subtotal(order):
    return sum((line_total(l) for l in order.lines if l.taxable), money(0))
