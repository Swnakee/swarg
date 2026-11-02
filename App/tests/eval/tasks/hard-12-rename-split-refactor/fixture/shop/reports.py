from .utils import fmt_money, truncate


def customer_report(repo):
    lines = ["Customer report"]
    for c in repo.customers():
        lines.append("%-12s %12s  %s" % (truncate(c.name, 12), fmt_money(c.balance_cents), c.tier))
    lines.append("Total customers: %d" % len(repo.customers()))
    return "\n".join(lines) + "\n"
