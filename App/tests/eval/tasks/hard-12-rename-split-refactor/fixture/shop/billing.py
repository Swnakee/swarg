from .orders import orders_for_customer, total_for_customer
from .utils import fmt_money, slug


def invoice_for_customer(repo, customer_id):
    c = repo.get_customer(customer_id)
    lines = ["Invoice for %s (customer #%d)" % (c.name, c.id)]
    for o in orders_for_customer(repo, customer_id):
        lines.append("  order %d: %s [%s]" % (o.id, fmt_money(o.amount_cents), o.status))
    lines.append("Total: %s" % fmt_money(total_for_customer(repo, customer_id)))
    return "\n".join(lines) + "\n"


def invoice_filename(customer):
    return "invoice-%s-%d.txt" % (slug(customer.name), customer.id)
